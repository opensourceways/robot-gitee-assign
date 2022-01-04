package main

import "github.com/opensourceways/community-robot-lib/config"

type configuration struct {
	ConfigItems []botConfig `json:"config_items,omitempty"`
}

func (c *configuration) configFor(org, repo string) *botConfig {
	if c == nil {
		return nil
	}

	items := c.ConfigItems
	v := make([]config.IRepoFilter, len(items))
	for i := range items {
		v[i] = &items[i]
	}

	if i := config.Find(org, repo, v); i >= 0 {
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
	config.RepoFilter

	// EnablePRAssign indicates whether to enable manaing the PR reviewers, default false.
	EnablePRAssign bool `json:"enable_pr_assign,omitempty"`

	// EnableIssueAssign indicates whether to enable managing the issue assignee, default false.
	EnableIssueAssign bool `json:"enable_issue_assign,omitempty"`

	// EnableIssueCollaborator indicates whether to enable managing the issue collaborators, default false.
	EnableIssueCollaborator bool `json:"enable_issue_collaborator,omitempty"`
}

func (c *botConfig) setDefault() {
}

func (c *botConfig) validate() error {
	return c.RepoFilter.Validate()
}
