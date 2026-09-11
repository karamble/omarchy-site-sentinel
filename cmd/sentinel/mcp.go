package main

import (
	"flag"
	"fmt"

	"github.com/karamble/omarchy-site-sentinel/sites"
)

// runMCP prints the entry to paste into an agent's MCP configuration.
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8098", "daemon address")
	storePath := fs.String("store", "", "path to sites.json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path := *storePath
	if path == "" {
		path = sites.DefaultPath()
	}
	store, err := sites.Load(path)
	if err != nil {
		return err
	}

	fmt.Printf(`Add this to the "mcpServers" object of your agent's config:

  "sitesentinel": {
    "type": "http",
    "url": "http://%s/mcp",
    "headers": { "Authorization": "Bearer %s" }
  }

The token is read from %s.
Recycling it with "sentinel recycle" invalidates this entry immediately.
`, *addr, store.APIToken, path)
	return nil
}
