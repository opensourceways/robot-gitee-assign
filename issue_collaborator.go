package main

import (
	"fmt"
	"strings"

	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
)

func (bot *robot) handleIssueCollaborator(e *sdk.NoteEvent) error {
	assign, unassign := parseAssignIssueCollaboratorsCmd(
		e.GetComment().GetBody(), e.GetCommenter(),
	)
	if assign.Len() == 0 && unassign.Len() == 0 {
		return nil
	}

	org, repo := e.GetOrgRepo()

	writeComment := func(s string) error {
		return bot.cli.CreateIssueComment(org, repo, e.GetIssueNumber(), s)
	}

	if v := assign.Intersection(unassign); v.Len() > 0 {
		return writeComment(fmt.Sprintf(
			"@%s , conflict people exist who are: %s",
			e.GetCommenter(),
			strings.Join(v.UnsortedList(), ", "),
		))
	}

	issue := e.GetIssue()

	if assign.Len() > 0 {
		repo, err := bot.cli.GetRepo(org, repo)
		if err != nil {
			return err
		}

		invalidOnes := []string{}

		currentAssignee := issue.GetAssignee().GetLogin()
		if assign.Has(currentAssignee) {
			invalidOnes = append(invalidOnes, fmt.Sprintf(
				"Can't add the assignee( %s ) as collaborator",
				currentAssignee,
			))

			assign.Delete(currentAssignee)
		}

		members := sets.NewString(repo.GetMembers()...)

		if v := assign.Difference(members); v.Len() > 0 {
			invalidOnes = append(invalidOnes, fmt.Sprintf(
				"These people( %s ) who are not the member of repo are not allowed to be added as collaborators of issue.",
				strings.Join(v.List(), ", "),
			))

			assign = assign.Difference(v)
		}

		if len(invalidOnes) > 0 {
			writeComment(fmt.Sprintf(
				"@%s , the following people can't be added as collaborators of issue with reasons bellow.\n%s",
				e.GetCommenter(),
				strings.Join(invalidOnes, "\n"),
			))
		}
	}

	return bot.updateIssueCollaborator(org, repo, issue, assign, unassign)
}

func (bot *robot) updateIssueCollaborator(org, repo string, issue *sdk.IssueHook, assign, unassign sets.String) error {
	current := getIssueCollaborator(issue)
	newOnes := current

	if unassign.Len() > 0 {
		newOnes = newOnes.Difference(unassign)
	}

	if assign.Len() > 0 {
		newOnes = newOnes.Union(assign)
	}

	if newOnes.Equal(current) {
		return nil
	}

	// for gitee api "0" means empty collaborator
	v := "0"
	if newOnes.Len() > 0 {
		v = strings.Join(newOnes.UnsortedList(), ",")
	}
	_, err := bot.cli.UpdateIssue(
		org, issue.GetNumber(),
		sdk.IssueUpdateParam{
			Repo:          repo,
			Collaborators: v,
		},
	)
	return err
}
