// Package api serves the daemon's loopback HTTP interface and mounts the MCP
// endpoint.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/karamble/omarchy-site-sentinel/alerts"
	"github.com/karamble/omarchy-site-sentinel/mcpserver"
	"github.com/karamble/omarchy-site-sentinel/monitor"
	"github.com/karamble/omarchy-site-sentinel/sites"
)

// Server owns the store and the monitor, and mediates every change to either.
type Server struct {
	mu     sync.RWMutex
	store  *sites.Store
	mon    *monitor.Monitor
	engine *alerts.Engine

	logger  *slog.Logger
	version string
	started time.Time
}

// NewServer wires a server. Attach the monitor with SetMonitor.
func NewServer(store *sites.Store, mon *monitor.Monitor, logger *slog.Logger, version string) *Server {
	return &Server{
		store:   store,
		mon:     mon,
		logger:  logger,
		version: version,
		started: time.Now(),
	}
}

// Store hands out the current store.
func (s *Server) Store() *sites.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store
}

// SetMonitor attaches the monitor. Construction is circular: the monitor reads
// configuration through the server, so build the server first.
func (s *Server) SetMonitor(m *monitor.Monitor) {
	s.mu.Lock()
	s.mon = m
	s.mu.Unlock()
}

// SetEngine attaches the alert engine once it exists.
func (s *Server) SetEngine(e *alerts.Engine) {
	s.mu.Lock()
	s.engine = e
	s.mu.Unlock()
}

// alertEngine resolves the engine per call. Handler runs before SetEngine, so
// a captured value would pin a nil.
func (s *Server) alertEngine() mcpserver.Alerts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return nil
	}
	return s.engine
}

// Handler builds the routing table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /api/sites", s.handleSites)
	mux.HandleFunc("POST /api/sites", s.handleAddSite)
	mux.HandleFunc("PATCH /api/sites/{id}", s.handleEditSite)
	mux.HandleFunc("DELETE /api/sites/{id}", s.handleRemoveSite)
	mux.HandleFunc("GET /api/catalogue", s.handleCatalogue)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("POST /api/alerts", s.handleArm)
	mux.HandleFunc("PATCH /api/alerts/{id}", s.handleEditAlert)
	mux.HandleFunc("DELETE /api/alerts/{id}", s.handleDisarm)
	mux.HandleFunc("GET /api/agents", s.handleAgents)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/monitoring", s.handleMonitoring)
	mux.HandleFunc("POST /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/mcp", s.handleMCPToggle)
	mux.HandleFunc("POST /api/token/recycle", s.handleRecycleToken)

	// The MCP endpoint is checked per request rather than mounted once, so the
	// switch takes effect immediately instead of at the next daemon restart.
	mcpHandler := mcpserver.Handler(mcpserver.Source{
		State:      s.Snapshot,
		Sites:      s.siteList,
		Mutate:     s.mutate,
		Monitoring: func() bool { return s.Store().MonitoringOn() },
		Alerts:     s.alertEngine,
	}, s.version)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Store().MCPOn() {
			writeJSON(w, s.logger, http.StatusNotFound, map[string]string{
				"error": "the mcp endpoint is disabled in Site Sentinel settings",
			})
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	return s.withRequestLog(s.withAuth(mux))
}

// Snapshot is the alert-facing view, with the store's thresholds folded in.
func (s *Server) Snapshot() alerts.Snapshot {
	st := s.Store()
	return alerts.Snapshot{
		State:      s.mon.Snapshot(),
		CertWarn:   st.CertWarn(),
		CertUrgent: st.CertUrgent(),
		DomainWarn: st.DomainWarn(),
		Monitoring: st.MonitoringOn(),
		TakenAt:    time.Now().UTC(),
	}
}

func (s *Server) siteList() []sites.Site { return s.Store().Sites }

// mutate applies a change to the store under the lock and persists it.
func (s *Server) mutate(apply func(*sites.Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := apply(s.store); err != nil {
		return err
	}
	return s.store.Save()
}

type healthResponse struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Uptime     string                     `json:"uptime"`
	Sites      int                        `json:"sites"`
	Enabled    int                        `json:"enabled"`
	Down       int                        `json:"down"`
	Monitoring bool                       `json:"monitoring"`
	MCPEnabled bool                       `json:"mcpEnabled"`
	LocalFault bool                       `json:"localFault"`
	FaultSince time.Time                  `json:"faultSince,omitzero"`
	Last       map[monitor.Tier]time.Time `json:"last"`
	CertWarn   int                        `json:"certWarnDays"`
	CertUrgent int                        `json:"certUrgentDays"`
	DomainWarn int                        `json:"domainWarnDays"`
}

func (s *Server) health() healthResponse {
	st := s.Store()
	snap := s.mon.Snapshot()
	down := 0
	for i := range snap.Sites {
		if snap.Sites[i].Down() {
			down++
		}
	}
	return healthResponse{
		Status:     "ok",
		Version:    s.version,
		Uptime:     time.Since(s.started).Round(time.Second).String(),
		Sites:      len(st.Sites),
		Enabled:    len(st.Enabled()),
		Down:       down,
		Monitoring: st.MonitoringOn(),
		MCPEnabled: st.MCPOn(),
		LocalFault: snap.LocalFault,
		FaultSince: snap.FaultSince,
		Last:       snap.Last,
		CertWarn:   st.CertWarn(),
		CertUrgent: st.CertUrgent(),
		DomainWarn: st.DomainWarn(),
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, s.health())
}

// Row is one site as the panel draws it: the state plus the derived numbers and
// the line of explanation.
type Row struct {
	monitor.SiteState
	Severity   string `json:"severity"`
	Reason     string `json:"reason"`
	CertDays   int    `json:"certDays"`
	DomainDays int    `json:"domainDays"`
	Enabled    bool   `json:"enabled"`
}

type dashboardResponse struct {
	Health healthResponse `json:"health"`
	Rows   []Row          `json:"rows"`
	Worst  string         `json:"worst"`
	Alerts int            `json:"alerts"`
}

// handleDashboard returns everything the panel paints in one call.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	st := s.Store()
	snap := s.mon.Snapshot()
	now := time.Now().UTC()

	enabled := make(map[string]bool, len(st.Sites))
	for _, site := range st.Sites {
		enabled[site.ID] = site.Enabled
	}

	rows := make([]Row, 0, len(snap.Sites))
	worst := monitor.SevOK
	for i := range snap.Sites {
		state := snap.Sites[i]
		sev := state.Severity(now, st.CertWarn(), st.CertUrgent(), st.DomainWarn())
		if sev > worst {
			worst = sev
		}
		rows = append(rows, Row{
			SiteState:  state,
			Severity:   sev.String(),
			Reason:     state.Reason(now, st.CertWarn(), st.CertUrgent(), st.DomainWarn()),
			CertDays:   state.CertDaysLeft(now),
			DomainDays: state.DomainDaysLeft(now),
			Enabled:    enabled[state.ID],
		})
	}

	sortRows(rows)

	armed := 0
	if e := s.alertEngine(); e != nil {
		armed = len(e.List())
	}
	writeJSON(w, s.logger, http.StatusOK, dashboardResponse{
		Health: s.health(),
		Rows:   rows,
		Worst:  worst.String(),
		Alerts: armed,
	})
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"sites": s.Store().Sites})
}

type siteInput struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Expect      string `json:"expect"`
	Domain      string `json:"domain"`
	Enabled     *bool  `json:"enabled"`
	CheckTLS    *bool  `json:"checkTLS"`
	CheckDomain *bool  `json:"checkDomain"`
}

func (s *Server) handleAddSite(w http.ResponseWriter, r *http.Request) {
	var in siteInput
	if !s.decode(w, r, &in) {
		return
	}
	site := sites.Site{
		Name: in.Name, URL: in.URL, Expect: in.Expect, Domain: in.Domain,
		Enabled:  in.Enabled == nil || *in.Enabled,
		CheckTLS: in.CheckTLS, CheckDomain: in.CheckDomain,
	}

	var added sites.Site
	if err := s.mutate(func(st *sites.Store) error {
		var err error
		added, err = st.Add(site)
		return err
	}); err != nil {
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Probe at once rather than leaving the row blank until the next round.
	go s.mon.Refresh(context.Background())
	writeJSON(w, s.logger, http.StatusOK, added)
}

func (s *Server) handleEditSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in siteInput
	if !s.decode(w, r, &in) {
		return
	}
	var out sites.Site
	if err := s.mutate(func(st *sites.Store) error {
		var err error
		out, err = st.Edit(id, func(site *sites.Site) {
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
	}); err != nil {
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, out)
}

func (s *Server) handleRemoveSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	removed := false
	if err := s.mutate(func(st *sites.Store) error {
		removed = st.Remove(id)
		return nil
	}); err != nil {
		writeJSON(w, s.logger, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if removed {
		s.mon.Forget(id)
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]bool{"removed": removed})
}

func (s *Server) handleCatalogue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"catalogue": alerts.Catalogue()})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	e := s.alertEngine()
	if e == nil {
		writeJSON(w, s.logger, http.StatusServiceUnavailable, map[string]string{
			"error": "alerts are not running on this daemon",
		})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"alerts": e.List()})
}

func (s *Server) handleArm(w http.ResponseWriter, r *http.Request) {
	e := s.alertEngine()
	if e == nil {
		writeJSON(w, s.logger, http.StatusServiceUnavailable, map[string]string{
			"error": "alerts are not running on this daemon",
		})
		return
	}
	var t alerts.Trigger
	if !s.decode(w, r, &t) {
		return
	}
	armed, err := e.Arm(t)
	if err != nil {
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, armed)
}

func (s *Server) handleEditAlert(w http.ResponseWriter, r *http.Request) {
	e := s.alertEngine()
	if e == nil {
		writeJSON(w, s.logger, http.StatusServiceUnavailable, map[string]string{
			"error": "alerts are not running on this daemon",
		})
		return
	}
	var patch alerts.Trigger
	if !s.decode(w, r, &patch) {
		return
	}
	out, err := e.Edit(r.PathValue("id"), func(t *alerts.Trigger) {
		if patch.Path != "" {
			t.Path = patch.Path
		}
		if patch.Operator != "" {
			t.Operator = patch.Operator
		}
		if patch.Reason != "" {
			t.Reason = patch.Reason
		}
		if patch.Params.Value != "" {
			t.Params.Value = patch.Params.Value
		}
		if patch.Params.Above != nil {
			t.Params.Above = patch.Params.Above
		}
		if patch.Params.Below != nil {
			t.Params.Below = patch.Params.Below
		}
		if !patch.ExpiresAt.IsZero() {
			t.ExpiresAt = patch.ExpiresAt
		}
		if len(patch.Where) > 0 {
			t.Where = patch.Where
		}
	})
	if err != nil {
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, out)
}

func (s *Server) handleDisarm(w http.ResponseWriter, r *http.Request) {
	e := s.alertEngine()
	if e == nil {
		writeJSON(w, s.logger, http.StatusServiceUnavailable, map[string]string{
			"error": "alerts are not running on this daemon",
		})
		return
	}
	removed, err := e.Disarm(r.PathValue("id"))
	if err != nil {
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]bool{"disarmed": removed})
}

// handleAgents counts the triggers each armedBy owns.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	e := s.alertEngine()
	if e == nil {
		writeJSON(w, s.logger, http.StatusOK, map[string]any{"agents": []string{}})
		return
	}
	seen := map[string]int{}
	for _, t := range e.List() {
		if t.ArmedBy != "" {
			seen[t.ArmedBy]++
		}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"agents": seen})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	go s.mon.Refresh(context.Background())
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"status": "refreshing"})
}

func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.mutate(func(st *sites.Store) error {
		v := in.Enabled
		st.Monitoring = &v
		return nil
	}); err != nil {
		writeJSON(w, s.logger, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]bool{"monitoring": in.Enabled})
}

func (s *Server) handleMCPToggle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.mutate(func(st *sites.Store) error {
		v := in.Enabled
		st.MCPEnabled = &v
		return nil
	}); err != nil {
		writeJSON(w, s.logger, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]bool{"mcpEnabled": in.Enabled})
}

// handleSettings changes cadences and thresholds. An omitted field is left
// alone.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReachMin       *int `json:"reachMin"`
		DNSMin         *int `json:"dnsMin"`
		TLSMin         *int `json:"tlsMin"`
		DomainMin      *int `json:"domainMin"`
		CertWarnDays   *int `json:"certWarnDays"`
		CertUrgentDays *int `json:"certUrgentDays"`
		DomainWarnDays *int `json:"domainWarnDays"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.mutate(func(st *sites.Store) error {
		for _, f := range []struct{ in, out **int }{
			{&in.ReachMin, &st.ReachMin},
			{&in.DNSMin, &st.DNSMin},
			{&in.TLSMin, &st.TLSMin},
			{&in.DomainMin, &st.DomainMin},
			{&in.CertWarnDays, &st.CertWarnDays},
			{&in.CertUrgentDays, &st.CertUrgentDays},
			{&in.DomainWarnDays, &st.DomainWarnDays},
		} {
			if *f.in != nil {
				*f.out = *f.in
			}
		}
		return nil
	}); err != nil {
		writeJSON(w, s.logger, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, s.health())
}

// handleRecycleToken mints a new bearer token.
func (s *Server) handleRecycleToken(w http.ResponseWriter, r *http.Request) {
	var token string
	if err := s.mutate(func(st *sites.Store) error {
		tok, err := sites.NewAPIToken()
		if err != nil {
			return err
		}
		st.APIToken = tok
		token = tok
		return nil
	}); err != nil {
		writeJSON(w, s.logger, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"apiToken": token})
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the token per request: recycling it must lock out old clients
		// immediately, not at the next daemon restart.
		want := []byte(s.Store().APIToken)
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="sitesentinel"`)
			writeJSON(w, s.logger, http.StatusUnauthorized, map[string]string{
				"error": "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

// maxBody caps a request body. Every request this API accepts is a handful of
// fields, so the cap sits far above real use and far below anything that could
// exhaust memory.
const maxBody = 64 << 10

// decode reads a JSON body under a hard size cap, reporting the error itself.
//
// MaxBytesReader stops the read at the cap rather than after it, so a client
// holding the connection open cannot make the daemon buffer without bound. An
// oversized body answers 413, since the request was well formed and merely too
// large; anything else is a 400.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, s.logger, http.StatusRequestEntityTooLarge,
				map[string]string{"error": "request body too large"})
			return false
		}
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Error("encoding response", "err", err)
	}
}
