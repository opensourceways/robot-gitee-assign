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
	// EnablePRAssign controls whether the assign command is valid for PR, default false.
	EnablePRAssign bool `json:"enable_pr_assign,omitempty"`

	// EnableIssueAssign controls whether the assign command is valid for issue, default false.
	EnableIssueAssign bool `json:"enable_issue_assign,omitempty"`

	// EnableCollaboratorOption controls whether the collaborator command is valid for issue,default false.
	EnableCollaboratorOption bool `json:"enable_collaborator_option,omitempty"`
}

func (c *botConfig) setDefault() {
}

func (c *botConfig) validate() error {
	return c.PluginForRepo.Validate()
}
