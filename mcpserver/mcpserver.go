// Package mcpserver exposes Site Sentinel over the Model Context Protocol.
//
// Read tools return the sentinel's own observations. Write tools change the
// site list and the trigger store, and nothing else.
package mcpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/omarchy-site-sentinel/alerts"
	"github.com/karamble/omarchy-site-sentinel/monitor"
	"github.com/karamble/omarchy-site-sentinel/sites"
)

// Source is everything the tools read and write.
type Source struct {
	// State is the alert-facing snapshot, thresholds already folded in.
	State func() alerts.Snapshot
	// Sites is the configured list, including sites switched off.
	Sites func() []sites.Site
	// Mutate applies a change to the store and persists it.
	Mutate func(func(*sites.Store) error) error
	// Forget drops a removed site's monitor state. Mutate's counterpart:
	// taking a site out of the store is half of forgetting it, and without
	// this the record left in state.json is an orphan no code path can reach
	// -- the listing still shows it, and removing it again finds nothing in
	// the store to remove.
	Forget func(string)
	// Monitoring reports the master switch.
	Monitoring func() bool
	// Alerts resolves the trigger engine per call, because the handler is built
	// before the engine exists.
	Alerts func() Alerts
}

// alerts resolves the engine, reporting false when there is not one to write to.
func (s Source) alerts() (Alerts, bool) {
	if s.Alerts == nil {
		return nil, false
	}
	a := s.Alerts()
	if a == nil {
		return nil, false
	}
	return a, true
}

// Handler builds the streamable HTTP handler to mount on the daemon's listener.
func Handler(src Source, version string) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "sitesentinel",
		Title:   "Site Sentinel: sites, certificates and domains",
		Version: version,
	}, nil)
	register(server, src)
	registerAlerts(server, src)

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
}

type empty struct{}

// status rides along with every answer: whether checking is on, and whether
// this machine has connectivity.
type status struct {
	Monitoring bool      `json:"monitoring"`
	LocalFault bool      `json:"localFault"`
	Checked    time.Time `json:"lastChecked,omitzero"`
	Note       string    `json:"note,omitempty"`
}

func (s Source) status() status {
	snap := s.State()
	out := status{
		Monitoring: s.Monitoring == nil || s.Monitoring(),
		LocalFault: snap.State.LocalFault,
		Checked:    snap.State.Last[monitor.TierReach],
	}
	switch {
	case !out.Monitoring:
		out.Note = "the sentinel is asleep: nothing is being checked, this is the last known state"
	case out.LocalFault:
		out.Note = "this machine has no usable connectivity, so nothing here is evidence about any site"
	}
	return out
}

// siteRow is what every read tool returns for a site: the observations plus the
// derived day counts.
type siteRow struct {
	ID       string `json:"id"`
	Site     string `json:"site"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	Enabled  bool   `json:"enabled"`
	Severity string `json:"severity"`
	Reason   string `json:"reason,omitempty"`

	Up         bool      `json:"up"`
	Probed     bool      `json:"probed"`
	StatusCode int       `json:"statusCode,omitempty"`
	ResponseMS int64     `json:"responseMs,omitempty"`
	Since      time.Time `json:"since,omitzero"`

	Addrs   []string `json:"addrs,omitempty"`
	Reverse string   `json:"reverse,omitempty"`
	CDN     string   `json:"cdn,omitempty"`
	NS      []string `json:"nameservers,omitempty"`

	CertIssuer   string    `json:"certIssuer,omitempty"`
	CertExpires  time.Time `json:"certExpires,omitzero"`
	CertDaysLeft *int      `json:"certDaysLeft,omitempty"`
	CertValid    *bool     `json:"certValid,omitempty"`
	CertError    string    `json:"certError,omitempty"`

	Domain         string    `json:"domain,omitempty"`
	DomainExpires  time.Time `json:"domainExpires,omitzero"`
	DomainDaysLeft *int      `json:"domainDaysLeft,omitempty"`
	// DomainUnknown says the registry publishes no expiry. Not the same as an
	// imminent expiry.
	DomainUnknown bool     `json:"domainExpiryUnknown,omitempty"`
	DomainStatus  []string `json:"domainStatus,omitempty"`
	DomainError   string   `json:"domainError,omitempty"`
}

func rows(src Source) []siteRow {
	snap := src.State()
	now := snap.TakenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// known is separate from enabled on purpose. A site the store has but has
	// switched off is known and disabled; a site the store does not have at all
	// is neither, and reading it out of enabled alone gave it the same answer as
	// a paused site -- which is how a removed site went on being listed.
	//
	// Monitor.Snapshot already drops unmatched state, so in the daemon this is
	// the second of two guards. It is worth having because resolve() reads these
	// rows to turn a caller's argument into an id, and that feeds remove and
	// edit: a write path should not be safe only because its caller filtered.
	enabled := map[string]bool{}
	known := map[string]bool{}
	filter := src.Sites != nil
	if filter {
		for _, s := range src.Sites() {
			enabled[s.ID] = s.Enabled
			known[s.ID] = true
		}
	}

	out := make([]siteRow, 0, len(snap.State.Sites))
	for i := range snap.State.Sites {
		st := &snap.State.Sites[i]
		if filter && !known[st.ID] {
			continue
		}
		row := siteRow{
			ID:       st.ID,
			Site:     st.Label,
			URL:      st.URL,
			Host:     st.Host,
			Enabled:  enabled[st.ID],
			Severity: st.Severity(now, snap.CertWarn, snap.CertUrgent, snap.DomainWarn).String(),
			Reason:   st.Reason(now, snap.CertWarn, snap.CertUrgent, snap.DomainWarn),

			Up: st.Up, Probed: st.Probed,
			StatusCode: st.StatusCode, ResponseMS: st.ResponseMS, Since: st.StateSince,
			Addrs: st.Addrs, Reverse: st.Reverse, CDN: st.CDN, NS: st.NS,
			Domain: st.Domain, DomainStatus: st.DomainStatus, DomainError: st.DomainError,
		}
		if st.CertChecked {
			days := st.CertDaysLeft(now)
			valid := st.CertValid && st.CertCoversHost
			row.CertIssuer, row.CertExpires = st.CertIssuer, st.CertNotAfter
			row.CertDaysLeft, row.CertValid, row.CertError = &days, &valid, st.CertError
		}
		if st.DomainKnown {
			days := st.DomainDaysLeft(now)
			row.DomainExpires, row.DomainDaysLeft = st.DomainExpires, &days
		} else if st.Domain != "" {
			row.DomainUnknown = true
		}
		out = append(out, row)
	}
	return out
}

type sitesOut struct {
	Status status    `json:"status"`
	Count  int       `json:"count"`
	Sites  []siteRow `json:"sites"`
}

type siteOut struct {
	Status status   `json:"status"`
	Site   *siteRow `json:"site,omitempty"`
	Error  string   `json:"error,omitempty"`
}

type healthOut struct {
	Status  status                     `json:"status"`
	Watched int                        `json:"watched"`
	Down    int                        `json:"down"`
	Last    map[monitor.Tier]time.Time `json:"lastChecked"`
}

type siteIn struct {
	Site string `json:"site" jsonschema:"the site id, host or name, as returned by sentinel_sites"`
}

type addIn struct {
	URL         string `json:"url" jsonschema:"required: the address to watch, scheme included, such as https://example.com"`
	Name        string `json:"name,omitempty" jsonschema:"what to call it in the panel; defaults to the hostname"`
	Expect      string `json:"expect,omitempty" jsonschema:"a string the page must contain; without it a 200 carrying an error page reads as healthy"`
	Domain      string `json:"domain,omitempty" jsonschema:"the registrable domain to ask the registry about, when it differs from the hostname"`
	CheckTLS    *bool  `json:"checkTLS,omitempty" jsonschema:"whether to read the certificate; defaults to yes for https"`
	CheckDomain *bool  `json:"checkDomain,omitempty" jsonschema:"whether to ask the registry about renewal; defaults to yes"`
}

type editSiteIn struct {
	Site        string `json:"site" jsonschema:"the site id, host or name to change"`
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Expect      string `json:"expect,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty" jsonschema:"false stops checking it without forgetting it"`
	CheckTLS    *bool  `json:"checkTLS,omitempty"`
	CheckDomain *bool  `json:"checkDomain,omitempty"`
}

type removeIn struct {
	Site string `json:"site" jsonschema:"the site id, host or name to stop watching"`
}

func register(s *mcp.Server, src Source) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_sites",
		Description: "Every watched site with its current state: up or down, response time, " +
			"where it resolves, certificate expiry and domain renewal. Worst first.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, sitesOut, error) {
		list := rows(src)
		return nil, sitesOut{Status: src.status(), Count: len(list), Sites: list}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "sentinel_site",
		Description: "One site in full, found by id, hostname or name.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in siteIn) (*mcp.CallToolResult, siteOut, error) {
		for _, row := range rows(src) {
			if matches(row, in.Site) {
				return nil, siteOut{Status: src.status(), Site: &row}, nil
			}
		}
		return nil, siteOut{Status: src.status(), Error: "no watched site matches " + in.Site}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_certificates",
		Description: "Certificates for every https site, soonest to expire first. " +
			"A certificate can be invalid while far from expiry: check certValid, not only certDaysLeft.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, sitesOut, error) {
		var out []siteRow
		for _, row := range rows(src) {
			if row.CertDaysLeft != nil {
				out = append(out, row)
			}
		}
		sortByCert(out)
		return nil, sitesOut{Status: src.status(), Count: len(out), Sites: out}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_domains",
		Description: "Domain registrations, soonest to renew first. A site with " +
			"domainExpiryUnknown set has a registry that publishes no expiry date; that is not an imminent expiry.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, sitesOut, error) {
		var out []siteRow
		for _, row := range rows(src) {
			if row.Domain != "" {
				out = append(out, row)
			}
		}
		sortByDomain(out)
		return nil, sitesOut{Status: src.status(), Count: len(out), Sites: out}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_health",
		Description: "Whether the sentinel is awake, when each tier last ran, and whether " +
			"this machine currently has connectivity at all.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, healthOut, error) {
		snap := src.State()
		down := 0
		for i := range snap.State.Sites {
			if snap.State.Sites[i].Down() {
				down++
			}
		}
		return nil, healthOut{
			Status:  src.status(),
			Watched: len(snap.State.Sites),
			Down:    down,
			Last:    snap.State.Last,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_add_site",
		Description: "Watch a new site. A URL already in the list is rejected as a duplicate. " +
			"This changes the sentinel's own list and touches nothing on the site itself.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, siteOut, error) {
		if src.Mutate == nil {
			return nil, siteOut{Status: src.status(), Error: "this daemon cannot change its site list"}, nil
		}
		err := src.Mutate(func(st *sites.Store) error {
			_, err := st.Add(sites.Site{
				Name: in.Name, URL: in.URL, Expect: in.Expect, Domain: in.Domain,
				Enabled: true, CheckTLS: in.CheckTLS, CheckDomain: in.CheckDomain,
			})
			return err
		})
		if err != nil {
			return nil, siteOut{Status: src.status(), Error: err.Error()}, nil
		}
		for _, row := range rows(src) {
			if row.URL == in.URL {
				return nil, siteOut{Status: src.status(), Site: &row}, nil
			}
		}
		// Added but not yet probed.
		return nil, siteOut{Status: src.status(), Site: &siteRow{URL: in.URL, Enabled: true}}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_edit_site",
		Description: "Change a watched site. Omitted fields keep their current value. " +
			"Set enabled false to stop checking a site without forgetting its history.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in editSiteIn) (*mcp.CallToolResult, siteOut, error) {
		if src.Mutate == nil {
			return nil, siteOut{Status: src.status(), Error: "this daemon cannot change its site list"}, nil
		}
		id, ok := resolve(src, in.Site)
		if !ok {
			return nil, siteOut{Status: src.status(), Error: "no watched site matches " + in.Site}, nil
		}
		err := src.Mutate(func(st *sites.Store) error {
			_, err := st.Edit(id, func(site *sites.Site) {
				if in.Name != "" {
					site.Name = in.Name
				}
				if in.URL != "" {
					site.URL = in.URL
				}
				if in.Expect != "" {
					site.Expect = in.Expect
				}
				if in.Domain != "" {
					site.Domain = in.Domain
				}
				if in.Enabled != nil {
					site.Enabled = *in.Enabled
				}
				if in.CheckTLS != nil {
					site.CheckTLS = in.CheckTLS
				}
				if in.CheckDomain != nil {
					site.CheckDomain = in.CheckDomain
				}
			})
			return err
		})
		if err != nil {
			return nil, siteOut{Status: src.status(), Error: err.Error()}, nil
		}
		for _, row := range rows(src) {
			if row.ID == id {
				return nil, siteOut{Status: src.status(), Site: &row}, nil
			}
		}
		return nil, siteOut{Status: src.status()}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "sentinel_remove_site",
		Description: "Stop watching a site and forget its history. To pause one instead, " +
			"use sentinel_edit_site with enabled false.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in removeIn) (*mcp.CallToolResult, disarmOut, error) {
		if src.Mutate == nil {
			return nil, disarmOut{Status: src.status()}, nil
		}
		id, ok := resolve(src, in.Site)
		if !ok {
			return nil, disarmOut{Status: src.status(), ID: in.Site}, nil
		}
		removed := false
		err := src.Mutate(func(st *sites.Store) error {
			removed = st.Remove(id)
			return nil
		})
		if err != nil {
			return nil, disarmOut{Status: src.status(), ID: id}, err
		}
		// After Mutate returns, not inside it: the store write holds the
		// server's lock, and this is the ordering the HTTP handler already
		// uses. Guarded like the alert engine, so a Source built without a
		// Forget still works.
		if removed && src.Forget != nil {
			src.Forget(id)
		}
		return nil, disarmOut{Status: src.status(), Removed: removed, ID: id}, nil
	})
}

// matches accepts an id, hostname, name or URL.
func matches(row siteRow, want string) bool {
	return row.ID == want ||
		equalFold(row.Host, want) ||
		equalFold(row.Site, want) ||
		equalFold(row.URL, want)
}

func resolve(src Source, want string) (string, bool) {
	for _, row := range rows(src) {
		if matches(row, want) {
			return row.ID, true
		}
	}
	// A site added but never probed has no row yet.
	if src.Sites != nil {
		for _, s := range src.Sites() {
			if s.ID == want || equalFold(s.Host(), want) || equalFold(s.Label(), want) || equalFold(s.URL, want) {
				return s.ID, true
			}
		}
	}
	return "", false
}
