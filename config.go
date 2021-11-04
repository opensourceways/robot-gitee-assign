package main

import libconfig "github.com/opensourceways/community-robot-lib/config"

type configuration struct {
	ConfigItems []botConfig `json:"config_items,omitempty"`
}

func (c *configuration) configFor(org, repo string) *botConfig {
	if c == nil {
		return nil
	}

	items := c.ConfigItems
	v := make([]libconfig.IPluginForRepo, len(items))
	for i := range items {
		v[i] = &items[i]
	}

	if i := libconfig.FindConfig(org, repo, v); i >= 0 {
		return &items[i]
	}
	return nil
}

func (c *configuration) Validate() error {
	if c == nil {
		return nil
	}

	items := c.ConfigItems
	for i := range items {
		if err := items[i].validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *configuration) SetDefault() {
	if c == nil {
		return
	}

	Items := c.ConfigItems
	for i := range Items {
		Items[i].setDefault()
	}
}

type botConfig struct {
	libconfig.PluginForRepo
	// closePRAssign controls whether the assign command is valid for PR, default false.
	ClosePRAssign bool `json:"close_pr_assign"`
	// closeIssueAssign controls whether the assign command is valid for issue, default false.
	CloseIssueAssign bool `json:"close_issue_assign"`
	// closeCollaboratorOption controls whether the collaborator command is valid for issue,default false.
	CloseCollaboratorOption bool `json:"close_collaborator_option"`
}

func (c *botConfig) setDefault() {
}

func (c *botConfig) validate() error {
	return c.PluginForRepo.Validate()
}
