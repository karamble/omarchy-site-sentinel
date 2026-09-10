// Package sites stores the watched site list and the bearer token guarding the
// daemon's API. The file is written 0600.
package sites

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrNotConfigured reports that no store exists yet.
var ErrNotConfigured = errors.New("no site store: run the sentinel daemon once to create one")

// Site is one watched site.
type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"` // what the panel shows; defaults to Host
	URL  string `json:"url"`  // the address probed, scheme included

	// Expect is a string the response body must contain.
	Expect string `json:"expect,omitempty"`

	// Domain overrides the registrable name asked about over RDAP. Empty
	// derives it from the URL.
	Domain string `json:"domain,omitempty"`

	Enabled bool `json:"enabled"`

	// CheckTLS and CheckDomain opt a site out of a tier. Nil decides from the
	// URL scheme.
	CheckTLS    *bool `json:"checkTLS,omitempty"`
	CheckDomain *bool `json:"checkDomain,omitempty"`

	Added time.Time `json:"added"`
}

// Host is the hostname probed, without port or path.
func (s Site) Host() string {
	u, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Label is what the panel draws: the given name, or the host.
func (s Site) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Host()
}

// TLSWanted reports whether the certificate tier applies to this site.
func (s Site) TLSWanted() bool {
	if s.CheckTLS != nil {
		return *s.CheckTLS
	}
	return strings.HasPrefix(strings.ToLower(s.URL), "https://")
}

// DomainWanted reports whether the registration tier applies.
func (s Site) DomainWanted() bool {
	if s.CheckDomain != nil {
		return *s.CheckDomain
	}
	return true
}

// Addr is the host:port to dial for TLS, defaulting to 443.
func (s Site) Addr() string {
	u, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	if p := u.Port(); p != "" {
		return u.Hostname() + ":" + p
	}
	return u.Hostname() + ":443"
}

// Validate rejects a site that cannot be probed.
func (s Site) Validate() error {
	if strings.TrimSpace(s.URL) == "" {
		return errors.New("a site needs a URL")
	}
	u, err := url.Parse(s.URL)
	if err != nil {
		return fmt.Errorf("cannot parse %q: %w", s.URL, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "":
		return fmt.Errorf("%q has no scheme: write https://%s", s.URL, s.URL)
	default:
		return fmt.Errorf("%q: only http and https can be probed", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%q has no host", s.URL)
	}
	return nil
}

// Store is the on-disk configuration. The zero value is usable: Save writes it
// with a freshly generated APIToken if one is missing.
type Store struct {
	Version  int    `json:"version"`
	APIToken string `json:"apiToken"`
	Sites    []Site `json:"sites"`

	// Monitoring is the master switch. Nil reads as on.
	Monitoring *bool `json:"monitoring,omitempty"`

	// MCPEnabled exposes the MCP endpoint. Nil reads as off.
	MCPEnabled *bool `json:"mcpEnabled,omitempty"`

	// Cadences in minutes, per tier. Nil means the package default.
	ReachMin  *int `json:"reachMin,omitempty"`
	DNSMin    *int `json:"dnsMin,omitempty"`
	TLSMin    *int `json:"tlsMin,omitempty"`
	DomainMin *int `json:"domainMin,omitempty"`

	// Certificate warning thresholds in days.
	CertWarnDays   *int `json:"certWarnDays,omitempty"`
	CertUrgentDays *int `json:"certUrgentDays,omitempty"`

	// DomainWarnDays is how far ahead a renewal is worth mentioning.
	DomainWarnDays *int `json:"domainWarnDays,omitempty"`

	path string
}

// Defaults, in the units of the fields above.
const (
	DefaultReachMin  = 5
	DefaultDNSMin    = 30
	DefaultTLSMin    = 360
	DefaultDomainMin = 1440

	DefaultCertWarnDays   = 14
	DefaultCertUrgentDays = 7
	DefaultDomainWarnDays = 30
)

func value(p *int, fallback int) int {
	if p == nil || *p <= 0 {
		return fallback
	}
	return *p
}

func (s *Store) ReachEvery() time.Duration {
	return time.Duration(value(s.ReachMin, DefaultReachMin)) * time.Minute
}
func (s *Store) DNSEvery() time.Duration {
	return time.Duration(value(s.DNSMin, DefaultDNSMin)) * time.Minute
}
func (s *Store) TLSEvery() time.Duration {
	return time.Duration(value(s.TLSMin, DefaultTLSMin)) * time.Minute
}

// DomainEvery is floored at 24 hours, because registries rate limit.
func (s *Store) DomainEvery() time.Duration {
	d := time.Duration(value(s.DomainMin, DefaultDomainMin)) * time.Minute
	if d < 24*time.Hour {
		return 24 * time.Hour
	}
	return d
}

func (s *Store) CertWarn() int   { return value(s.CertWarnDays, DefaultCertWarnDays) }
func (s *Store) CertUrgent() int { return value(s.CertUrgentDays, DefaultCertUrgentDays) }
func (s *Store) DomainWarn() int { return value(s.DomainWarnDays, DefaultDomainWarnDays) }

// MonitoringOn reports the master switch, defaulting to on.
func (s *Store) MonitoringOn() bool { return s.Monitoring == nil || *s.Monitoring }

// MCPOn reports whether the MCP endpoint is served, defaulting to off.
func (s *Store) MCPOn() bool { return s.MCPEnabled != nil && *s.MCPEnabled }

// Enabled returns the sites to probe. Empty means make no network request.
func (s *Store) Enabled() []Site {
	out := make([]Site, 0, len(s.Sites))
	for _, site := range s.Sites {
		if site.Enabled {
			out = append(out, site)
		}
	}
	return out
}

// Find returns the site with the given id.
func (s *Store) Find(id string) (Site, bool) {
	for _, site := range s.Sites {
		if site.ID == id {
			return site, true
		}
	}
	return Site{}, false
}

// Add appends a validated site, assigning an id and rejecting a duplicate URL.
func (s *Store) Add(site Site) (Site, error) {
	if err := site.Validate(); err != nil {
		return Site{}, err
	}
	for _, existing := range s.Sites {
		if strings.EqualFold(existing.URL, site.URL) {
			return Site{}, fmt.Errorf("%s is already watched as %q", site.URL, existing.Label())
		}
	}
	if site.ID == "" {
		id, err := NewID()
		if err != nil {
			return Site{}, err
		}
		site.ID = id
	}
	if site.Added.IsZero() {
		site.Added = time.Now().UTC()
	}
	s.Sites = append(s.Sites, site)
	return site, nil
}

// Edit applies a change to one site, validating the result before keeping it.
func (s *Store) Edit(id string, apply func(*Site)) (Site, error) {
	for i := range s.Sites {
		if s.Sites[i].ID != id {
			continue
		}
		draft := s.Sites[i]
		apply(&draft)
		if err := draft.Validate(); err != nil {
			return Site{}, err
		}
		s.Sites[i] = draft
		return draft, nil
	}
	return Site{}, fmt.Errorf("no site with id %s", id)
}

// Remove deletes one site, reporting whether it was there.
func (s *Store) Remove(id string) bool {
	before := len(s.Sites)
	s.Sites = slices.DeleteFunc(s.Sites, func(site Site) bool { return site.ID == id })
	return len(s.Sites) != before
}

// DefaultPath is the store's location in the plugin's config directory.
func DefaultPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "sitesentinel", "sites.json")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "sitesentinel", "sites.json")
}

// Dir is the config directory holding the store and the daemon's state.
func Dir() string { return filepath.Dir(DefaultPath()) }

// NewAPIToken mints the bearer token guarding the daemon's API.
func NewAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating api token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewID mints a site identifier.
func NewID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Load reads the store, refusing one whose mode is wider than 0600.
func Load(path string) (*Store, error) {
	if path == "" {
		path = DefaultPath()
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("%s is mode %04o, want 0600: it holds the api token", path, perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	s.path = path
	return &s, nil
}

// Save writes the store atomically at 0600, minting an APIToken if absent.
func (s *Store) Save() error {
	if s.path == "" {
		s.path = DefaultPath()
	}
	if s.Version == 0 {
		s.Version = 1
	}
	if s.APIToken == "" {
		tok, err := NewAPIToken()
		if err != nil {
			return err
		}
		s.APIToken = tok
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding store: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, ".sites-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}

// SetPath points the store at a file other than the default, for tests.
func (s *Store) SetPath(path string) { s.path = path }

// Path reports where the store lives.
func (s *Store) Path() string {
	if s.path == "" {
		return DefaultPath()
	}
	return s.path
}
