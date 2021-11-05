package main

import (
	"errors"
	"fmt"
	"strings"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	libplugin "github.com/opensourceways/community-robot-lib/giteeplugin"
	"github.com/opensourceways/community-robot-lib/utils"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	botName        = "assign"
	assigneeErrMsg = "assignee(s) **%s** can not be %s, make sure the assignee is a repository collaborator, otherwise please try again."
	moreThenOneMsg = "assignee(s) **%s** can not be %s, because the [un]assign command can only assign one person to an issue"
)

type iClient interface {
	ListCollaborators(org, repo string) ([]sdk.ProjectMember, error)
	AssignPR(owner, repo string, number int32, logins []string) error
	UnassignPR(owner, repo string, number int32, logins []string) error
	AssignGiteeIssue(org, repo string, number string, login string) error
	UnassignGiteeIssue(org, repo string, number string, login string) error
	CreateIssueComment(org, repo string, number string, comment string) error
	CreatePRComment(org, repo string, number int32, comment string) error
	UpdateIssue(owner, number string, param sdk.IssueUpdateParam) (sdk.Issue, error)
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type robot struct {
	cli iClient
}

func (bot *robot) NewPluginConfig() libconfig.PluginConfig {
	return &configuration{}
}

func (bot *robot) getConfig(cfg libconfig.PluginConfig) (*configuration, error) {
	if c, ok := cfg.(*configuration); ok {
		return c, nil
	}
	return nil, errors.New("can't convert to configuration")
}

func (bot *robot) RegisterEventHandler(p libplugin.HandlerRegitster) {
	p.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, cfg libconfig.PluginConfig, log *logrus.Entry) error {
	ew := giteeclient.NewNoteEventWrapper(e)
	if !ew.IsCreatingCommentEvent() {
		log.Debug("Event is not a creation of a comment, skipping.")
		return nil
	}

	config, err := bot.getConfig(cfg)
	if err != nil {
		return err
	}

	bCfg := config.configFor(ew.GetOrgRep())
	if bCfg == nil {
		return nil
	}

	if ew.IsPullRequest() {
		return bot.handlePR(giteeclient.NewPRNoteEvent(e), bCfg, log)
	}

	if ew.IsIssue() {
		return bot.handleIssue(giteeclient.NewIssueNoteEvent(e), bCfg, log)
	}

	return nil
}

func (bot *robot) handlePR(e giteeclient.PRNoteEvent, cfg *botConfig, log *logrus.Entry) error {
	if cfg.ClosePRAssign {
		return nil
	}

	mu := matchAssign(e.GetComment(), e.GetCommenter())
	if !mu.isMatched() {
		return nil
	}

	prInfo := e.GetPRInfo()
	var commandResult []string

	if len(mu.Removes) > 0 {
		if err := bot.cli.UnassignPR(prInfo.Org, prInfo.Repo, prInfo.Number, mu.Removes); err != nil {
			commandResult = append(commandResult, fmt.Sprintf(assigneeErrMsg, strings.Join(mu.Removes, ","), "unassign"))
			log.Error(err)
		}
	}

	if len(mu.Adds) > 0 {
		hr := bot.handlePRAssign(prInfo, mu.Adds, log)
		commandResult = append(commandResult, hr...)
	}

	if len(commandResult) == 0 {
		return nil
	}

	comment := genExecCommandComment(e.GetCommenter(), e.NoteEvent.Comment.HtmlUrl, commandResult)

	return bot.cli.CreatePRComment(prInfo.Org, prInfo.Repo, prInfo.Number, comment)
}

func (bot *robot) handlePRAssign(prInfo giteeclient.PRInfo, adds []string, log *logrus.Entry) []string {
	var assignRes []string

	repoCollaborateSet, err := bot.getReposCollaborators(prInfo.Org, prInfo.Repo)
	if err != nil {
		log.Error(err)
		return append(assignRes, fmt.Sprintf(assigneeErrMsg, strings.Join(adds, ","), "assign"))
	}

	addSet := sets.NewString(adds...)
	cantAdds := addSet.Difference(repoCollaborateSet)
	canAdds := addSet.Difference(cantAdds)

	if len(canAdds) > 0 {
		if err := bot.cli.AssignPR(prInfo.Org, prInfo.Repo, prInfo.Number, canAdds.List()); err != nil {
			assignRes = append(assignRes, fmt.Sprintf(assigneeErrMsg, strings.Join(canAdds.List(), ","), "assign"))
			log.Error(err)
		}
	}

	if len(cantAdds) > 0 {
		assignRes = append(assignRes, fmt.Sprintf("gitee didn't allow you to assign to: %s.", strings.Join(cantAdds.List(), ",")))
	}

	return assignRes
}

func (bot *robot) handleIssue(e giteeclient.IssueNoteEvent, cfg *botConfig, log *logrus.Entry) error {
	multiErrors := utils.NewMultiErrors()
	if !cfg.CloseIssueAssign {
		multiErrors.AddError(bot.handleIssueAssignee(e, log))
	}

	if !cfg.CloseCollaboratorOption {
		multiErrors.AddError(bot.handleIssueCollaborator(e, log))
	}

	return multiErrors.Err()
}

func (bot *robot) handleIssueAssignee(e giteeclient.IssueNoteEvent, log *logrus.Entry) error {
	mu := matchAssign(e.GetComment(), e.GetCommenter())
	if !mu.isMatched() {
		return nil
	}

	org, repo := e.GetOrgRep()
	number := e.GetIssueNumber()

	var results []string
	isAssignee := func(login string) bool {
		return e.Issue.Assignee != nil && e.Issue.Assignee.Login == login
	}

	//add a assignee
	if result := bot.issueAssigneeOperate(org, repo, number, mu.Adds, isAssignee, true, log); result != "" {
		results = append(results, result)
	}

	//remove assignee
	if result := bot.issueAssigneeOperate(org, repo, number, mu.Removes, isAssignee, false, log); result != "" {
		results = append(results, result)
	}

	return bot.createIssueComment(org, repo, number, e.GetCommenter(), e.Comment.HtmlUrl, results)
}

func (bot *robot) issueAssigneeOperate(org, repo, number string, users []string, isAssignee func(string) bool, isAdd bool, log *logrus.Entry) string {
	length := len(users)
	if length == 0 {
		return ""
	}

	operate := "unassign"
	if isAdd {
		operate = "assign"
	}

	if length > 1 {
		return fmt.Sprintf(moreThenOneMsg, strings.Join(users, ","), operate)
	}

	user := users[0]
	if isAdd {
		if isAssignee(user) {
			return fmt.Sprintf("assign failed, because **%s** already is assignee.", user)
		}

		if err := bot.cli.AssignGiteeIssue(org, repo, number, user); err != nil {
			log.Error(err)
			return fmt.Sprintf(assigneeErrMsg, user, operate)
		}
	} else {
		if !isAssignee(user) {
			return fmt.Sprintf("unassign failed, because **%s** is not assignee.", user)
		}
		if err := bot.cli.UnassignGiteeIssue(org, repo, number, user); err != nil {
			log.Error(err)
			return fmt.Sprintf(assigneeErrMsg, user, operate)
		}
	}

	return ""
}

func (bot *robot) handleIssueCollaborator(e giteeclient.IssueNoteEvent, log *logrus.Entry) error {
	mu := matchCollaborator(e.GetComment(), e.GetCommenter())
	if !mu.isMatched() {
		return nil
	}

	org, repo := e.GetOrgRep()
	repoColls, err := bot.getReposCollaborators(org, repo)
	if err != nil {
		return err
	}

	currentColls := sets.NewString()
	for _, v := range e.Issue.Collaborators {
		currentColls.Insert(v.Login)
	}

	var cResult []string
	//filter legitimate collaborators that need to be added
	canAdds := func() sets.String {
		addSet := sets.NewString()
		if len(mu.Adds) == 0 {
			return addSet
		}

		addSet.Insert(mu.Adds...)
		// assignee can not be at collaborator
		if e.Issue.Assignee != nil {
			if v := e.Issue.Assignee.Login; addSet.Has(v) {
				addSet.Delete(v)
				cResult = append(cResult, fmt.Sprintf("the assignee **%s**  can't be collaborator at same time.", v))
			}
		}

		// issue's collaborator must be repository collaborator
		if v := addSet.Difference(repoColls); v.Len() > 0 {
			addSet = addSet.Difference(v)
			msFmt := "these persons(**%s**) are not allowed to be added as collaborators which must be the member of repository."
			cResult = append(cResult, fmt.Sprintf(msFmt, strings.Join(v.List(), ", ")))
		}

		return addSet
	}()

	//filter legitimate collaborators that need to be removed
	canRemoves := func() sets.String {
		rmSet := sets.NewString()
		if len(mu.Removes) == 0 {
			return rmSet
		}

		rmSet.Insert(mu.Removes...)
		if v := rmSet.Difference(currentColls); v.Len() > 0 {
			missFmt := "these persons(**%s**) are not in the issue current collaborators and no need to be removed again."
			cResult = append(cResult, fmt.Sprintf(missFmt, strings.Join(v.List(), ", ")))
		}
		return rmSet
	}()

	// filter the need updates collaborators.
	// remove issue collaborators who are no longer collaborators of the repository.
	// remove canRemoves collaborators .
	// add canAdds collaborators.
	updates := currentColls.Intersection(repoColls).Difference(canRemoves).Union(canAdds)

	//for gitee api "0" means empty collaborator
	collaborator := "0"
	if len(updates) > 0 {
		collaborator = strings.Join(updates.List(), ",")
	}

	number := e.GetIssueNumber()
	param := sdk.IssueUpdateParam{
		Repo:          repo,
		Collaborators: collaborator,
	}

	if _, err = bot.cli.UpdateIssue(org, number, param); err != nil {
		updatesWithComment := strings.Join(canAdds.Union(canRemoves).List(), ",")
		cResult = append(cResult, fmt.Sprintf("update issue's collaborators %s failed.", updatesWithComment))
		log.Error(err)
	}

	return bot.createIssueComment(org, repo, number, e.GetCommenter(), e.Comment.HtmlUrl, cResult)
}

func (bot *robot) createIssueComment(org, repo, number, commenter, url string, messages []string) error {
	if len(messages) == 0 {
		return nil
	}
	comment := genExecCommandComment(commenter, url, messages)
	return bot.cli.CreateIssueComment(org, repo, number, comment)
}

func (bot *robot) getReposCollaborators(org, repo string) (sets.String, error) {
	collaborators, err := bot.cli.ListCollaborators(org, repo)
	if err != nil {
		return nil, err
	}

	colls := sets.NewString()
	for _, item := range collaborators {
		colls.Insert(item.Login)
	}
	return colls, nil
}

func genExecCommandComment(commenter, commentUrl string, messages []string) string {
	fmtStr := `@%s In response to [this](%s):
- %s`
	return fmt.Sprintf(fmtStr, commenter, commentUrl, strings.Join(messages, "\n- "))
}
