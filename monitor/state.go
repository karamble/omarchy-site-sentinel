package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/karamble/omarchy-site-sentinel/check"
)

// Severity is how loudly a site is asking for attention.
type Severity int

const (
	SevOK Severity = iota
	SevInfo
	SevWarn
	SevUrgent
)

func (s Severity) String() string {
	switch s {
	case SevUrgent:
		return "urgent"
	case SevWarn:
		return "warn"
	case SevInfo:
		return "info"
	default:
		return "ok"
	}
}

// SiteState is everything known about one site, across all four tiers.
type SiteState struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url"`
	Host  string `json:"host"`

	// Reachability.
	Up         bool      `json:"up"`
	StatusCode int       `json:"statusCode"`
	ResponseMS int64     `json:"responseMs"`
	Redirects  []string  `json:"redirects,omitempty"`
	ReachError string    `json:"reachError,omitempty"`
	Fails      int       `json:"fails"`
	StateSince time.Time `json:"stateSince,omitzero"`
	LastReach  time.Time `json:"lastReach,omitzero"`
	// Probed is false until the first round.
	Probed bool `json:"probed"`

	// Hosting.
	Addrs          []string  `json:"addrs,omitempty"`
	Reverse        string    `json:"reverse,omitempty"`
	CDN            string    `json:"cdn,omitempty"`
	NS             []string  `json:"ns,omitempty"`
	AddressChanged bool      `json:"addressChanged"`
	NSChanged      bool      `json:"nsChanged"`
	DNSError       string    `json:"dnsError,omitempty"`
	LastDNS        time.Time `json:"lastDns,omitzero"`

	// Certificate.
	CertIssuer     string    `json:"certIssuer,omitempty"`
	CertNotAfter   time.Time `json:"certNotAfter,omitzero"`
	CertValid      bool      `json:"certValid"`
	CertCoversHost bool      `json:"certCoversHost"`
	CertError      string    `json:"certError,omitempty"`
	CertChecked    bool      `json:"certChecked"`
	LastTLS        time.Time `json:"lastTls,omitzero"`

	// Registration.
	Domain        string    `json:"domain,omitempty"`
	DomainExpires time.Time `json:"domainExpires,omitzero"`
	DomainKnown   bool      `json:"domainKnown"`
	DomainStatus  []string  `json:"domainStatus,omitempty"`
	DomainError   string    `json:"domainError,omitempty"`
	LastDomain    time.Time `json:"lastDomain,omitzero"`
}

// Down reports a confirmed outage: probed, not up, and failed enough
// consecutive rounds. Not the same as !Up, which is also true before the first
// probe.
func (s *SiteState) Down() bool {
	return s.Probed && !s.Up && s.Fails >= failuresBeforeDown
}

// Failing reports a site that has missed a round but is not yet down.
func (s *SiteState) Failing() bool {
	return s.Probed && !s.Up && s.Fails > 0 && s.Fails < failuresBeforeDown
}

// CertDaysLeft is negative once the certificate has expired, and meaningless
// unless CertChecked.
func (s *SiteState) CertDaysLeft(now time.Time) int {
	if s.CertNotAfter.IsZero() {
		return 0
	}
	return int(s.CertNotAfter.Sub(now).Hours() / 24)
}

// DomainDaysLeft is meaningless unless DomainKnown.
func (s *SiteState) DomainDaysLeft(now time.Time) int {
	if !s.DomainKnown || s.DomainExpires.IsZero() {
		return 0
	}
	return int(s.DomainExpires.Sub(now).Hours() / 24)
}

// Severity folds every tier into the one value that colours a row.
func (s *SiteState) Severity(now time.Time, certWarn, certUrgent, domainWarn int) Severity {
	if !s.Probed {
		return SevOK
	}
	if s.Down() {
		return SevUrgent
	}
	if s.Failing() {
		return SevWarn
	}
	if s.CertChecked {
		if !s.CertValid || !s.CertCoversHost {
			return SevUrgent
		}
		if d := s.CertDaysLeft(now); d <= certUrgent {
			return SevUrgent
		} else if d <= certWarn {
			return SevWarn
		}
	}
	if s.DomainKnown && s.DomainDaysLeft(now) <= domainWarn {
		return SevWarn
	}
	if s.AddressChanged || s.NSChanged {
		return SevInfo
	}
	return SevOK
}

// Reason is the line a row shows when it is not simply fine.
func (s *SiteState) Reason(now time.Time, certWarn, certUrgent, domainWarn int) string {
	switch {
	case !s.Probed:
		return "not checked yet"
	case s.Failing():
		return "one missed round, not yet confirmed"
	case s.Down() && s.ReachError != "":
		return trimURL(s.ReachError)
	case s.Down():
		return fmt.Sprintf("HTTP %d", s.StatusCode)
	case s.CertChecked && !s.CertCoversHost:
		return "certificate does not cover this host"
	case s.CertChecked && !s.CertValid:
		return s.CertError
	case s.CertChecked && s.CertDaysLeft(now) <= certWarn:
		return fmt.Sprintf("certificate expires in %d days", s.CertDaysLeft(now))
	case s.DomainKnown && s.DomainDaysLeft(now) <= domainWarn:
		return fmt.Sprintf("domain renews in %d days", s.DomainDaysLeft(now))
	case s.NSChanged:
		return "nameservers changed"
	case s.AddressChanged:
		return "address changed"
	}
	return ""
}

// trimURL drops the `Get "<url>": ` prefix the http client puts on its errors,
// since the row already names the host.
func trimURL(err string) string {
	if !strings.HasPrefix(err, "Get \"") {
		return err
	}
	if i := strings.Index(err, "\": "); i > 0 {
		return err[i+3:]
	}
	return err
}

// applyReach folds one HTTP probe into the state, marking the site down only
// after failuresBeforeDown consecutive failures.
func (s *SiteState) applyReach(r check.Reach, expect string, now time.Time) {
	s.LastReach = now
	s.StatusCode = r.StatusCode
	s.ResponseMS = r.Elapsed.Milliseconds()
	s.Redirects = r.Chain
	s.ReachError = ""
	if r.Err != nil {
		s.ReachError = r.Err.Error()
	}

	healthy := r.Up(expect)
	wasUp := s.Up
	firstLook := !s.Probed
	s.Probed = true

	if healthy {
		s.Fails = 0
		s.Up = true
		if !wasUp || firstLook {
			s.StateSince = now
		}
		return
	}

	s.Fails++
	if s.Fails >= failuresBeforeDown {
		s.Up = false
		if wasUp || firstLook {
			s.StateSince = now
		}
	}
}

// applyDNS records where the name points and whether that moved.
func (s *SiteState) applyDNS(h check.Hosting, err error, ns []string, nsErr error, now time.Time) {
	s.LastDNS = now
	if err != nil {
		s.DNSError = err.Error()
		return
	}
	s.DNSError = ""

	addrs := make([]string, 0, len(h.Addrs))
	for _, a := range h.Addrs {
		addrs = append(addrs, a.String())
	}
	sort.Strings(addrs)

	// The first lookup sets the baseline and is not a change.
	if len(s.Addrs) > 0 && !slices.Equal(s.Addrs, addrs) {
		s.AddressChanged = true
	}
	s.Addrs = addrs

	s.CDN = h.CDN
	s.Reverse = ""
	if len(h.Reverse) > 0 {
		s.Reverse = strings.TrimSuffix(h.Reverse[0], ".")
	}

	if nsErr == nil && len(ns) > 0 {
		if len(s.NS) > 0 && !slices.Equal(s.NS, ns) {
			s.NSChanged = true
		}
		s.NS = ns
	}
}

// applyTLS records the certificate a host served. An error means none was
// read at all, since an invalid one arrives as a Cert carrying VerifyErr.
func (s *SiteState) applyTLS(c check.Cert, err error, now time.Time) {
	s.LastTLS = now
	if err != nil {
		s.CertError = err.Error()
		s.CertChecked = false
		return
	}
	s.CertChecked = true
	s.CertIssuer = c.Issuer
	s.CertNotAfter = c.NotAfter
	s.CertValid = c.ChainValid
	s.CertCoversHost = c.CoversHost
	s.CertError = c.VerifyErr
}

// applyDomain records the registration. A failed query sets the error and
// leaves any date already held in place.
func (s *SiteState) applyDomain(name string, d check.Domain, err error, now time.Time) {
	s.LastDomain = now
	s.Domain = name
	if err != nil {
		s.DomainError = err.Error()
		return
	}
	s.DomainError = ""
	s.DomainStatus = d.Status
	if d.Known() {
		s.DomainExpires = d.Expiration
		s.DomainKnown = true
	}
}

// AcknowledgeChanges clears the change flags once they have been reported.
func (s *SiteState) AcknowledgeChanges() {
	s.AddressChanged = false
	s.NSChanged = false
}

// Snapshot is the state as the API and the alert engine read it.
type Snapshot struct {
	Sites      []SiteState        `json:"sites"`
	LocalFault bool               `json:"localFault"`
	FaultSince time.Time          `json:"faultSince,omitempty"`
	Last       map[Tier]time.Time `json:"last"`
	Taken      time.Time          `json:"taken"`
}

// Snapshot copies the current state out from under the lock.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := Snapshot{
		LocalFault: m.round.LocalFault,
		FaultSince: m.round.FaultSince,
		Last:       make(map[Tier]time.Time, len(m.round.Last)),
		Taken:      time.Now().UTC(),
		Sites:      make([]SiteState, 0, len(m.state)),
	}
	for k, v := range m.round.Last {
		out.Last[k] = v
	}
	for _, s := range m.state {
		out.Sites = append(out.Sites, *s)
	}
	sort.Slice(out.Sites, func(i, j int) bool {
		return strings.ToLower(out.Sites[i].Label) < strings.ToLower(out.Sites[j].Label)
	})
	return out
}

// persisted is the on-disk shape of the monitor's memory.
type persisted struct {
	Version int                   `json:"version"`
	Sites   map[string]*SiteState `json:"sites"`
	Round   Round                 `json:"round"`
}

// save writes state atomically. A failure is logged, not propagated.
func (m *Monitor) save() {
	if m.statePath == "" {
		return
	}
	m.mu.RLock()
	doc := persisted{Version: 1, Sites: make(map[string]*SiteState, len(m.state)), Round: m.round}
	for k, v := range m.state {
		dup := *v
		doc.Sites[k] = &dup
	}
	m.mu.RUnlock()

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		m.logger.Warn("encoding state", "err", err)
		return
	}
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.logger.Warn("creating state directory", "dir", dir, "err", err)
		return
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		m.logger.Warn("creating temp state file", "err", err)
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		m.logger.Warn("chmod state file", "err", err)
		return
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		m.logger.Warn("writing state", "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		m.logger.Warn("closing state", "err", err)
		return
	}
	if err := os.Rename(name, m.statePath); err != nil {
		m.logger.Warn("replacing state", "err", err)
	}
}

// load restores state from disk. A missing file is a normal first run.
func (m *Monitor) load() {
	if m.statePath == "" {
		return
	}
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return // no state yet is the normal first run
	}
	var doc persisted
	if err := json.Unmarshal(raw, &doc); err != nil {
		m.logger.Warn("ignoring unreadable state file", "path", m.statePath, "err", err)
		return
	}
	if doc.Sites != nil {
		m.state = doc.Sites
	}
	if doc.Round.Last != nil {
		m.round = doc.Round
	}
}
