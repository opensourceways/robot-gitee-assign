package main

import (
	"errors"

	"github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/robot-gitee-framework"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"github.com/sirupsen/logrus"
)

const botName = "assign"

type iClient interface {
	ListCollaborators(org, repo string) ([]sdk.ProjectMember, error)
	AssignPR(owner, repo string, number int32, logins []string) error
	UnassignPR(owner, repo string, number int32, logins []string) error
	AssignGiteeIssue(org, repo string, number string, login string) error
	UnassignGiteeIssue(org, repo string, number string, login string) error
	CreateIssueComment(org, repo string, number string, comment string) error
	CreatePRComment(org, repo string, number int32, comment string) error
	UpdateIssue(owner, number string, param sdk.IssueUpdateParam) (sdk.Issue, error)
	GetRepo(org, repo string) (sdk.Project, error)
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type robot struct {
	cli iClient
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config) (*configuration, error) {
	if c, ok := cfg.(*configuration); ok {
		return c, nil
	}
	return nil, errors.New("can't convert to configuration")
}

func (bot *robot) RegisterEventHandler(f framework.HandlerRegitster) {
	f.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, c config.Config, log *logrus.Entry) error {
	if !e.IsCreatingCommentEvent() {
		log.Debug("Event is not a creation of a comment, skipping.")
		return nil
	}

	config, err := bot.getConfig(c)
	if err != nil {
		return err
	}

	cfg := config.configFor(e.GetOrgRepo())
	if cfg == nil {
		return nil
	}

	if e.IsIssue() && e.IsIssueOpen() {
		if cfg.EnableIssueAssign {
			if err := bot.handleIssueAssign(e); err != nil {
				return err
			}
		}

		if cfg.EnableIssueCollaborator {
			return bot.handleIssueCollaborator(e)
		}

		return nil
	}

	if cfg.EnablePRAssign && e.IsPullRequest() && e.IsPROpen() {
		return bot.handlePRAssign(e)
	}

	return nil
}
