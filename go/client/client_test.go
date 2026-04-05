// SPDX-License-Identifier: GPL-3.0-or-later
package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Work-Fort/Pylon/go/client"
)

func TestServices_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			t.Errorf("path = %q, want /api/services", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth = %q, want Bearer test-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{
					"name":      "sharkfin",
					"label":     "Chat",
					"route":     "/chat",
					"base_url":  "http://sharkfin:16000",
					"ui":        true,
					"connected": true,
					"display":   "nav",
				},
				{
					"name":      "hive",
					"label":     "Hive",
					"route":     "/hive",
					"base_url":  "http://hive:17000",
					"ui":        true,
					"connected": true,
					"display":   "menu",
				},
			},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-token")
	services, err := c.Services(context.Background())
	if err != nil {
		t.Fatalf("Services() error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("got %d services, want 2", len(services))
	}
	if services[0].Name != "sharkfin" {
		t.Errorf("name = %q, want sharkfin", services[0].Name)
	}
	if services[0].BaseURL != "http://sharkfin:16000" {
		t.Errorf("base_url = %q", services[0].BaseURL)
	}
}

func TestServices_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
	}))
	defer srv.Close()

	c := client.New(srv.URL, "bad-token")
	_, err := c.Services(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, client.ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestServices_Unreachable(t *testing.T) {
	c := client.New("http://127.0.0.1:1", "token")
	_, err := c.Services(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
