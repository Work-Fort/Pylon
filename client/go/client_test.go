// SPDX-License-Identifier: Apache-2.0
package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Work-Fort/Pylon/client/go"
)

func TestServices_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			t.Errorf("path = %q, want /api/services", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "ApiKey-v1 test-token" {
			t.Errorf("auth = %q, want ApiKey-v1 test-token", got)
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

func TestServiceByName_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "hive", "label": "Hive", "base_url": "http://hive:17000", "connected": true},
				{"name": "sharkfin", "label": "Chat", "base_url": "http://sharkfin:16000", "connected": true},
			},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, "token")
	svc, err := c.ServiceByName(context.Background(), "sharkfin")
	if err != nil {
		t.Fatalf("ServiceByName() error: %v", err)
	}
	if svc.Name != "sharkfin" {
		t.Errorf("name = %q, want sharkfin", svc.Name)
	}
	if svc.BaseURL != "http://sharkfin:16000" {
		t.Errorf("base_url = %q", svc.BaseURL)
	}
}

func TestServiceByName_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{
				{"name": "hive", "label": "Hive", "base_url": "http://hive:17000"},
			},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, "token")
	_, err := c.ServiceByName(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, client.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceByName_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"services": []any{}})
	}))
	defer srv.Close()

	c := client.New(srv.URL, "token")
	_, err := c.ServiceByName(context.Background(), "anything")
	if !errors.Is(err, client.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestClient_APIKeySendsApiKeyV1(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"services":[]}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "wf-svc_secret")
	_, _ = c.Services(context.Background())
	if gotAuth != "ApiKey-v1 wf-svc_secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "ApiKey-v1 wf-svc_secret")
	}
}
