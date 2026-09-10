package api

import (
	"time"

	"github.com/karamble/omarchy-site-sentinel/monitor"
)

// Demo returns a fabricated dashboard for the preview screenshot, built from
// the structs the real handler serves. Every name is a reserved example domain
// (RFC 2606).
func Demo() any {
	now := time.Now().UTC()
	day := 24 * time.Hour

	type spec struct {
		label       string
		host        string
		up          bool
		fails       int
		status      int
		ms          int64
		reachErr    string
		addrs       []string
		reverse     string
		cdn         string
		certDays    int
		certValid   bool
		certCovers  bool
		certIssuer  string
		certErr     string
		certKnown   bool
		domain      string
		domainDays  int
		domainKnown bool
		enabled     bool
		since       time.Duration
	}

	specs := []spec{
		{
			label: "Storefront", host: "shop.example.com", up: false, fails: 3, ms: 0,
			reachErr: `Get "https://shop.example.com": dial tcp: connect: connection refused`,
			addrs:    []string{"203.0.113.24"}, reverse: "web01.example.net",
			certKnown: true, certDays: 61, certValid: true, certCovers: true, certIssuer: "R11",
			domain: "example.com", domainDays: 213, domainKnown: true, enabled: true, since: 47 * time.Minute,
		},
		{
			label: "Client portal", host: "portal.example.org", up: true, status: 200, ms: 412,
			addrs: []string{"203.0.113.44"}, reverse: "web02.example.net",
			certKnown: true, certDays: 5, certValid: true, certCovers: true, certIssuer: "R11",
			domain: "example.org", domainDays: 96, domainKnown: true, enabled: true, since: 31 * day,
		},
		{
			label: "Legacy intranet", host: "old.example.net", up: true, status: 200, ms: 738,
			addrs:     []string{"198.51.100.9"},
			certKnown: true, certDays: 3287, certValid: false, certCovers: false, certIssuer: "default",
			certErr: "x509: certificate is not valid for any names",
			domain:  "example.net", domainDays: 51, domainKnown: true, enabled: true, since: 12 * day,
		},
		{
			label: "Marketing site", host: "www.example.com", up: true, status: 200, ms: 189,
			addrs: []string{"203.0.113.8"}, cdn: "Cloudflare",
			certKnown: true, certDays: 74, certValid: true, certCovers: true, certIssuer: "E6",
			domain: "example.com", domainDays: 213, domainKnown: true, enabled: true, since: 64 * day,
		},
		{
			label: "Documentation", host: "docs.example.org", up: true, status: 200, ms: 264,
			addrs: []string{"203.0.113.44"}, reverse: "web02.example.net",
			certKnown: true, certDays: 44, certValid: true, certCovers: true, certIssuer: "R11",
			domain: "example.org", domainDays: 96, domainKnown: true, enabled: true, since: 64 * day,
		},
		{
			label: "Partner API", host: "api.example.de", up: true, status: 200, ms: 331,
			addrs: []string{"198.51.100.21"}, reverse: "api01.example.net",
			certKnown: true, certDays: 58, certValid: true, certCovers: true, certIssuer: "R11",
			// A registry that publishes no expiry: unknown, not urgent.
			domain: "example.de", domainKnown: false, enabled: true, since: 90 * day,
		},
		{
			label: "Staging", host: "staging.example.com", enabled: false,
			addrs: []string{"203.0.113.24"}, domain: "example.com",
		},
	}

	rows := make([]Row, 0, len(specs))
	for _, s := range specs {
		st := monitor.SiteState{
			ID: s.host, Label: s.label, Host: s.host, URL: "https://" + s.host,
			Up: s.up, Probed: s.enabled, StatusCode: s.status, ResponseMS: s.ms,
			ReachError: s.reachErr, Fails: s.fails,
			Addrs: s.addrs, Reverse: s.reverse, CDN: s.cdn,
			Domain: s.domain, DomainKnown: s.domainKnown,
			LastReach: now, LastDNS: now, LastTLS: now, LastDomain: now,
		}
		if s.since > 0 {
			st.StateSince = now.Add(-s.since)
		}
		if s.certKnown {
			st.CertChecked = true
			st.CertIssuer = s.certIssuer
			st.CertNotAfter = now.Add(time.Duration(s.certDays) * day)
			st.CertValid = s.certValid
			st.CertCoversHost = s.certCovers
			st.CertError = s.certErr
		}
		if s.domainKnown {
			st.DomainExpires = now.Add(time.Duration(s.domainDays) * day)
		}

		sev := st.Severity(now, 14, 7, 30)
		rows = append(rows, Row{
			SiteState:  st,
			Severity:   sev.String(),
			Reason:     st.Reason(now, 14, 7, 30),
			CertDays:   st.CertDaysLeft(now),
			DomainDays: st.DomainDaysLeft(now),
			Enabled:    s.enabled,
		})
	}
	sortRows(rows)

	return dashboardResponse{
		Health: healthResponse{
			Status: "ok", Version: "demo", Uptime: "6h12m",
			Sites: len(specs), Enabled: len(specs) - 1, Down: 1,
			Monitoring: true, MCPEnabled: false, LocalFault: false,
			CertWarn: 14, CertUrgent: 7, DomainWarn: 30,
			Last: map[monitor.Tier]time.Time{
				monitor.TierReach:  now.Add(-2 * time.Minute),
				monitor.TierDNS:    now.Add(-11 * time.Minute),
				monitor.TierTLS:    now.Add(-3 * time.Hour),
				monitor.TierDomain: now.Add(-9 * time.Hour),
			},
		},
		Rows:   rows,
		Worst:  "urgent",
		Alerts: 2,
	}
}
