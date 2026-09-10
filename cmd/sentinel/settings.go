package main

import (
	"flag"
	"fmt"

	"github.com/karamble/omarchy-site-sentinel/client"
)

// runToggle switches monitoring or the MCP endpoint on or off.
func runToggle(args []string, path, label string) error {
	word, args := leading(args)
	fs := flag.NewFlagSet(label, flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if word == "" {
		word = fs.Arg(0)
	}

	var on bool
	switch word {
	case "on", "true", "yes":
		on = true
	case "off", "false", "no":
		on = false
	default:
		return fmt.Errorf("usage: sentinel %s on|off", label)
	}

	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out map[string]any
	if err := cl.Do(ctx, "POST", path, map[string]bool{"enabled": on}, &out); err != nil {
		return err
	}
	if c.json {
		return client.PrintJSON(out)
	}
	fmt.Printf("%s is %s\n", label, word)
	return nil
}

// runSet changes cadences and thresholds. An omitted flag is left alone.
func runSet(args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	c := bind(fs)
	reach := fs.Int("reach", 0, "minutes between reachability rounds")
	dns := fs.Int("dns", 0, "minutes between DNS rounds")
	tlsMin := fs.Int("tls", 0, "minutes between certificate rounds")
	domain := fs.Int("domain", 0, "minutes between registry rounds; floored at a day")
	certWarn := fs.Int("cert-warn", 0, "days left on a certificate that counts as a warning")
	certUrgent := fs.Int("cert-urgent", 0, "days left on a certificate that counts as urgent")
	domainWarn := fs.Int("domain-warn", 0, "days before renewal worth mentioning")
	if err := fs.Parse(args); err != nil {
		return err
	}

	body := map[string]int{}
	for name, target := range map[string]*int{
		"reachMin": reach, "dnsMin": dns, "tlsMin": tlsMin, "domainMin": domain,
		"certWarnDays": certWarn, "certUrgentDays": certUrgent, "domainWarnDays": domainWarn,
	} {
		body[name] = *target
	}
	// Only flags actually given are sent, so an omitted one is not reset.
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	send := map[string]int{}
	for flagName, field := range map[string]string{
		"reach": "reachMin", "dns": "dnsMin", "tls": "tlsMin", "domain": "domainMin",
		"cert-warn": "certWarnDays", "cert-urgent": "certUrgentDays", "domain-warn": "domainWarnDays",
	} {
		if given[flagName] {
			send[field] = body[field]
		}
	}
	if len(send) == 0 {
		fs.Usage()
		return nil
	}

	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out map[string]any
	if err := cl.Do(ctx, "POST", "/api/settings", send, &out); err != nil {
		return err
	}
	if c.json {
		return client.PrintJSON(out)
	}
	fmt.Println("updated")
	return nil
}

// runRefresh asks the daemon to check every site now.
func runRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
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
	if err := cl.Do(ctx, "POST", "/api/refresh", nil, nil); err != nil {
		return err
	}
	fmt.Println("checking now")
	return nil
}
