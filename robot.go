package main

import (
	"errors"
	"fmt"
	"github.com/opensourceways/community-robot-lib/utils"
	"strings"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	libplugin "github.com/opensourceways/community-robot-lib/giteeplugin"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	botName           = "assign"
	addOperation      = "adding"
	removeOperation   = "removing"
	assignOperation   = "assign"
	unassignOperation = "unassign"
	logAssignIssueMsg = "%s assignee(s) from %s/%s#%s: %v"
	logAssignPRMsg    = "%s assignee(s) from %s/%s#%d: %v"
	assigneeErrMsg    = "assignee(s) **%s** can not be %s, make sure the assignee is a repository collaborator, otherwise please try again."
	moreThenOneMsg    = "assignee(s) **%s** can not be %s, because the [un]assign command can only assign one person to an issue"
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
		log.Info(fmt.Printf(logAssignPRMsg, removeOperation, prInfo.Org, prInfo.Repo, prInfo.Number, mu.Removes))
		if err := bot.cli.UnassignPR(prInfo.Org, prInfo.Repo, prInfo.Number, mu.Removes); err != nil {
			log.Errorf(logAssignPRMsg, mu.Removes, prInfo.Org, prInfo.Repo, prInfo.Number, err.Error())
			commandResult = append(commandResult, fmt.Sprintf(assigneeErrMsg, strings.Join(mu.Removes, ","), unassignOperation))
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

func (bot *robot) handleIssue(e giteeclient.IssueNoteEvent, cfg *botConfig, log *logrus.Entry) error {
	multiErrors := utils.NewMultiErrors()
	if !cfg.CloseIssueAssign {
		multiErrors.AddError(bot.issueAssignOperate(e, log))
	}

	if !cfg.CloseCollaboratorOption {
		multiErrors.AddError(bot.issueCollaboratorOperate(e, log))
	}

	return multiErrors.Err()
}

func (bot *robot) handlePRAssign(prInfo giteeclient.PRInfo, adds []string, log *logrus.Entry) []string {
	var assignRes []string
	log.Info(fmt.Printf(logAssignPRMsg, addOperation, prInfo.Org, prInfo.Repo, prInfo.Number, adds))

	logErr := func(err string) {
		log.Errorf(logAssignPRMsg, addOperation, adds, prInfo.Org, prInfo.Repo, prInfo.Number, err)
	}

	csSet, err := bot.getReposCollaborators(prInfo.Org, prInfo.Repo)
	if err != nil {
		logErr(err.Error())
		return append(assignRes, fmt.Sprintf(assigneeErrMsg, strings.Join(adds, ","), assignOperation))
	}

	addSet := sets.NewString(adds...)
	cantAdds := addSet.Difference(csSet)
	canAdds := addSet.Difference(cantAdds)

	if len(canAdds) > 0 {
		if err := bot.cli.AssignPR(prInfo.Org, prInfo.Repo, prInfo.Number, canAdds.List()); err != nil {
			logErr(err.Error())
			assignRes = append(assignRes, fmt.Sprintf(assigneeErrMsg, strings.Join(canAdds.List(), ","), assignOperation))
		}
	}

	if len(cantAdds) > 0 {
		assignRes = append(assignRes, fmt.Sprintf("gitee didn't allow you to assign to: %s.", strings.Join(cantAdds.List(), ",")))
	}

	return assignRes
}

func (bot *robot) issueAssignOperate(e giteeclient.IssueNoteEvent, log *logrus.Entry) error {
	mu := matchAssign(e.GetComment(), e.GetCommenter())
	if !mu.isMatched() {
		return nil
	}

	org, repo := e.GetOrgRep()
	number := e.GetIssueNumber()
	var execRes []string

	execRm := func() {
		lr := len(mu.Removes)
		if lr == 1 {
			cUser := mu.Removes[0]
			if e.Issue.Assignee == nil || e.Issue.Assignee.Login == cUser {
				execRes = append(execRes, fmt.Sprintf("unassign failed, because **%s** is not assignee.", cUser))
				return
			}

			log.Info(fmt.Printf(logAssignIssueMsg, removeOperation, org, repo, number, mu.Removes))
			if err := bot.cli.UnassignGiteeIssue(org, repo, number, cUser); err != nil {
				log.Error(err)
				execRes = append(execRes, fmt.Sprintf(assigneeErrMsg, cUser, unassignOperation))
			}
			return
		}
		if lr > 1 {
			execRes = append(execRes, fmt.Sprintf(moreThenOneMsg, strings.Join(mu.Removes, ","), unassignOperation))
			return
		}
	}

	execAdd := func() {
		la := len(mu.Adds)
		if la == 1 {
			log.Info(fmt.Printf(logAssignIssueMsg, addOperation, org, repo, number, mu.Adds))

			if err := bot.cli.AssignGiteeIssue(org, repo, number, mu.Adds[0]); err != nil {
				log.Error(err)
				execRes = append(execRes, fmt.Sprintf(assigneeErrMsg, mu.Adds[0], assignOperation))
			}
			return
		}
		if la > 1 {
			execRes = append(execRes, fmt.Sprintf(moreThenOneMsg, strings.Join(mu.Adds, ","), assignOperation))
		}
	}

	execRm()
	execAdd()

	return bot.createIssueComment(org, repo, number, e.GetCommenter(), e.Comment.HtmlUrl, execRes)
}

func (bot *robot) issueCollaboratorOperate(e giteeclient.IssueNoteEvent, log *logrus.Entry) error {
	mu := matchCollaborator(e.GetComment(), e.GetCommenter())
	if !mu.isMatched() {
		return nil
	}

	org, repo := e.GetOrgRep()
	repoColls, err := bot.getReposCollaborators(org, repo)
	if err != nil {
		return err
	}

	currentColls := func() sets.String {
		ccs := sets.NewString()
		for _, v := range e.Issue.Collaborators {
			ccs.Insert(v.Login)
		}
		return ccs
	}()

	var cResult []string
	filterAdds := func() sets.String {
		addSet := sets.NewString()
		if len(mu.Adds) == 0 {
			return addSet
		}

		addSet.Insert(mu.Adds...)
		// assignee can not be at collaborator
		if e.Issue.Assignee != nil {
			if v := e.Issue.Assignee.Login; addSet.Has(v) {
				cResult = append(cResult, fmt.Sprintf("the assignee(**%s**) can't be collaborator at same time", v))
				addSet.Delete(v)
			}
		}

		// issue's collaborator must be repository collaborator
		if v := addSet.Difference(repoColls); v.Len() > 0 {
			cResult = append(cResult, fmt.Sprintf("these persons(**%s**) are not allowed to be added as collaborators which must be the member of repository.", strings.Join(v.List(), ", ")))
			addSet = addSet.Difference(v)
		}

		return addSet
	}

	filterRms := func() sets.String {
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
	}

	canAdds := filterAdds()
	canRms := filterRms()

	// filter the need updates collaborators.
	// remove issue collaborators who are no longer collaborators of the repository.
	// remove canRms collaborators.
	// add canAdds collaborators.
	updates := currentColls.Intersection(repoColls).Difference(canRms).Union(canAdds)

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
	updatesWithComment := strings.Join(canAdds.Union(canRms).List(), ",")
	if _, err = bot.cli.UpdateIssue(org, number, param); err != nil {
		log.Error(err)
		cResult = append(cResult, fmt.Sprintf("update issue's collaborators %s failed", updatesWithComment))
	}

	return bot.createIssueComment(org, repo, number, e.GetCommenter(), e.Comment.HtmlUrl, cResult)
}

func (bot *robot) createIssueComment(org, repo, number, commenter, url string, messges []string) error {
	if len(messges) == 0 {
		return nil
	}
	comment := genExecCommandComment(commenter, url, messges)
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
