// Command sentinel is the whole of Site Sentinel: the daemon, the queries and
// the verbs that change configuration, in one binary.
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

var (
	version = "dev"
)

const usage = `Site Sentinel watches sites, certificates and domain renewals.

usage: sentinel <command> [flags]

running it:
  daemon              start the watcher; this is what the plugin runs
  status              one line per site, worst first
  health              whether the daemon is awake and when each tier last ran
  refresh             check every site now

looking:
  sites               the full state of every site
  certs               certificates, soonest to expire first
  domains             registrations, soonest to renew first

changing:
  add <url>           watch a site
  edit <site>         change one
  pause <site>        stop checking it without forgetting it
  resume <site>       check it again
  remove <site>       stop watching and forget its history

alerts:
  alerts              list armed triggers
  catalogue           every path a trigger can watch
  arm                 arm a trigger
  disarm <id>         take one down

setup:
  monitoring on|off   the master switch for all checking
  mcp-endpoint on|off whether the MCP endpoint answers
  set                 cadences and warning thresholds
  mcp                 print the MCP entry to paste into an agent's config
  recycle             mint a new API token, locking out old clients
  purge               delete the configuration directory and everything in it
  version             print the version

Run "sentinel <command> -h" for the flags of one command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "daemon":
		err = runDaemon(args)
	case "status":
		err = runStatus(args)
	case "health":
		err = runSimple(args, "GET", "/api/health")
	case "sites":
		err = runSimple(args, "GET", "/api/sites")
	case "certs", "certificates":
		err = runCerts(args)
	case "domains":
		err = runDomains(args)
	case "add":
		err = runAdd(args)
	case "edit":
		err = runEdit(args)
	case "pause":
		err = runEnable(args, false)
	case "resume":
		err = runEnable(args, true)
	case "remove":
		err = runRemove(args)
	case "alerts":
		err = runSimple(args, "GET", "/api/alerts")
	case "catalogue":
		err = runSimple(args, "GET", "/api/catalogue")
	case "arm":
		err = runArm(args)
	case "disarm":
		err = runDisarm(args)
	case "mcp":
		err = runMCP(args)
	case "monitoring":
		err = runToggle(args, "/api/monitoring", "monitoring")
	case "mcp-endpoint":
		err = runToggle(args, "/api/mcp", "mcp-endpoint")
	case "set":
		err = runSet(args)
	case "refresh":
		err = runRefresh(args)
	case "recycle":
		err = runRecycle(args)
	case "purge":
		err = runPurge(args)
	case "version", "-version", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "sentinel: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "sentinel:", err)
		os.Exit(1)
	}
}

// timeout is how long any one command waits on the daemon.
func timeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
