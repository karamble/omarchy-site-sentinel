package mcpserver

import (
	"testing"

	"github.com/karamble/omarchy-site-sentinel/alerts"
	"github.com/karamble/omarchy-site-sentinel/monitor"
	"github.com/karamble/omarchy-site-sentinel/sites"
)

// stub builds a Source over fixed values, which is all the row builder reads.
func stub(state []monitor.SiteState, list []sites.Site) Source {
	src := Source{
		State: func() alerts.Snapshot {
			return alerts.Snapshot{State: monitor.Snapshot{Sites: state}, Monitoring: true}
		},
	}
	if list != nil {
		src.Sites = func() []sites.Site { return list }
	}
	return src
}

// State for a site the store no longer lists must not be reported. The monitor
// filters it at the snapshot now, but the row builder is what turned an
// unmatched record into a row claiming to be a paused site, so the exclusion is
// worth holding here too: it is the shape of the bug, not just one instance.
func TestRowsLeaveOutStateWithNoSite(t *testing.T) {
	src := stub(
		[]monitor.SiteState{
			{ID: "live", Label: "Live", Host: "live.example"},
			{ID: "ghost", Label: "Ghost", Host: "ghost.example"},
		},
		[]sites.Site{{ID: "live", URL: "https://live.example", Enabled: true}},
	)

	got := rows(src)
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].ID != "live" {
		t.Fatalf("want the live site, got %q", got[0].ID)
	}
}

// A Source without a site list has nothing to check against. Emitting nothing
// would be worse than emitting everything: the tools would report a site-less
// installation rather than admit they cannot tell.
func TestRowsWithoutASiteListStillEmit(t *testing.T) {
	src := stub([]monitor.SiteState{{ID: "a", Label: "A"}}, nil)

	if got := rows(src); len(got) != 1 {
		t.Fatalf("want 1 row with a nil Sites, got %d", len(got))
	}
}

// resolve reads through rows, so an id that only exists in monitor state must
// not resolve. It did, which let the remove and edit tools accept an id the
// store had already forgotten -- and answer removed:false having found it.
func TestResolveRefusesASiteTheStoreDoesNotHave(t *testing.T) {
	src := stub(
		[]monitor.SiteState{
			{ID: "live", Label: "Live", Host: "live.example"},
			{ID: "ghost", Label: "Ghost", Host: "ghost.example"},
		},
		[]sites.Site{{ID: "live", URL: "https://live.example", Enabled: true}},
	)

	if _, ok := resolve(src, "ghost"); ok {
		t.Fatal("resolved an id the store does not have")
	}
	if _, ok := resolve(src, "ghost.example"); ok {
		t.Fatal("resolved a hostname the store does not have")
	}
	if id, ok := resolve(src, "live.example"); !ok || id != "live" {
		t.Fatalf("want live resolved by hostname, got %q ok=%v", id, ok)
	}
}

// The enabled flag comes from the store, not from state, so a paused site is
// still listed -- pausing and removing have to stay distinguishable.
func TestRowsReportPausedSitesAsPresent(t *testing.T) {
	src := stub(
		[]monitor.SiteState{{ID: "off", Label: "Off", Host: "off.example"}},
		[]sites.Site{{ID: "off", URL: "https://off.example", Enabled: false}},
	)

	got := rows(src)
	if len(got) != 1 {
		t.Fatalf("want the paused site listed, got %d rows", len(got))
	}
	if got[0].Enabled {
		t.Fatal("want enabled=false for a paused site")
	}
}
