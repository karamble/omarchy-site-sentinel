package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeRejectsOversizedBody checks the cap is enforced during the read
// rather than after it: a body far larger than maxBody must answer 413 without
// the handler ever seeing the whole thing.
func TestDecodeRejectsOversizedBody(t *testing.T) {
	s := &Server{logger: slog.New(slog.DiscardHandler)}

	var target struct {
		Name string `json:"name"`
	}
	huge := `{"name":"` + strings.Repeat("a", maxBody*4) + `"}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader(huge))

	if s.decode(w, r, &target) {
		t.Fatal("decode accepted a body four times the cap")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
	if target.Name != "" {
		t.Errorf("target was populated from an oversized body: %d bytes", len(target.Name))
	}
}

// TestDecodeAcceptsNormalBody guards against a cap so tight it breaks real use.
func TestDecodeAcceptsNormalBody(t *testing.T) {
	s := &Server{logger: slog.New(slog.DiscardHandler)}

	var target struct {
		URL string `json:"url"`
	}
	body, err := json.Marshal(map[string]string{"url": "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader(string(body)))

	if !s.decode(w, r, &target) {
		got, _ := io.ReadAll(w.Body)
		t.Fatalf("decode rejected an ordinary body: %d %s", w.Code, got)
	}
	if target.URL != "https://example.com" {
		t.Errorf("url = %q", target.URL)
	}
}

// TestDecodeRejectsMalformedBody keeps malformed input on 400, distinct from the
// 413 an oversized one gets.
func TestDecodeRejectsMalformedBody(t *testing.T) {
	s := &Server{logger: slog.New(slog.DiscardHandler)}

	var target struct{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader("{not json"))

	if s.decode(w, r, &target) {
		t.Fatal("decode accepted malformed JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
