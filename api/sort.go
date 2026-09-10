package api

import (
	"sort"
	"strings"
)

// severityRank orders the words the API emits, worst first.
var severityRank = map[string]int{"urgent": 0, "warn": 1, "info": 2, "ok": 3}

// sortRows orders worst first, then by name.
func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := severityRank[rows[i].Severity], severityRank[rows[j].Severity]
		if a != b {
			return a < b
		}
		return strings.ToLower(rows[i].Label) < strings.ToLower(rows[j].Label)
	})
}
