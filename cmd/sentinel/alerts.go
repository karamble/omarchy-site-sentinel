package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/karamble/omarchy-site-sentinel/alerts"
	"github.com/karamble/omarchy-site-sentinel/client"
)

// list collects a repeatable flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

func runArm(args []string) error {
	fs := flag.NewFlagSet("arm", flag.ExitOnError)
	c := bind(fs)
	path := fs.String("path", "", "required: a path from `sentinel catalogue`")
	op := fs.String("op", "", "required: appears, disappears, count, crosses, becomes or ages")
	expires := fs.String("expires", "", "required: how long the watch stands, such as 7d or 12h")
	above := fs.Float64("above", 0, "bound for crosses and count")
	below := fs.Float64("below", 0, "bound for crosses and count")
	value := fs.String("value", "", "the value becomes waits for")
	olderThan := fs.String("older-than", "", "the age ages measures against, such as 7d")
	field := fs.String("field", "", "which timestamp ages measures")
	reason := fs.String("reason", "", "why this matters; the alarm carries nothing else")
	deliverTo := fs.String("to", "you", "where the alarm goes")
	standing := fs.Bool("standing", false, "ring every time until it expires")
	armedBy := fs.String("armed-by", "", "who owns this watch")
	var where list
	fs.Var(&where, "where", "filter as field=value or field~=text; repeatable")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: sentinel arm -path <path> -op <operator> -expires <span> [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" || *op == "" || *expires == "" {
		fs.Usage()
		return errors.New("-path, -op and -expires are required")
	}

	operator, ok := alerts.ParseOperator(*op)
	if !ok {
		return fmt.Errorf("unknown operator %q", *op)
	}
	expiresAt, err := alerts.ParseExpiry(*expires, time.Now())
	if err != nil {
		return err
	}
	clauses, err := alerts.ParseWhereAll(where)
	if err != nil {
		return err
	}

	t := alerts.Trigger{
		Path:      *path,
		Operator:  operator,
		Where:     clauses,
		ExpiresAt: expiresAt,
		Reason:    *reason,
		DeliverTo: *deliverTo,
		Standing:  *standing,
		ArmedBy:   *armedBy,
		Params: alerts.Params{
			Value:     *value,
			OlderThan: *olderThan,
			Field:     *field,
		},
	}
	// Only a bound that was actually given is sent; zero is a legitimate bound.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "above":
			v := *above
			t.Params.Above = &v
		case "below":
			v := *below
			t.Params.Below = &v
		}
	})

	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out alerts.Trigger
	if err := cl.Do(ctx, "POST", "/api/alerts", t, &out); err != nil {
		return err
	}
	if c.json {
		return client.PrintJSON(out)
	}
	fmt.Printf("armed %s: %s %s, expires %s\n",
		out.ID, out.Path, out.Operator, out.ExpiresAt.Format("2006-01-02 15:04"))
	return nil
}

func runDisarm(args []string) error {
	id, args := leading(args)
	fs := flag.NewFlagSet("disarm", flag.ExitOnError)
	c := bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		id = fs.Arg(0)
	}
	if id == "" {
		return errors.New("usage: sentinel disarm <id>")
	}
	cl, err := c.dial()
	if err != nil {
		return err
	}
	ctx, cancel := timeout()
	defer cancel()

	var out struct {
		Disarmed bool `json:"disarmed"`
	}
	if err := cl.Do(ctx, "DELETE", "/api/alerts/"+id, nil, &out); err != nil {
		return err
	}
	if !out.Disarmed {
		return fmt.Errorf("no trigger with id %s", id)
	}
	fmt.Println("disarmed")
	return nil
}
