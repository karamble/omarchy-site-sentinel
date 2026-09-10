// Package check performs the unauthenticated probes a site sentinel needs.
// Every function here is standard library only and starts no processes.
package check

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/smtp"
	"strings"
	"time"
)

// bodyLimit caps how much of a response body is read.
const bodyLimit = 64 << 10

// Reach is the result of one HTTP probe.
type Reach struct {
	StatusCode int
	Elapsed    time.Duration
	Chain      []string // every URL after the first, in order
	Matched    bool     // expect was found in the body
	Err        error
}

// Up reports a 2xx or 3xx status with the expected string present.
func (r Reach) Up(expect string) bool {
	return r.Err == nil &&
		r.StatusCode >= 200 && r.StatusCode < 400 &&
		(expect == "" || r.Matched)
}

// HTTP probes url, following redirects and recording the chain. expect, when
// non-empty, is a string the body must contain.
func HTTP(ctx context.Context, client *http.Client, url, expect string) Reach {
	var chain []string
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		chain = append(chain, req.URL.String())
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Reach{Chain: chain, Err: err}
	}
	req.Header.Set("User-Agent", "site-sentinel/1 (+monitoring)")

	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return Reach{Elapsed: time.Since(start), Chain: chain, Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return Reach{StatusCode: resp.StatusCode, Elapsed: time.Since(start), Chain: chain, Err: err}
	}

	return Reach{
		StatusCode: resp.StatusCode,
		Elapsed:    time.Since(start),
		Chain:      chain,
		Matched:    expect != "" && strings.Contains(string(body), expect),
	}
}

// Cert describes the certificate a host actually served.
type Cert struct {
	Subject    string
	Issuer     string
	NotBefore  time.Time
	NotAfter   time.Time
	DNSNames   []string
	CoversHost bool
	ChainValid bool
	VerifyErr  string
}

// DaysLeft is negative once the certificate has expired.
func (c Cert) DaysLeft(now time.Time) int {
	return int(c.NotAfter.Sub(now).Hours() / 24)
}

// TLS reads the certificate served for host on addr. Verification is disabled
// during the dial and performed afterwards, so an invalid certificate is
// reported rather than failing the connection.
func TLS(ctx context.Context, d *net.Dialer, addr, host string) (Cert, error) {
	td := &tls.Dialer{
		NetDialer: d,
		Config: &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: true, // verified manually below
			MinVersion:         tls.VersionTLS12,
		},
	}
	conn, err := td.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Cert{}, err
	}
	defer conn.Close()

	state := conn.(*tls.Conn).ConnectionState()
	return fromState(state, host)
}

func fromState(state tls.ConnectionState, host string) (Cert, error) {
	if len(state.PeerCertificates) == 0 {
		return Cert{}, errors.New("peer sent no certificate")
	}
	leaf := state.PeerCertificates[0]

	inter := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		inter.AddCert(c)
	}

	out := Cert{
		Subject:   leaf.Subject.CommonName,
		Issuer:    leaf.Issuer.CommonName,
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
		DNSNames:  leaf.DNSNames,
	}
	// Hostname coverage and chain validity are reported separately.
	if err := leaf.VerifyHostname(host); err == nil {
		out.CoversHost = true
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       host,
		Intermediates: inter,
	}); err != nil {
		out.VerifyErr = err.Error()
	} else {
		out.ChainValid = true
	}
	return out, nil
}

// Mail is the result of an unauthenticated SMTP probe.
type Mail struct {
	Banner   string
	STARTTLS bool
	Cert     Cert
}

// SMTP connects, reads the banner, greets and, when STARTTLS is offered,
// upgrades to read the mail host's certificate. It never authenticates.
func SMTP(ctx context.Context, d *net.Dialer, addr, host string) (Mail, error) {
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return Mail{}, err
	}
	// net/smtp takes no context, so the deadline carries cancellation.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return Mail{}, err
	}
	defer c.Close()

	out := Mail{}
	if err := c.Hello("localhost"); err != nil {
		return out, fmt.Errorf("EHLO: %w", err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		out.STARTTLS = true
		cfg := &tls.Config{ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
		if err := c.StartTLS(cfg); err != nil {
			return out, fmt.Errorf("STARTTLS: %w", err)
		}
		if state, ok := c.TLSConnectionState(); ok {
			if cert, err := fromState(state, host); err == nil {
				out.Cert = cert
			}
		}
	}
	_ = c.Quit()
	return out, nil
}

// Domain carries registration facts from RDAP or WHOIS.
type Domain struct {
	Expiration   time.Time
	Registration time.Time
	LastChanged  time.Time
	Status       []string
	Source       string // "rdap" or "whois"
}

// Known reports whether the registry published an expiry date.
func (d Domain) Known() bool { return !d.Expiration.IsZero() }

type rdapResponse struct {
	Status []string `json:"status"`
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
}

// RDAP fetches registration events for domain from an RDAP service URL.
func RDAP(ctx context.Context, client *http.Client, base, domain string) (Domain, error) {
	u := strings.TrimSuffix(base, "/") + "/domain/" + domain
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Domain{}, err
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := client.Do(req)
	if err != nil {
		return Domain{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Domain{}, fmt.Errorf("rdap %s: %s", domain, resp.Status)
	}

	var r rdapResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return Domain{}, err
	}

	out := Domain{Status: r.Status, Source: "rdap"}
	for _, e := range r.Events {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		switch strings.ToLower(e.Action) {
		case "expiration":
			out.Expiration = t
		case "registration":
			out.Registration = t
		case "last changed":
			out.LastChanged = t
		}
	}
	return out, nil
}

// Bootstrap maps each TLD to its RDAP service URLs, from IANA's registry.
// Fetch once and cache.
func Bootstrap(ctx context.Context, client *http.Client) (map[string][]string, error) {
	const src = "https://data.iana.org/rdap/dns.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var doc struct {
		Services [][2][]string `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&doc); err != nil {
		return nil, err
	}

	out := make(map[string][]string)
	for _, svc := range doc.Services {
		tlds, urls := svc[0], svc[1]
		for _, tld := range tlds {
			out[strings.ToLower(tld)] = urls
		}
	}
	return out, nil
}

// WHOIS queries port 43 and returns the raw response, for registries with no
// RDAP service. Parsing is per-registry and belongs above this layer.
func WHOIS(ctx context.Context, d *net.Dialer, server, domain string) (string, error) {
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(server, "43"))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := io.WriteString(conn, domain+"\r\n"); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(conn, bodyLimit))
	return string(b), err
}

// Listing is one DNSBL answer.
type Listing struct {
	Zone   string
	Listed bool
	Codes  []string
}

// DNSBL asks zone whether ip is listed.
//
// A 127.255.255.x answer means the zone refused the query, not that the address
// is listed, and is returned as an error. Use a non-public resolver.
func DNSBL(ctx context.Context, r *net.Resolver, ip netip.Addr, zone string) (Listing, error) {
	rev, err := reverse(ip)
	if err != nil {
		return Listing{Zone: zone}, err
	}

	addrs, err := r.LookupHost(ctx, rev+"."+zone)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return Listing{Zone: zone, Listed: false}, nil // NXDOMAIN: not listed
		}
		return Listing{Zone: zone}, err
	}

	out := Listing{Zone: zone, Listed: true, Codes: addrs}
	for _, a := range addrs {
		if strings.HasPrefix(a, "127.255.255.") {
			return Listing{Zone: zone}, fmt.Errorf("%s refused the query (%s): use a non-public resolver", zone, a)
		}
	}
	return out, nil
}

func reverse(ip netip.Addr) (string, error) {
	if !ip.Is4() {
		return "", fmt.Errorf("dnsbl: %s is not IPv4", ip)
	}
	b := ip.As4()
	return fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0]), nil
}

// LinkUp reports whether any interface is up, not loopback, and carries a
// routable address.
func LinkUp() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			pfx, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := pfx.Addr()
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			return true
		}
	}
	return false
}

// Transport reports whether err is a network-layer failure rather than an
// answer from the far end.
func Transport(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound // NXDOMAIN is an answer, not a transport fault
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// LocalFault reports whether a round failed because this machine has no
// connectivity: the link is down, or every site in the round failed at the
// transport layer. A round of one is never treated as a local fault.
func LocalFault(round []Reach) bool {
	if !LinkUp() {
		return true
	}
	if len(round) < 2 {
		return false
	}
	for _, r := range round {
		if !Transport(r.Err) {
			return false
		}
	}
	return true
}
