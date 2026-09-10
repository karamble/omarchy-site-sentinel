package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/karamble/omarchy-site-sentinel/client"
	"github.com/karamble/omarchy-site-sentinel/sites"
)

func runAdd(args []string) error {
	url0, args := leading(args)
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	c := bind(fs)
	name := fs.String("name", "", "what to call it in the panel; defaults to the hostname")
	expect := fs.String("expect", "", "a string the page must contain")
	domain := fs.String("domain", "", "the registrable domain, when it differs from the hostname")
	noTLS := fs.Bool("no-tls", false, "do not read the certificate")
	noDomain := fs.Bool("no-domain", false, "do not ask the registry about renewal")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: sentinel add <url> [flags]\n\n"+
			"Without -expect a page returning 200 with a database error on it reads as healthy.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if url0 == "" {
		url0 = fs.Arg(0)
	}
	if url0 == "" || fs.NArg() > 1 {
		fs.Usage()
		return errors.New("give exactly one URL")
	}

	body := map[string]any{"url": url0, "name": *name, "expect": *expect, "domain": *domain}
	if *noTLS {
		body["checkTLS"] = false
	}
	if *noDomain {
		body["checkDomain"] = false
	}

	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out sites.Site
	if err := cl.Do(ctx, "POST", "/api/sites", body, &out); err != nil {
		return err
	}
	if c.json {
		return client.PrintJSON(out)
	}
	fmt.Printf("watching %s as %q (id %s)\n", out.URL, out.Label(), out.ID)
	return nil
}

func runEdit(args []string) error {
	want, args := leading(args)
	fs := flag.NewFlagSet("edit", flag.ExitOnError)
	c := bind(fs)
	name := fs.String("name", "", "rename it")
	url := fs.String("url", "", "change the address probed")
	expect := fs.String("expect", "", "change the string the page must contain")
	domain := fs.String("domain", "", "change the registrable domain")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if want == "" {
		want = fs.Arg(0)
	}
	if want == "" {
		return errors.New("usage: sentinel edit <site> [flags]")
	}

	body := map[string]any{}
	for k, v := range map[string]string{"name": *name, "url": *url, "expect": *expect, "domain": *domain} {
		if v != "" {
			body[k] = v
		}
	}
	if len(body) == 0 {
		return errors.New("nothing to change: give at least one flag")
	}
	return patchSite(c, want, body)
}

func runEnable(args []string, enabled bool) error {
	verb := "pause"
	if enabled {
		verb = "resume"
	}
	want, args := leading(args)
	fs := flag.NewFlagSet(verb, flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if want == "" {
		want = fs.Arg(0)
	}
	if want == "" {
		return fmt.Errorf("usage: sentinel %s <site>", verb)
	}
	return patchSite(c, want, map[string]any{"enabled": enabled})
}

func patchSite(c *common, want string, body map[string]any) error {
	cl, err := c.dial()
	if err != nil {
		return err
	}
	id, err := resolveSite(c, want)
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()
	var out sites.Site
	if err := cl.Do(ctx, "PATCH", "/api/sites/"+id, body, &out); err != nil {
		return err
	}
	if c.json {
		return client.PrintJSON(out)
	}
	fmt.Printf("updated %s\n", out.Label())
	return nil
}

func runRemove(args []string) error {
	want, args := leading(args)
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	c := bind(fs)
	yes := fs.Bool("yes", false, "do not ask")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if want == "" {
		want = fs.Arg(0)
	}
	if want == "" {
		return errors.New("usage: sentinel remove <site>")
	}

	id, err := resolveSite(c, want)
	if err != nil {
		return err
	}
	if !*yes && !confirm(fmt.Sprintf("stop watching %s and forget its history?", want)) {
		fmt.Println("left alone")
		return nil
	}

	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()
	var out struct {
		Removed bool `json:"removed"`
	}
	if err := cl.Do(ctx, "DELETE", "/api/sites/"+id, nil, &out); err != nil {
		return err
	}
	if !out.Removed {
		return fmt.Errorf("no site matched %s", want)
	}
	fmt.Println("removed")
	return nil
}

// resolveSite accepts an id, hostname, URL or display name.
func resolveSite(c *common, want string) (string, error) {
	cl, err := c.dial()
	if err != nil {
		return "", err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out struct {
		Sites []sites.Site `json:"sites"`
	}
	if err := cl.Do(ctx, "GET", "/api/sites", nil, &out); err != nil {
		return "", err
	}
	for _, s := range out.Sites {
		if s.ID == want ||
			strings.EqualFold(s.Host(), want) ||
			strings.EqualFold(s.URL, want) ||
			strings.EqualFold(s.Label(), want) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no watched site matches %q: try sentinel sites", want)
}

func runRecycle(args []string) error {
	fs := flag.NewFlagSet("recycle", flag.ExitOnError)
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
	var out struct {
		Token string `json:"apiToken"`
	}
	if err := cl.Do(ctx, "POST", "/api/token/recycle", nil, &out); err != nil {
		return err
	}
	fmt.Println("A new API token is in place. Every client holding the old one is locked out now.")
	fmt.Println()
	fmt.Printf(`Paste this into the "mcpServers" object of your agent's config:

  "sitesentinel": {
    "type": "http",
    "url": "http://%s/mcp",
    "headers": { "Authorization": "Bearer %s" }
  }

It is shown once, here, because this is the moment it has to be copied.
`, c.addr, out.Token)
	return nil
}

// runPurge deletes the configuration directory. Run it before removing the
// plugin: this binary lives in the folder that removal deletes.
func runPurge(args []string) error {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	yes := fs.Bool("yes", false, "do not ask")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := sites.Dir()
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("nothing to purge: %s does not exist\n", dir)
		return nil
	}

	fmt.Printf("this deletes %s and everything in it:\n", dir)
	for _, f := range []string{"sites.json", "state.json", "triggers.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			fmt.Printf("  %s\n", f)
		}
	}
	if !*yes && !confirm("delete it?") {
		fmt.Println("left alone")
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Printf("deleted %s\n", dir)
	fmt.Println("the plugin itself is removed with: omarchy plugin remove karamble.sitesentinel")
	return nil
}

func confirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
