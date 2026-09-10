package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strings"
)

// Hosting is where a name currently points.
type Hosting struct {
	Addrs   []netip.Addr // A and AAAA, sorted so comparison is stable
	Reverse []string     // PTR of the first address, best effort
	CDN     string       // "" when the addresses are not in a known CDN range
}

// Same compares address sets, ignoring order.
func (h Hosting) Same(other Hosting) bool {
	return slices.Equal(addrStrings(h.Addrs), addrStrings(other.Addrs))
}

func addrStrings(a []netip.Addr) []string {
	out := make([]string, 0, len(a))
	for _, x := range a {
		out = append(out, x.String())
	}
	sort.Strings(out)
	return out
}

// Resolve looks up the addresses for host, and the reverse name of the first
// one where there is one. A missing PTR record is not an error.
func Resolve(ctx context.Context, r *net.Resolver, host string) (Hosting, error) {
	ips, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return Hosting{}, err
	}
	out := Hosting{Addrs: make([]netip.Addr, 0, len(ips))}
	for _, ip := range ips {
		out.Addrs = append(out.Addrs, ip.Unmap())
	}
	sort.Slice(out.Addrs, func(i, j int) bool {
		return out.Addrs[i].Compare(out.Addrs[j]) < 0
	})
	if len(out.Addrs) > 0 {
		if names, err := r.LookupAddr(ctx, out.Addrs[0].String()); err == nil {
			out.Reverse = names
		}
	}
	return out, nil
}

// Nameservers returns the delegation for domain, lowercased and sorted.
func Nameservers(ctx context.Context, r *net.Resolver, domain string) ([]string, error) {
	ns, err := r.LookupNS(ctx, domain)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, strings.ToLower(strings.TrimSuffix(n.Host, ".")))
	}
	sort.Strings(out)
	return out, nil
}

// Exchange is one MX record.
type Exchange struct {
	Host string
	Pref uint16
}

// MX returns the mail exchangers for domain, sorted by preference. An empty
// result with no error means the domain publishes none.
func MX(ctx context.Context, r *net.Resolver, domain string) ([]Exchange, error) {
	recs, err := r.LookupMX(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Exchange, 0, len(recs))
	for _, m := range recs {
		out = append(out, Exchange{
			Host: strings.ToLower(strings.TrimSuffix(m.Host, ".")),
			Pref: m.Pref,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pref < out[j].Pref })
	return out, nil
}

// TXT returns the TXT records at name.
func TXT(ctx context.Context, r *net.Resolver, name string) ([]string, error) {
	recs, err := r.LookupTXT(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, nil
		}
		return nil, err
	}
	return recs, nil
}

// Ranges holds CIDR blocks belonging to one provider.
type Ranges struct {
	Name   string
	Blocks []netip.Prefix
}

// Match reports the provider name when any address falls inside its blocks.
func Match(addrs []netip.Addr, sets []Ranges) string {
	for _, set := range sets {
		for _, block := range set.Blocks {
			for _, ip := range addrs {
				if block.Contains(ip) {
					return set.Name
				}
			}
		}
	}
	return ""
}

// Cloudflare fetches Cloudflare's published address ranges.
func Cloudflare(ctx context.Context, client *http.Client) (Ranges, error) {
	out := Ranges{Name: "Cloudflare"}
	for _, u := range []string{
		"https://www.cloudflare.com/ips-v4",
		"https://www.cloudflare.com/ips-v6",
	} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return out, err
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("cloudflare ranges: %s", resp.Status)
		}
		for _, line := range strings.Fields(string(body)) {
			if p, err := netip.ParsePrefix(line); err == nil {
				out.Blocks = append(out.Blocks, p)
			}
		}
	}
	if len(out.Blocks) == 0 {
		return out, errors.New("cloudflare published no usable ranges")
	}
	return out, nil
}

// Registrable reduces a hostname to the name a registry knows, so
// www.example.com becomes example.com.
//
// It approximates the public suffix list with the table below; a ccTLD missing
// from it is asked about at the wrong level and RDAP answers that it does not
// know the name.
func Registrable(host string) string {
	host = strings.ToLower(strings.Trim(strings.TrimSuffix(host, "."), "."))
	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}
	last2 := strings.Join(labels[len(labels)-2:], ".")
	if slices.Contains(multiLabelSuffixes, last2) && len(labels) >= 3 {
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return last2
}

var multiLabelSuffixes = []string{
	"co.uk", "org.uk", "me.uk", "ac.uk", "gov.uk",
	"co.nz", "co.za", "com.au", "net.au", "org.au",
	"com.br", "com.mx", "co.jp", "or.jp", "ne.jp",
	"co.in", "com.tr", "com.ar", "com.sg",
}

// bootstrapKey is the TLD an RDAP bootstrap lookup is keyed by.
func bootstrapKey(domain string) string {
	labels := strings.Split(strings.ToLower(domain), ".")
	return labels[len(labels)-1]
}

// Service picks an https RDAP base URL for domain from a bootstrap map.
func Service(boot map[string][]string, domain string) (string, error) {
	urls := boot[bootstrapKey(domain)]
	if len(urls) == 0 {
		return "", fmt.Errorf("no RDAP service for .%s", bootstrapKey(domain))
	}
	for _, u := range urls {
		if strings.HasPrefix(u, "https://") {
			return u, nil
		}
	}
	return urls[0], nil
}
