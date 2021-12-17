package main

import (
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	assignRe       = regexp.MustCompile(`(?mi)^/(un)?assign(( @?[-\w]+?)*)\s*$`)
	collaboratorRe = regexp.MustCompile(`(?mi)^/(add|rm)-collaborator(( @?[-\w]+?)*)\s*$`)
)

func parseAssignCmd(comment, commenter string) (sets.String, sets.String) {
	return parseCmd(
		assignRe,
		func(s string) bool { return s == "" },
		comment,
		commenter,
	)
}

func parseAssignIssueCollaboratorsCmd(comment, commenter string) (sets.String, sets.String) {
	return parseCmd(
		collaboratorRe,
		func(s string) bool { return s == "add" },
		comment,
		commenter,
	)
}

func parseCmd(exp *regexp.Regexp, isAdd func(string) bool, comment, commenter string) (sets.String, sets.String) {
	assign := sets.NewString()
	unassign := sets.NewString()

	f := func(action string, v ...string) {
		if isAdd(action) {
			assign.Insert(v...)
		} else {
			unassign.Insert(v...)
		}
	}

	matches := exp.FindAllStringSubmatch(comment, -1)
	for _, re := range matches {
		if re[2] == "" {
			f(re[1], commenter)
		} else {
			f(re[1], parseLogins(re[2])...)
		}
	}

	return assign, unassign
}

func parseLogins(text string) []string {
	var parts []string
	for _, s := range strings.Split(text, " ") {
		if v := strings.Trim(s, "@ "); v != "" {
			parts = append(parts, v)
		}
	}
	return parts
}
