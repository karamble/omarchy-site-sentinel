package monitor

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/karamble/omarchy-site-sentinel/sites"
)

// withState builds a monitor holding state for the given ids, with no state
// file behind it so nothing is written. store may be nil, which is the case
// Snapshot has to tolerate.
func withState(store func() *sites.Store, ids ...string) *Monitor {
	m := &Monitor{state: make(map[string]*SiteState), store: store}
	for _, id := range ids {
		m.state[id] = &SiteState{ID: id, Label: id}
	}
	return m
}

func storeWith(ids ...string) func() *sites.Store {
	st := &sites.Store{}
	for _, id := range ids {
		st.Sites = append(st.Sites, sites.Site{ID: id, URL: "https://" + id + ".example"})
	}
	return func() *sites.Store { return st }
}

// The reconciliation that clears records an earlier removal left behind.
func TestPruneDropsStateWithNoSite(t *testing.T) {
	m := withState(nil, "keep", "ghost", "alsoGhost")

	dropped := m.Prune(map[string]bool{"keep": true})
	if dropped != 2 {
		t.Fatalf("want 2 dropped, got %d", dropped)
	}
	if _, ok := m.state["keep"]; !ok {
		t.Fatal("pruned a site that is still watched")
	}
	if len(m.state) != 1 {
		t.Fatalf("want 1 record left, got %d", len(m.state))
	}
}

// A clean start must not report work it did not do, so the caller can stay
// quiet rather than logging on every boot.
func TestPruneReportsNothingWhenNothingIsOrphaned(t *testing.T) {
	m := withState(nil, "a", "b")

	if dropped := m.Prune(map[string]bool{"a": true, "b": true}); dropped != 0 {
		t.Fatalf("want 0 dropped, got %d", dropped)
	}
	if len(m.state) != 2 {
		t.Fatalf("want both kept, got %d", len(m.state))
	}
}

// Snapshot is the choke point every consumer reads through: the dashboard, the
// MCP tools, health and the alert engine. State with no site behind it must not
// reach any of them.
func TestSnapshotLeavesOutStateWithNoSite(t *testing.T) {
	m := withState(storeWith("live"), "live", "ghost")

	snap := m.Snapshot()
	if len(snap.Sites) != 1 {
		t.Fatalf("want 1 site, got %d", len(snap.Sites))
	}
	if snap.Sites[0].ID != "live" {
		t.Fatalf("want live, got %q", snap.Sites[0].ID)
	}
}

// Without a store there is nothing to check against, and claiming the
// installation has no sites would be a worse answer than showing what state
// there is.
func TestSnapshotWithoutAStoreEmitsEverything(t *testing.T) {
	m := withState(nil, "a", "b")

	if snap := m.Snapshot(); len(snap.Sites) != 2 {
		t.Fatalf("want both sites with a nil store, got %d", len(snap.Sites))
	}
}

// Pausing a site keeps it in the store, so it must still be reported. If this
// ever fails, pausing and removing have become indistinguishable.
func TestSnapshotKeepsPausedSites(t *testing.T) {
	st := &sites.Store{Sites: []sites.Site{{ID: "off", URL: "https://off.example", Enabled: false}}}
	m := withState(func() *sites.Store { return st }, "off")

	if snap := m.Snapshot(); len(snap.Sites) != 1 {
		t.Fatalf("want the paused site reported, got %d", len(snap.Sites))
	}
}

// A round already running holds the site list from when it started, so a site
// removed mid-round still gets probed and ensure() puts its record back after
// Forget deleted it. save() must not commit that to disk, or every removal
// raced by a round leaves an orphan behind -- which is the bug this whole
// change exists to end, arriving by a second route.
func TestSaveLeavesOutStateWithNoSite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	m := withState(storeWith("live"), "live", "removedMidRound")
	m.statePath = path
	m.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	m.save()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	var doc struct {
		Sites map[string]*SiteState `json:"sites"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing state: %v", err)
	}

	if _, ok := doc.Sites["removedMidRound"]; ok {
		t.Fatal("wrote state for a site the store does not have")
	}
	if _, ok := doc.Sites["live"]; !ok {
		t.Fatal("dropped the site that is still watched")
	}
}
