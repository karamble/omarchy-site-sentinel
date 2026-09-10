// Package client talks to a running daemon over its loopback API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/karamble/omarchy-site-sentinel/sites"
)

// Client is a connection to the daemon.
type Client struct {
	Addr  string
	Token string
	HTTP  *http.Client
}

// New reads the token from the store. A token is never accepted as an
// argument, because argv is readable by every process.
func New(addr, storePath string) (*Client, error) {
	store, err := sites.Load(storePath)
	if errors.Is(err, sites.ErrNotConfigured) {
		return nil, fmt.Errorf("the daemon has never run: start it from the panel")
	}
	if err != nil {
		return nil, err
	}
	return &Client{
		Addr:  addr,
		Token: store.APIToken,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Do performs one request and decodes the answer into out.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	url := "http://" + strings.TrimPrefix(c.Addr, "http://") + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("the daemon is not answering on %s: %w", c.Addr, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// PrintJSON writes an indented value to stdout, for the --json flags.
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Raw performs one request and returns the response body unchanged. Use it
// wherever the daemon's answer is passed on verbatim: decoding into a local
// struct and re-encoding drops every field that struct does not name.
func (c *Client) Raw(ctx context.Context, method, path string) ([]byte, error) {
	var out json.RawMessage
	if err := c.Do(ctx, method, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
