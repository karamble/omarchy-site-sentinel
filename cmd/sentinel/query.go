package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/karamble/omarchy-site-sentinel/api"
	"github.com/karamble/omarchy-site-sentinel/client"
)

// common carries the flags every talking-to-the-daemon command shares.
type common struct {
	addr  string
	store string
	json  bool
	demo  bool
}

func bind(fs *flag.FlagSet) *common {
	c := &common{}
	fs.StringVar(&c.addr, "addr", "127.0.0.1:8098", "daemon address")
	fs.StringVar(&c.store, "store", "", "path to sites.json")
	fs.BoolVar(&c.json, "json", false, "emit raw JSON")
	fs.BoolVar(&c.demo, "demo", false, "fabricated data for the preview screenshot; contacts nothing")
	return c
}

func (c *common) dial() (*client.Client, error) { return client.New(c.addr, c.store) }

type row struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Host       string   `json:"host"`
	URL        string   `json:"url"`
	Severity   string   `json:"severity"`
	Reason     string   `json:"reason"`
	Up         bool     `json:"up"`
	Probed     bool     `json:"probed"`
	StatusCode int      `json:"statusCode"`
	ResponseMS int64    `json:"responseMs"`
	CertDays   int      `json:"certDays"`
	DomainDays int      `json:"domainDays"`
	CertOK     bool     `json:"certValid"`
	CertCovers bool     `json:"certCoversHost"`
	CertSeen   bool     `json:"certChecked"`
	CertIssuer string   `json:"certIssuer"`
	DomainSeen bool     `json:"domainKnown"`
	Domain     string   `json:"domain"`
	CDN        string   `json:"cdn"`
	NS         []string `json:"ns"`
	Reverse    string   `json:"reverse"`
	Enabled    bool     `json:"enabled"`
}

type dashboard struct {
	Health struct {
		Monitoring bool `json:"monitoring"`
		LocalFault bool `json:"localFault"`
		Sites      int  `json:"sites"`
		Down       int  `json:"down"`
	} `json:"health"`
	Rows []row `json:"rows"`
}

func fetchDashboard(c *common) (dashboard, error) {
	var d dashboard
	cl, err := c.dial()
	if err != nil {
		return d, err
	}
	ctx, cancel := timeout()
	defer cancel()
	return d, cl.Do(ctx, "GET", "/api/dashboard", nil, &d)
}

// passthrough prints the daemon's answer unchanged, for -json.
func passthrough(c *common, path string) error {
	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()
	raw, err := cl.Raw(ctx, "GET", path)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(raw, '\n'))
	return err
}

// preamble prints the two facts that change how the table should be read.
func preamble(d dashboard) {
	if d.Health.LocalFault {
		fmt.Println("this machine has no usable connectivity: nothing below is evidence about any site")
	}
	if !d.Health.Monitoring {
		fmt.Println("checking is switched off: this is the last known state")
	}
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if c.demo {
		return client.PrintJSON(api.Demo())
	}
	if c.json {
		return passthrough(c, "/api/dashboard")
	}
	d, err := fetchDashboard(c)
	if err != nil {
		return err
	}
	preamble(d)
	if len(d.Rows) == 0 {
		fmt.Println("no sites are being watched yet: sentinel add https://example.com")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tSTATE\tMS\tCERT\tDOMAIN\tNOTE")
	for _, r := range d.Rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Label, state(r), ms(r), certCell(r), domainCell(r), r.Reason)
	}
	return w.Flush()
}

func runCerts(args []string) error {
	fs := flag.NewFlagSet("certs", flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if c.json {
		return passthrough(c, "/api/dashboard")
	}
	d, err := fetchDashboard(c)
	if err != nil {
		return err
	}
	preamble(d)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tCERTIFICATE\tISSUER\tHOST")
	for _, r := range d.Rows {
		if !r.CertSeen {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Label, certCell(r), r.CertIssuer, r.Host)
	}
	return w.Flush()
}

func runDomains(args []string) error {
	fs := flag.NewFlagSet("domains", flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if c.json {
		return passthrough(c, "/api/dashboard")
	}
	d, err := fetchDashboard(c)
	if err != nil {
		return err
	}
	preamble(d)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DOMAIN\tRENEWS IN\tNAMESERVERS")
	for _, r := range d.Rows {
		cell := domainCell(r)
		if cell == "-" {

			cell = "not published"
		}
		name := r.Domain
		if name == "" {
			name = r.Host
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, cell, strings.Join(r.NS, " "))
	}
	return w.Flush()
}

func runSimple(args []string, method, path string) error {
	fs := flag.NewFlagSet(path, flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()
	var out any
	if err := cl.Do(ctx, method, path, nil, &out); err != nil {
		return err
	}
	return client.PrintJSON(out)
}

func state(r row) string {
	switch {
	case !r.Enabled:
		return "paused"
	case !r.Probed:
		return "new"
	case r.Up:
		return "up"
	default:
		return "down"
	}
}

func ms(r row) string {
	if !r.Probed || r.ResponseMS == 0 {
		return "-"
	}
	return fmt.Sprint(r.ResponseMS)
}

// certCell reports the fault rather than a day count when the certificate is
// broken.
func certCell(r row) string {
	switch {
	case !r.CertSeen:
		return "-"
	case !r.CertCovers:
		return "wrong host"
	case !r.CertOK:
		return "invalid"
	default:
		return fmt.Sprintf("%dd", r.CertDays)
	}
}

func domainCell(r row) string {
	if !r.DomainSeen {
		return "-"
	}
	return fmt.Sprintf("%dd", r.DomainDays)
}
