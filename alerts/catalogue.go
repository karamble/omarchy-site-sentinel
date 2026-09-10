package alerts

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/karamble/omarchy-site-sentinel/monitor"
)

// Kind is the shape of a leaf, which decides the operators it accepts.
type Kind string

const (
	KindNumber Kind = "number"
	KindText   Kind = "text"
	KindBool   Kind = "bool"
	KindList   Kind = "list"
)

// Leaf is one watchable path.
type Leaf struct {
	Path      string     `json:"path"`
	Kind      Kind       `json:"kind"`
	Operators []Operator `json:"operators"`
	Describes string     `json:"describes"`
	// Fields are what --where can filter on, for a list.
	Fields []string `json:"fields,omitempty"`
	// Identity is what makes two entries the same entry, which is what makes
	// appears and disappears honest.
	Identity []string `json:"identity,omitempty"`
	// TimeFields are the timestamps ages can measure.
	TimeFields []string `json:"timeFields,omitempty"`
}

// Accepts reports whether this leaf takes the given operator.
func (l Leaf) Accepts(op Operator) bool { return slices.Contains(l.Operators, op) }

var (
	numberOps = []Operator{OpCrosses}
	textOps   = []Operator{OpBecomes}
	listOps   = []Operator{OpAppears, OpDisappears, OpCount}
	// Lists carrying a timestamp also accept ages.
	listOpsAged = []Operator{OpAppears, OpDisappears, OpCount, OpAges}
)

// siteFields are what a --where clause can filter a site list on.
var (
	siteFields   = []string{"id", "site", "host", "url", "severity", "reason", "cdn", "domain"}
	siteIdentity = []string{"id"}
	siteTimes    = []string{"since", "certExpires", "domainExpires"}
)

// Catalogue is every path a trigger can watch.
func Catalogue() []Leaf {
	return []Leaf{
		// ---- the totals, as numbers
		{Path: "sites.down", Kind: KindNumber, Operators: numberOps,
			Describes: "sites currently down"},
		{Path: "sites.watched", Kind: KindNumber, Operators: numberOps,
			Describes: "sites being watched"},
		{Path: "sites.certExpiring", Kind: KindNumber, Operators: numberOps,
			Describes: "certificates inside the warning window"},
		{Path: "sites.domainExpiring", Kind: KindNumber, Operators: numberOps,
			Describes: "domains inside the renewal window"},
		{Path: "sites.worstCertDays", Kind: KindNumber, Operators: numberOps,
			Describes: "days left on the certificate closest to expiry"},
		{Path: "sites.worstDomainDays", Kind: KindNumber, Operators: numberOps,
			Describes: "days left on the domain closest to renewal"},
		{Path: "sites.slowestMs", Kind: KindNumber, Operators: numberOps,
			Describes: "response time of the slowest site, in milliseconds"},

		// ---- the state of the watcher itself
		{Path: "health.localFault", Kind: KindBool, Operators: textOps,
			Describes: "whether this machine currently has no usable connectivity"},
		{Path: "health.monitoring", Kind: KindBool, Operators: textOps,
			Describes: "whether checking is switched on"},

		// ---- the lists, which is where most triggers will live
		{Path: "sites.downList", Kind: KindList, Operators: listOpsAged,
			Describes: "sites that are down",
			Fields:    siteFields, Identity: siteIdentity, TimeFields: siteTimes},
		{Path: "sites.certProblems", Kind: KindList, Operators: listOpsAged,
			Describes: "certificates expiring, expired, invalid or not covering their host",
			Fields:    siteFields, Identity: siteIdentity, TimeFields: siteTimes},
		{Path: "sites.domainProblems", Kind: KindList, Operators: listOpsAged,
			Describes: "domains inside the renewal window",
			Fields:    siteFields, Identity: siteIdentity, TimeFields: siteTimes},
		{Path: "sites.moved", Kind: KindList, Operators: listOps,
			Describes: "sites whose addresses or nameservers changed since the last look",
			Fields:    siteFields, Identity: siteIdentity},
		{Path: "sites.all", Kind: KindList, Operators: listOps,
			Describes: "every watched site, for filtering with --where",
			Fields:    siteFields, Identity: siteIdentity, TimeFields: siteTimes},
	}
}

// Lookup finds a leaf by path.
func Lookup(path string) (Leaf, bool) {
	for _, l := range Catalogue() {
		if l.Path == path {
			return l, true
		}
	}
	return Leaf{}, false
}

// Snapshot is everything a trigger is evaluated against.
type Snapshot struct {
	State monitor.Snapshot

	// Thresholds come from the store.
	CertWarn   int
	CertUrgent int
	DomainWarn int

	Monitoring bool

	// TakenAt anchors the ages operator.
	TakenAt time.Time
}

// Number resolves a numeric path, reporting false when the path is not one.
func (s Snapshot) Number(path string) (float64, bool) {
	switch path {
	case "sites.down":
		return float64(len(s.downList())), true
	case "sites.watched":
		return float64(len(s.State.Sites)), true
	case "sites.certExpiring":
		return float64(len(s.certProblems())), true
	case "sites.domainExpiring":
		return float64(len(s.domainProblems())), true
	case "sites.worstCertDays":
		return s.worstCertDays(), true
	case "sites.worstDomainDays":
		return s.worstDomainDays(), true
	case "sites.slowestMs":
		return s.slowestMs(), true
	}
	return 0, false
}

// Text resolves a text or bool path as a string.
func (s Snapshot) Text(path string) (string, bool) {
	switch path {
	case "health.localFault":
		return fmt.Sprint(s.State.LocalFault), true
	case "health.monitoring":
		return fmt.Sprint(s.Monitoring), true
	}
	return "", false
}

// List resolves a list path into filterable entries.
func (s Snapshot) List(path string) ([]map[string]any, bool) {
	switch path {
	case "sites.downList":
		return s.downList(), true
	case "sites.certProblems":
		return s.certProblems(), true
	case "sites.domainProblems":
		return s.domainProblems(), true
	case "sites.moved":
		return s.moved(), true
	case "sites.all":
		return s.entries(func(*monitor.SiteState) bool { return true }), true
	}
	return nil, false
}

// entries renders the sites matching keep as filterable maps.
func (s Snapshot) entries(keep func(*monitor.SiteState) bool) []map[string]any {
	now := s.TakenAt
	if now.IsZero() {
		now = s.State.Taken
	}
	out := make([]map[string]any, 0, len(s.State.Sites))
	for i := range s.State.Sites {
		site := &s.State.Sites[i]
		if !keep(site) {
			continue
		}
		entry := map[string]any{
			"id":       site.ID,
			"site":     site.Label,
			"host":     site.Host,
			"url":      site.URL,
			"severity": site.Severity(now, s.CertWarn, s.CertUrgent, s.DomainWarn).String(),
			"reason":   site.Reason(now, s.CertWarn, s.CertUrgent, s.DomainWarn),
			"up":       site.Up,
			"status":   site.StatusCode,
			"ms":       site.ResponseMS,
		}
		if site.CDN != "" {
			entry["cdn"] = site.CDN
		}
		if site.Domain != "" {
			entry["domain"] = site.Domain
		}
		if !site.StateSince.IsZero() {
			entry["since"] = site.StateSince.Format(time.RFC3339)
		}
		if site.CertChecked && !site.CertNotAfter.IsZero() {
			entry["certExpires"] = site.CertNotAfter.Format(time.RFC3339)
			entry["certDays"] = site.CertDaysLeft(now)
		}
		if site.DomainKnown {
			entry["domainExpires"] = site.DomainExpires.Format(time.RFC3339)
			entry["domainDays"] = site.DomainDaysLeft(now)
		}
		out = append(out, entry)
	}
	return out
}

// downList is empty during a local fault: with no connectivity nothing is
// known about any site.
func (s Snapshot) downList() []map[string]any {
	if s.State.LocalFault {
		return nil
	}
	return s.entries(func(site *monitor.SiteState) bool { return site.Down() })
}

func (s Snapshot) certProblems() []map[string]any {
	now := s.now()
	return s.entries(func(site *monitor.SiteState) bool {
		if !site.CertChecked {
			return false
		}
		return !site.CertValid || !site.CertCoversHost || site.CertDaysLeft(now) <= s.CertWarn
	})
}

func (s Snapshot) domainProblems() []map[string]any {
	now := s.now()
	return s.entries(func(site *monitor.SiteState) bool {
		return site.DomainKnown && site.DomainDaysLeft(now) <= s.DomainWarn
	})
}

func (s Snapshot) moved() []map[string]any {
	return s.entries(func(site *monitor.SiteState) bool {
		return site.AddressChanged || site.NSChanged
	})
}

func (s Snapshot) now() time.Time {
	if !s.TakenAt.IsZero() {
		return s.TakenAt
	}
	return s.State.Taken
}

// worstCertDays is the fewest days left on any checked certificate, or a large
// number when none has been checked.
func (s Snapshot) worstCertDays() float64 {
	now := s.now()
	worst := math.MaxInt32
	for i := range s.State.Sites {
		site := &s.State.Sites[i]
		if !site.CertChecked || site.CertNotAfter.IsZero() {
			continue
		}
		if d := site.CertDaysLeft(now); d < worst {
			worst = d
		}
	}
	return float64(worst)
}

func (s Snapshot) worstDomainDays() float64 {
	now := s.now()
	worst := math.MaxInt32
	for i := range s.State.Sites {
		site := &s.State.Sites[i]
		if !site.DomainKnown {
			continue
		}
		if d := site.DomainDaysLeft(now); d < worst {
			worst = d
		}
	}
	return float64(worst)
}

func (s Snapshot) slowestMs() float64 {
	var worst int64
	for i := range s.State.Sites {
		if ms := s.State.Sites[i].ResponseMS; ms > worst {
			worst = ms
		}
	}
	return float64(worst)
}

// identityOf renders the fields that make two entries the same entry.
func identityOf(entry map[string]any, fields []string) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, fmt.Sprint(entry[f]))
	}
	return strings.Join(parts, "\x1f")
}

// filter keeps the entries every where clause accepts.
func filter(entries []map[string]any, wheres []Where) []map[string]any {
	if len(wheres) == 0 {
		return entries
	}
	var out []map[string]any
	for _, e := range entries {
		keep := true
		for _, w := range wheres {
			if !w.Match(e) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}
