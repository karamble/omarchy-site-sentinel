package monitor

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/karamble/omarchy-site-sentinel/check"
)

var now = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func ok() check.Reach  { return check.Reach{StatusCode: 200} }
func bad() check.Reach { return check.Reach{StatusCode: 500} }

func TestSiteDownNeedsTwoFailures(t *testing.T) {
	s := &SiteState{}
	s.applyReach(ok(), "", now)
	if !s.Up {
		t.Fatal("a 200 should be up")
	}

	s.applyReach(bad(), "", now.Add(time.Minute))
	if !s.Up {
		t.Error("one failure must not mark a site down: that is how flaky links become alerts")
	}
	if s.Fails != 1 {
		t.Errorf("fails = %d, want 1", s.Fails)
	}

	s.applyReach(bad(), "", now.Add(2*time.Minute))
	if s.Up {
		t.Error("two consecutive failures should mark the site down")
	}

	s.applyReach(ok(), "", now.Add(3*time.Minute))
	if !s.Up || s.Fails != 0 {
		t.Error("a success should clear the failure count immediately")
	}
}

func TestContentAssertionOutranksStatus(t *testing.T) {
	r := check.Reach{StatusCode: 200}
	if r.Up("Welcome") {
		t.Error("a 200 whose body lacks the expected string is not up")
	}
	r.Matched = true
	if !r.Up("Welcome") {
		t.Error("a 200 with the expected string is up")
	}
	if plain := (check.Reach{StatusCode: 200}); !plain.Up("") {
		t.Error("with no assertion a 200 is up")
	}
}

func TestFirstLookupIsNotAChange(t *testing.T) {
	s := &SiteState{}
	first := check.Hosting{Addrs: mustAddrs(t, "203.0.113.10")}
	s.applyDNS(first, nil, []string{"ns1.example.net"}, nil, now)
	if s.AddressChanged || s.NSChanged {
		t.Fatal("the first lookup establishes the baseline, it is not a change")
	}

	s.applyDNS(first, nil, []string{"ns1.example.net"}, nil, now)
	if s.AddressChanged || s.NSChanged {
		t.Error("an unchanged lookup is not a change")
	}

	s.applyDNS(check.Hosting{Addrs: mustAddrs(t, "198.51.100.7")}, nil,
		[]string{"ns1.elsewhere.net"}, nil, now)
	if !s.AddressChanged {
		t.Error("a moved address should be flagged")
	}
	if !s.NSChanged {
		t.Error("a moved delegation should be flagged")
	}

	s.AcknowledgeChanges()
	if s.AddressChanged || s.NSChanged {
		t.Error("acknowledging should clear the flags so a move announces once")
	}
}

func TestDomainFailureKeepsTheKnownDate(t *testing.T) {
	s := &SiteState{}
	expiry := now.AddDate(1, 0, 0)
	s.applyDomain("example.com", check.Domain{Expiration: expiry}, nil, now)
	if !s.DomainKnown || !s.DomainExpires.Equal(expiry) {
		t.Fatal("a successful lookup should record the date")
	}

	s.applyDomain("example.com", check.Domain{}, errors.New("registry timed out"), now)
	if !s.DomainKnown || !s.DomainExpires.Equal(expiry) {
		t.Error("one failed poll must not erase a renewal date already in hand")
	}
	if s.DomainError == "" {
		t.Error("the failure should still be visible")
	}
}

func TestUnknownExpiryIsNotExpiringToday(t *testing.T) {
	s := &SiteState{}
	// Several ccTLDs publish no expiration at all.
	s.applyDomain("example.de", check.Domain{Status: []string{"connect"}}, nil, now)
	if s.DomainKnown {
		t.Fatal("a registry publishing no expiry must read as unknown")
	}
	if got := s.DomainDaysLeft(now); got != 0 {
		t.Errorf("days left = %d, want 0 for unknown", got)
	}
	if sev := s.Severity(now, 14, 7, 30); sev != SevOK {
		t.Errorf("unknown expiry must not raise severity, got %s", sev)
	}
}

func TestCertificateThresholds(t *testing.T) {
	base := func(days int) *SiteState {
		return &SiteState{
			Probed: true, Up: true, CertChecked: true,
			CertValid: true, CertCoversHost: true,
			CertNotAfter: now.Add(time.Duration(days)*24*time.Hour + time.Hour),
		}
	}
	for _, tc := range []struct {
		days int
		want Severity
	}{
		{days: 30, want: SevOK}, // Let's Encrypt renews here; warning would be noise
		{days: 15, want: SevOK},
		{days: 14, want: SevWarn},
		{days: 8, want: SevWarn},
		{days: 7, want: SevUrgent},
		{days: -1, want: SevUrgent},
	} {
		if got := base(tc.days).Severity(now, 14, 7, 30); got != tc.want {
			t.Errorf("%d days left: severity %s, want %s", tc.days, got, tc.want)
		}
	}
}

func TestWrongHostCertificateIsUrgentEvenIfValid(t *testing.T) {
	// A certificate that parses and has years left, but covers no names.
	s := &SiteState{
		Probed: true, Up: true, CertChecked: true,
		CertValid: false, CertCoversHost: false,
		CertNotAfter: now.AddDate(9, 0, 0),
		CertIssuer:   "default",
	}
	if got := s.Severity(now, 14, 7, 30); got != SevUrgent {
		t.Errorf("severity %s, want urgent", got)
	}
	if got := s.Reason(now, 14, 7, 30); got != "certificate does not cover this host" {
		t.Errorf("reason %q", got)
	}
}

func TestLocalFaultSuppression(t *testing.T) {
	refused := check.Reach{Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	answered := check.Reach{StatusCode: 500}
	nx := check.Reach{Err: &net.DNSError{Err: "no such host", IsNotFound: true}}

	for _, tc := range []struct {
		name  string
		round []check.Reach
		want  bool
	}{
		{"every site failed at the transport layer", []check.Reach{refused, refused, refused}, true},
		{"one site answered", []check.Reach{refused, answered}, false},
		{"a single site is never enough evidence", []check.Reach{refused}, false},
		{"NXDOMAIN is an answer about the domain", []check.Reach{nx, nx}, false},
	} {
		if got := check.LocalFault(tc.round); got != tc.want {
			t.Errorf("%s: LocalFault = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func mustAddrs(t *testing.T, list ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		a, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parsing %s: %v", s, err)
		}
		out = append(out, a)
	}
	return out
}

// A site is not an outage the moment it is added, nor after one missed round.
// Up starts false, so counting !Up as down reported every new site as down.
func TestNotUpIsNotDown(t *testing.T) {
	s := &SiteState{}
	if s.Down() || s.Failing() {
		t.Error("a site never probed is neither down nor failing")
	}
	if sev := s.Severity(now, 14, 7, 30); sev != SevOK {
		t.Errorf("unprobed severity %s, want ok", sev)
	}

	s.applyReach(bad(), "", now)
	if s.Down() {
		t.Error("one missed round is not a confirmed outage")
	}
	if !s.Failing() {
		t.Error("one missed round should read as failing")
	}
	if sev := s.Severity(now, 14, 7, 30); sev != SevWarn {
		t.Errorf("one missed round severity %s, want warn", sev)
	}

	s.applyReach(bad(), "", now.Add(time.Minute))
	if !s.Down() || s.Failing() {
		t.Error("two missed rounds is a confirmed outage")
	}
	if sev := s.Severity(now, 14, 7, 30); sev != SevUrgent {
		t.Errorf("confirmed outage severity %s, want urgent", sev)
	}
}

// A dial that never reached a handshake is not a certificate fault.
func TestUnreachableHostIsNotACertificateProblem(t *testing.T) {
	s := &SiteState{}
	s.applyTLS(check.Cert{}, &net.DNSError{Err: "no such host", IsNotFound: true}, now)
	if s.CertChecked {
		t.Error("NXDOMAIN says nothing about a certificate")
	}

	s = &SiteState{}
	s.applyTLS(check.Cert{}, &net.OpError{Op: "dial", Err: errors.New("connection refused")}, now)
	if s.CertChecked {
		t.Error("a refused dial says nothing about a certificate")
	}
	if s.CertError == "" {
		t.Error("the failure should still be recorded")
	}

	// A certificate that was actually read and is wrong still counts.
	s = &SiteState{}
	s.applyTLS(check.Cert{CoversHost: false, ChainValid: false, VerifyErr: "not valid for any names"}, nil, now)
	if !s.CertChecked || s.CertCoversHost {
		t.Error("a served certificate that covers no names is a certificate fault")
	}
}

func TestReasonDropsTheRepeatedURL(t *testing.T) {
	s := &SiteState{Probed: true, Fails: 2}
	s.ReachError = `Get "https://example.com": dial tcp: lookup example.com: no such host`
	got := s.Reason(now, 14, 7, 30)
	if strings.Contains(got, "https://example.com") {
		t.Errorf("the host is already on the row, reason should not repeat it: %q", got)
	}
	if got != "dial tcp: lookup example.com: no such host" {
		t.Errorf("reason = %q", got)
	}

	// An error in another shape is left alone.
	s.ReachError = "connection reset by peer"
	if got := s.Reason(now, 14, 7, 30); got != "connection reset by peer" {
		t.Errorf("reason = %q", got)
	}
}
