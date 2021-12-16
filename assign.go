package main

import (
	"fmt"
	"strings"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	"github.com/opensourceways/community-robot-lib/utils"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	commentAssignMiss       = "%s.\n\nChoose one of following members as assignee.\n- %s"
	commentCollaboratorMiss = "Update issue's collaborators failed: \n%s."
)

type missUserAction string

const (
	actionAssign     missUserAction = "assign"
	actionUnAssign   missUserAction = "unassign"
	actionUpdateColl missUserAction = "update"
)

type missingUsers struct {
	users  []string
	action missUserAction
}

func (m missingUsers) Error() string {
	return fmt.Sprintf(
		"Gitee didn't allow you to %s the following user(s): %s",
		m.action,
		strings.Join(m.users, ", "),
	)
}

type handler interface {
	handle() error
}

type common struct {
	cli        iClient
	cfg        *botConfig
	log        *logrus.Entry
	org        string
	repo       string
	commenter  string
	comment    string
	commentUrl string
}

func (c *common) repoCollaborateSet() (sets.String, error) {
	collaborators, err := c.cli.ListCollaborators(c.org, c.repo)
	if err != nil {
		return nil, err
	}

	colls := sets.NewString()
	for _, item := range collaborators {
		colls.Insert(item.Login)
	}

	return colls, err
}

type assignPR struct {
	common
	number int32
}

func (ap *assignPR) assign(users sets.String) error {
	cs, err := ap.repoCollaborateSet()
	if err != nil {
		return err
	}

	miss := users.Difference(cs)
	corrects := users.Difference(miss)

	if corrects.Len() > 0 {
		if err := ap.cli.AssignPR(ap.org, ap.repo, ap.number, corrects.List()); err != nil {
			return err
		}
	}

	if miss.Len() > 0 {
		return &missingUsers{
			action: actionAssign,
			users:  miss.List(),
		}
	}

	return nil
}

func (ap *assignPR) unAssign(users sets.String) error {
	return ap.cli.UnassignPR(ap.org, ap.repo, ap.number, users.UnsortedList())
}

func (ap *assignPR) addComment(mu missingUsers) error {
	comment := mu.Error()

	if v, err := ap.repoCollaborateSet(); err == nil {
		ap.log.Error(err)
		comment = fmt.Sprintf(commentAssignMiss, comment, v.List())
	}

	return ap.cli.CreatePRComment(
		ap.org, ap.repo, ap.number,
		commentWrapper(ap.commenter, ap.commentUrl, comment),
	)
}

func (ap *assignPR) handle() error {
	if ap.cfg.EnablePRAssign {
		return nil
	}

	mu := matchAssign(ap.comment, ap.commenter)
	if !mu.isMatched() {
		return nil
	}

	if len(mu.Removes) > 0 {
		return ap.unAssign(mu.Removes)
	}

	if len(mu.Adds) > 0 {
		err := ap.assign(mu.Adds)
		if e, ok := err.(missingUsers); ok {
			return ap.addComment(e)
		}

		return err
	}

	return nil
}

type assignIssue struct {
	common
	issue *sdk.IssueHook
}

func (ai *assignIssue) assignAssignee(users []string) error {
	muErr := missingUsers{users: users, action: actionAssign}
	if len(users) > 1 {
		return muErr
	}

	err := ai.cli.AssignGiteeIssue(ai.org, ai.repo, ai.issue.GetNumber(), users[0])
	if _, ok := err.(giteeclient.ErrorForbidden); ok {
		return muErr
	}

	return err
}

func (ai *assignIssue) unassignAssignee(users []string) error {
	if len(users) > 1 {
		return missingUsers{users: users, action: actionUnAssign}
	}

	if ai.issue.Assignee != nil && ai.issue.Assignee.Login == users[0] {
		return ai.cli.UnassignGiteeIssue(ai.org, ai.repo, ai.issue.GetNumber(), users[0])
	}

	return nil
}

func (ai *assignIssue) filterCollaborators(mu *matchUsers) (sets.String, error) {
	rc, err := ai.repoCollaborateSet()
	if err != nil {
		return nil, err
	}

	cc := sets.NewString()
	for _, v := range ai.issue.Collaborators {
		cc.Insert(v.Login)
	}

	miss := sets.NewString()
	canAdd := sets.NewString()

	if len(mu.Adds) > 0 {
		miss = mu.Adds.Difference(rc)

		if ai.issue.Assignee != nil && mu.Adds.Has(ai.issue.Assignee.Login) {
			miss.Insert(ai.issue.Assignee.Login)
		}

		canAdd = mu.Adds.Difference(miss)
	}

	canRm := sets.NewString()
	if len(mu.Removes) > 0 {
		rmMiss := mu.Removes.Difference(cc)
		canRm = mu.Removes.Difference(rmMiss)
		miss = miss.Union(rmMiss)
	}

	update := cc.Intersection(rc).Difference(canRm).Union(canAdd)

	if len(miss) > 0 {
		return update, missingUsers{users: miss.UnsortedList(), action: actionUpdateColl}
	}

	return update, nil
}

func (ai *assignIssue) updateCollaborators(users sets.String) error {
	// for gitee api "0" means empty collaborator
	updateStr := "0"
	if len(users) > 0 {
		updateStr = strings.Join(users.UnsortedList(), ",")
	}

	param := sdk.IssueUpdateParam{
		Repo:          ai.repo,
		Collaborators: updateStr,
	}

	if _, err := ai.cli.UpdateIssue(ai.org, ai.issue.GetNumber(), param); err != nil {
		return err
	}

	return nil
}

func (ai *assignIssue) addComment(mu missingUsers) error {
	comment := ""

	switch mu.action {
	case actionUpdateColl:
		comment = fmt.Sprintf(commentCollaboratorMiss, mu.Error())
	case actionAssign, actionUnAssign:
		if len(mu.users) > 0 {
			comment = fmt.Sprintf("Can only %s one person to an issue.", mu.action)
		} else {
			comment = mu.Error()
			if mu.action == actionAssign {
				if v, err := ai.repoCollaborateSet(); err == nil {
					ai.log.Error(err)
					comment = fmt.Sprintf(commentAssignMiss, comment, v.List())
				}
			}
		}
	}

	if comment == "" {
		return nil
	}

	return ai.cli.CreateIssueComment(
		ai.org, ai.repo, ai.issue.GetNumber(),
		commentWrapper(ai.commenter, ai.commentUrl, comment),
	)
}

func (ai *assignIssue) handleAssign() error {
	mu := matchAssign(ai.comment, ai.commenter)
	if !mu.isMatched() {
		return nil
	}

	f := func(err error) error {
		if v, ok := err.(missingUsers); ok {
			return ai.addComment(v)
		} else {
			return err
		}
	}

	if len(mu.Removes) > 0 {
		return f(ai.unassignAssignee(mu.Removes.UnsortedList()))
	}

	if len(mu.Adds) > 0 {
		return f(ai.assignAssignee(mu.Adds.UnsortedList()))
	}

	return nil
}

func (ai *assignIssue) handleCollaborators() error {
	mu := matchCollaborator(ai.comment, ai.commenter)
	if !mu.isMatched() {
		return nil
	}

	updates, err := ai.filterCollaborators(mu)
	v, ok := err.(missingUsers)
	if !ok && err != nil {
		return err
	}

	if err := ai.updateCollaborators(updates); err != nil {
		return err
	}

	return ai.addComment(v)
}

func (ai *assignIssue) handle() error {
	mErr := utils.NewMultiErrors()
	if ai.cfg.EnableIssueAssign {
		mErr.AddError(ai.handleAssign())
	}

	if ai.cfg.EnableCollaboratorOption {
		mErr.AddError(ai.handleCollaborators())
	}

	return mErr.Err()
}

func newHandler(c iClient, e giteeclient.NoteEventWrapper, cfg *botConfig, log *logrus.Entry) handler {
	org, repo := e.GetOrgRep()
	comm := common{
		cli:        c,
		cfg:        cfg,
		log:        log,
		org:        org,
		repo:       repo,
		commenter:  e.GetCommenter(),
		comment:    e.GetComment(),
		commentUrl: e.NoteEvent.GetComment().HtmlUrl,
	}

	if e.IsPullRequest() {
		return &assignPR{
			common: comm,
			number: e.GetPullRequest().GetNumber(),
		}
	}

	if e.IsIssue() {
		return &assignIssue{
			common: comm,
			issue:  e.GetIssue(),
		}
	}

	return nil
}

func commentWrapper(commenter, commentUrl, msg string) string {
	fmtStr := `@%s In response to [this](%s):
%s`
	return fmt.Sprintf(fmtStr, commenter, commentUrl, msg)
}
