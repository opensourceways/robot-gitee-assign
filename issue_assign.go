package main

import (
	"fmt"

	"github.com/opensourceways/community-robot-lib/giteeclient"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	msgMultipleAssignee           = "Can only assign one assignee to the issue."
	msgAssignRepeatedly           = "This issue is already assigned to ***%s***. Please do not assign repeatedly."
	msgNotAllowAssign             = "This issue can not be assigned to ***%s***. Please try to assign to the repository collaborators."
	msgCollaboratorCantAsAssignee = "***%s*** is already the issue collaborator and cannot be assigned as the assignee."
)

func (bot *robot) handleIssueAssign(e *sdk.NoteEvent) error {
	assign, unassign := parseAssignCmd(e.GetComment().GetBody(), e.GetCommenter())
	if assign.Len() == 0 && unassign.Len() == 0 {
		return nil
	}

	org, repo := e.GetOrgRepo()
	issue := e.GetIssue()
	number := e.GetIssueNumber()
	currentAssignee := issue.GetAssignee().GetLogin()

	writeComment := func(s string) error {
		return bot.cli.CreateIssueComment(
			org, repo, number, giteeclient.GenResponseWithReference(e, s),
		)
	}

	if n := assign.Len(); n > 0 {
		if n > 1 {
			return writeComment(msgMultipleAssignee)
		}

		if assign.Has(currentAssignee) {
			return writeComment(fmt.Sprintf(msgAssignRepeatedly, currentAssignee))
		}

		newOne := assign.UnsortedList()[0]

		if getIssueCollaborator(issue).Has(newOne) {
			return writeComment(fmt.Sprintf(msgCollaboratorCantAsAssignee, newOne))
		}

		err := bot.cli.AssignGiteeIssue(org, repo, number, newOne)
		if err == nil {
			return nil
		}

		if _, ok := err.(giteeclient.ErrorForbidden); ok {
			return writeComment(fmt.Sprintf(msgNotAllowAssign, newOne))
		}

		return err
	}

	// '/unassign xx' means that don't assign to that people.
	// If that people is not the current assignee, it is already satisfied.
	if unassign.Has(currentAssignee) {
		return bot.cli.UnassignGiteeIssue(org, repo, number, "")
	}

	return nil
}

func getIssueCollaborator(issue *sdk.IssueHook) sets.String {
	r := sets.NewString()

	v := issue.GetCollaborators()
	for i := range v {
		r.Insert(v[i].Login)
	}

	return r
}
