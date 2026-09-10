package mcpserver

import (
	"sort"
	"strings"
)

func equalFold(a, b string) bool { return strings.EqualFold(a, b) }

// sortByCert orders invalid certificates first, then by days remaining.
func sortByCert(rows []siteRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		bi := rows[i].CertValid != nil && !*rows[i].CertValid
		bj := rows[j].CertValid != nil && !*rows[j].CertValid
		if bi != bj {
			return bi
		}
		return *rows[i].CertDaysLeft < *rows[j].CertDaysLeft
	})
}

// sortByDomain orders soonest renewal first, unpublished expiry last.
func sortByDomain(rows []siteRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if (rows[i].DomainDaysLeft == nil) != (rows[j].DomainDaysLeft == nil) {
			return rows[i].DomainDaysLeft != nil
		}
		if rows[i].DomainDaysLeft == nil {
			return false
		}
		return *rows[i].DomainDaysLeft < *rows[j].DomainDaysLeft
	})
}
