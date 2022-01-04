package main

import (
	"fmt"
	"strings"

	"github.com/opensourceways/community-robot-lib/giteeclient"
	"github.com/opensourceways/community-robot-lib/utils"
	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
)

func (bot *robot) handlePRAssign(e *sdk.NoteEvent) error {
	assign, unassign := parseAssignCmd(
		e.GetComment().GetBody(), e.GetCommenter(),
	)
	if assign.Len() == 0 && unassign.Len() == 0 {
		return nil
	}

	org, repo := e.GetOrgRepo()

	writeComment := func(s string) error {
		return bot.cli.CreatePRComment(
			org, repo, e.GetPRNumber(),
			giteeclient.GenResponseWithReference(e, s),
		)
	}

	if v := assign.Intersection(unassign); v.Len() > 0 {
		return writeComment(fmt.Sprintf(
			"conflict people who are: %s exist",
			strings.Join(v.UnsortedList(), ", "),
		))
	}

	if assign.Len() > 0 {
		r, err := bot.cli.GetRepo(org, repo)
		if err != nil {
			return err
		}

		members := sets.NewString(r.GetMembers()...)

		if v := assign.Difference(members); v.Len() > 0 {
			msg := fmt.Sprintf(
				"These people( %s ) are not the member of repo.",
				strings.Join(v.List(), ", "),
			)

			writeComment(fmt.Sprintf(
				"The following people can't be added as reviewers of pr with reasons bellow.\n%s",
				msg,
			))

			assign = assign.Difference(v)
		}
	}

	return bot.updatePRReviewers(org, repo, e.GetPullRequest(), assign, unassign)
}

func (bot *robot) updatePRReviewers(org, repo string, pr *sdk.PullRequestHook, assign, unassign sets.String) error {
	current := sets.NewString()
	for _, v := range pr.GetAssignees() {
		current.Insert(v.GetLogin())
	}

	number := pr.GetNumber()
	merr := utils.NewMultiErrors()

	if v := unassign.Intersection(current); v.Len() > 0 {
		err := bot.cli.UnassignPR(org, repo, number, v.UnsortedList())
		if err != nil {
			merr.AddError(err)
		}
	}

	if v := assign.Difference(current); v.Len() > 0 {
		err := bot.cli.AssignPR(org, repo, number, v.UnsortedList())
		if err != nil {
			merr.AddError(err)
		}
	}

	return merr.Err()
}
