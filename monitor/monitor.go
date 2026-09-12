// Package monitor runs the checks on their own cadences and keeps the state
// that change detection and the alert engine read.
package monitor

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/karamble/omarchy-site-sentinel/check"
	"github.com/karamble/omarchy-site-sentinel/sites"
)

// Tier names a group of checks sharing a cadence.
type Tier string

const (
	TierReach  Tier = "reach"
	TierDNS    Tier = "dns"
	TierTLS    Tier = "tls"
	TierDomain Tier = "domain"
)

// failuresBeforeDown is how many consecutive failures mark a site down.
const failuresBeforeDown = 2

// Monitor owns the state of every watched site.
type Monitor struct {
	store  func() *sites.Store
	logger *slog.Logger

	client   *http.Client
	dialer   *net.Dialer
	resolver *net.Resolver

	mu    sync.RWMutex
	state map[string]*SiteState
	round Round

	// boot is the RDAP bootstrap map, fetched once. Nil until then.
	boot map[string][]string
	// cdn holds provider ranges for CDN detection.
	cdn []check.Ranges

	statePath string
}

// Round records what the last pass of each tier concluded overall.
type Round struct {
	Last       map[Tier]time.Time `json:"last"`
	LocalFault bool               `json:"localFault"`
	// FaultSince is when connectivity was first judged missing.
	FaultSince time.Time `json:"faultSince,omitempty"`
}

// New builds a monitor. store is a function so a reconfiguration takes effect
// on the next pass without a restart.
func New(store func() *sites.Store, statePath string, logger *slog.Logger) *Monitor {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	m := &Monitor{
		store:     store,
		logger:    logger,
		dialer:    dialer,
		resolver:  net.DefaultResolver,
		statePath: statePath,
		state:     make(map[string]*SiteState),
		round:     Round{Last: make(map[Tier]time.Time)},
		// Proxy is nil on purpose, and spelled out rather than left implicit.
		// http.DefaultTransport proxies according to HTTP_PROXY and friends,
		// which would mean a site's reachability and certificate were read
		// through whatever an environment variable named, rather than from the
		// site. A monitor has to look at the thing it is monitoring.
		client: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				DisableKeepAlives:     true,
			},
		},
	}
	m.load()
	return m
}

// Run drives one ticker per tier until ctx is done.
func (m *Monitor) Run(ctx context.Context) {
	st := m.store()
	reach := time.NewTicker(st.ReachEvery())
	dns := time.NewTicker(st.DNSEvery())
	tls := time.NewTicker(st.TLSEvery())
	dom := time.NewTicker(st.DomainEvery())
	defer func() {
		reach.Stop()
		dns.Stop()
		tls.Stop()
		dom.Stop()
	}()

	// Run every tier once so the panel has something on first paint.
	m.ReachRound(ctx)
	m.DNSRound(ctx)
	m.TLSRound(ctx)
	m.DomainRound(ctx)
	m.save()

	for {
		select {
		case <-ctx.Done():
			m.save()
			return
		case <-reach.C:
			m.ReachRound(ctx)
			m.save()
		case <-dns.C:
			m.DNSRound(ctx)
		case <-tls.C:
			m.TLSRound(ctx)
			m.save()
		case <-dom.C:
			m.DomainRound(ctx)
			m.save()
		}
	}
}

// active returns the sites to probe. Empty means make no network request.
func (m *Monitor) active() []sites.Site {
	st := m.store()
	if !st.MonitoringOn() {
		return nil
	}
	return st.Enabled()
}

// ReachRound probes every site once and folds the results into state.
func (m *Monitor) ReachRound(ctx context.Context) {
	list := m.active()
	if len(list) == 0 {
		return
	}

	type outcome struct {
		site sites.Site
		res  check.Reach
	}
	results := make([]outcome, len(list))

	var wg sync.WaitGroup
	for i, site := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = outcome{site: site, res: check.HTTP(ctx, m.client, site.URL, site.Expect)}
		}()
	}
	wg.Wait()

	reaches := make([]check.Reach, len(results))
	for i, r := range results {
		reaches[i] = r.res
	}

	// A round that failed because this machine is offline says nothing about
	// any site, so it is recorded and dropped.
	fault := check.LocalFault(reaches)

	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()

	m.round.Last[TierReach] = now
	if fault {
		if !m.round.LocalFault {
			m.round.FaultSince = now
			m.logger.Warn("no local connectivity: this round is not evidence about any site")
		}
		m.round.LocalFault = true
		return
	}
	if m.round.LocalFault {
		m.logger.Info("local connectivity is back", "was_out_for", now.Sub(m.round.FaultSince).Round(time.Second))
	}
	m.round.LocalFault = false
	m.round.FaultSince = time.Time{}

	for _, r := range results {
		s := m.ensure(r.site)
		s.applyReach(r.res, r.site.Expect, now)
	}
}

// DNSRound resolves every site and notices what moved.
func (m *Monitor) DNSRound(ctx context.Context) {
	list := m.active()
	if len(list) == 0 {
		return
	}
	if m.cdnRanges() == nil {
		if cf, err := check.Cloudflare(ctx, m.client); err == nil {
			m.mu.Lock()
			m.cdn = []check.Ranges{cf}
			m.mu.Unlock()
		}
	}

	now := time.Now().UTC()
	for _, site := range list {
		host := site.Host()
		if host == "" {
			continue
		}
		hosting, err := check.Resolve(ctx, m.resolver, host)
		if err == nil {
			hosting.CDN = check.Match(hosting.Addrs, m.cdnRanges())
		}
		ns, nsErr := check.Nameservers(ctx, m.resolver, check.Registrable(host))

		m.mu.Lock()
		s := m.ensure(site)
		s.applyDNS(hosting, err, ns, nsErr, now)
		m.round.Last[TierDNS] = now
		m.mu.Unlock()
	}
}

// TLSRound reads the certificate each https site is serving.
func (m *Monitor) TLSRound(ctx context.Context) {
	list := m.active()
	if len(list) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, site := range list {
		if !site.TLSWanted() {
			continue
		}
		cert, err := check.TLS(ctx, m.dialer, site.Addr(), site.Host())

		m.mu.Lock()
		s := m.ensure(site)
		s.applyTLS(cert, err, now)
		m.round.Last[TierTLS] = now
		m.mu.Unlock()
	}
}

// DomainRound asks each registry when the domain expires.
func (m *Monitor) DomainRound(ctx context.Context) {
	list := m.active()
	if len(list) == 0 {
		return
	}
	if m.bootstrap() == nil {
		boot, err := check.Bootstrap(ctx, m.client)
		if err != nil {
			m.logger.Warn("rdap bootstrap unavailable, skipping registration round", "err", err)
			return
		}
		m.mu.Lock()
		m.boot = boot
		m.mu.Unlock()
	}

	now := time.Now().UTC()
	// Serial, because registries rate limit.
	for _, site := range list {
		if !site.DomainWanted() {
			continue
		}
		name := site.Domain
		if name == "" {
			name = check.Registrable(site.Host())
		}
		if name == "" {
			continue
		}

		var dom check.Domain
		base, err := check.Service(m.bootstrap(), name)
		if err == nil {
			dom, err = check.RDAP(ctx, m.client, base, name)
		}

		m.mu.Lock()
		s := m.ensure(site)
		s.applyDomain(name, dom, err, now)
		m.round.Last[TierDomain] = now
		m.mu.Unlock()
	}
}

func (m *Monitor) bootstrap() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.boot
}

func (m *Monitor) cdnRanges() []check.Ranges {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cdn
}

// ensure returns the state for a site, creating it on first sight. The caller
// holds the lock.
func (m *Monitor) ensure(site sites.Site) *SiteState {
	s, ok := m.state[site.ID]
	if !ok {
		s = &SiteState{ID: site.ID}
		m.state[site.ID] = s
	}
	s.Label = site.Label()
	s.URL = site.URL
	s.Host = site.Host()
	return s
}

// Refresh runs every tier once, for the panel's refresh button and the API.
func (m *Monitor) Refresh(ctx context.Context) {
	m.ReachRound(ctx)
	m.DNSRound(ctx)
	m.TLSRound(ctx)
	m.DomainRound(ctx)
	m.save()
}

// Forget drops the state of a site that is no longer watched.
func (m *Monitor) Forget(id string) {
	m.mu.Lock()
	delete(m.state, id)
	m.mu.Unlock()
	m.save()
}
