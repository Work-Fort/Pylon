// SPDX-License-Identifier: GPL-3.0-or-later
package httpprober_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Work-Fort/Pylon/internal/infra/httpprober"
)

func TestProbe_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ui/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":              "sharkfin",
			"label":             "Chat",
			"route":             "/chat",
			"display":           "nav",
			"ws_paths":          []string{"/ws"},
			"notification_path": "/notifications/subscribe",
		})
	}))
	defer srv.Close()

	p := httpprober.New()
	result := p.Probe(context.Background(), srv.URL)

	if !result.Connected {
		t.Fatal("expected connected")
	}
	if !result.UI {
		t.Fatal("expected ui=true for 200")
	}
	if result.Manifest.Name != "sharkfin" {
		t.Errorf("name = %q, want sharkfin", result.Manifest.Name)
	}
	if result.Manifest.Display != "nav" {
		t.Errorf("display = %q, want nav", result.Manifest.Display)
	}
}

func TestProbe_NoUI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(267)
		json.NewEncoder(w).Encode(map[string]any{
			"name":  "hive",
			"label": "Hive",
			"route": "/hive",
		})
	}))
	defer srv.Close()

	p := httpprober.New()
	result := p.Probe(context.Background(), srv.URL)

	if !result.Connected {
		t.Fatal("expected connected")
	}
	if result.UI {
		t.Fatal("expected ui=false for 267")
	}
	if result.Manifest.Name != "hive" {
		t.Errorf("name = %q, want hive", result.Manifest.Name)
	}
	if result.Manifest.Display != "nav" {
		t.Errorf("display = %q, want nav (default)", result.Manifest.Display)
	}
}

func TestProbe_Unreachable(t *testing.T) {
	p := httpprober.New()
	result := p.Probe(context.Background(), "http://127.0.0.1:1")

	if result.Connected {
		t.Fatal("expected not connected")
	}
	if result.UI {
		t.Fatal("expected ui=false")
	}
}
