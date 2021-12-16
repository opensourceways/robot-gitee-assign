package main

import (
	"errors"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	libplugin "github.com/opensourceways/community-robot-lib/giteeplugin"
	"github.com/sirupsen/logrus"
)

const (
	botName = "assign"
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

	if h := newHandler(bot.cli, ew, bCfg, log); h != nil {
		return h.handle()
	}

	return nil
}
