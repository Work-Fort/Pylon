// SPDX-License-Identifier: GPL-3.0-or-later
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Work-Fort/Pylon/tests/e2e/harness"
)

var pylonBin string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "pylon-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "pylon")

	// Build the pylon binary from the project root module.
	wd, err2 := os.Getwd()
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err2)
		os.Exit(1)
	}
	projectRoot := filepath.Join(wd, "..", "..")
	cmd := exec.Command("go", "build", "-race", "-o", binPath, ".")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build pylon: %v\n", err)
		os.Exit(1)
	}

	pylonBin = binPath
	os.Exit(m.Run())
}

// --- Tests ---

func TestHealth(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/v1/health", addr))
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "healthy" {
		t.Fatalf("expected status=healthy, got %q", body["status"])
	}
}

func TestServices_Unauthenticated(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/services", addr))
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["passport_url"] == "" {
		t.Fatal("expected passport_url to be non-empty")
	}
}

func TestServices_Authenticated(t *testing.T) {
	// Start a fake service that serves /ui/health with 200.
	fakeSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":    "test-svc",
			"label":   "Test Service",
			"route":   "/test",
			"display": "nav",
		})
	}))
	defer fakeSvc.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{fakeSvc.URL}),
		harness.WithPollInterval("1s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Wait for at least one poll cycle to complete.
	time.Sleep(2 * time.Second)

	token := d.SignJWT("uuid-alice", "alice", "Alice", "user")

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Label     string `json:"label"`
			Route     string `json:"route"`
			BaseURL   string `json:"base_url"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Services) == 0 {
		t.Fatal("expected at least one service")
	}

	svc := body.Services[0]
	if svc.Name != "test-svc" {
		t.Fatalf("expected name=test-svc, got %q", svc.Name)
	}
	if !svc.Connected {
		t.Fatal("expected connected=true")
	}
	if !svc.UI {
		t.Fatal("expected ui=true")
	}
}

func TestServices_InvalidToken(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer garbage")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, body)
	}
}

func TestServices_267NoUI(t *testing.T) {
	// Start a fake service that returns 267 (connected but no UI).
	fakeSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(267)
		json.NewEncoder(w).Encode(map[string]any{
			"name":    "headless-svc",
			"label":   "Headless Service",
			"route":   "/headless",
			"display": "nav",
		})
	}))
	defer fakeSvc.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{fakeSvc.URL}),
		harness.WithPollInterval("1s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Wait for at least one poll cycle.
	time.Sleep(2 * time.Second)

	token := d.SignJWT("uuid-bob", "bob", "Bob", "user")

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Services) == 0 {
		t.Fatal("expected at least one service")
	}

	svc := body.Services[0]
	if svc.Name != "headless-svc" {
		t.Fatalf("expected name=headless-svc, got %q", svc.Name)
	}
	if !svc.Connected {
		t.Fatal("expected connected=true")
	}
	if svc.UI {
		t.Fatal("expected ui=false")
	}
}

func TestServices_Unreachable(t *testing.T) {
	// Get a free port that nothing is listening on.
	deadAddr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{"http://" + deadAddr}),
		harness.WithPollInterval("1s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	time.Sleep(2 * time.Second)

	token := d.SignJWT("uuid-carol", "carol", "Carol", "user")

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(body.Services))
	}

	svc := body.Services[0]
	if svc.Connected {
		t.Fatal("expected connected=false for unreachable service")
	}
	if svc.UI {
		t.Fatal("expected ui=false for unreachable service")
	}
}

func TestServices_5xxError(t *testing.T) {
	// Service that responds with 500 Internal Server Error.
	fakeSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer fakeSvc.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{fakeSvc.URL}),
		harness.WithPollInterval("1s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	time.Sleep(2 * time.Second)

	token := d.SignJWT("uuid-dave", "dave", "Dave", "user")

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(body.Services))
	}

	svc := body.Services[0]
	// Server responded (connected=true) but with an error, not a health manifest
	if !svc.Connected {
		t.Fatal("expected connected=true for 5xx service (server responded)")
	}
	if svc.UI {
		t.Fatal("expected ui=false for 5xx service")
	}
}

func TestServices_Timeout(t *testing.T) {
	// Start a server that accepts connections but never responds,
	// causing the prober's 5-second timeout to fire.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	hangAddr := listener.Addr().String()

	// Accept connections in background but never write a response.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold the connection open — never respond.
			go func(c net.Conn) {
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{"http://" + hangAddr}),
		harness.WithPollInterval("1s"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Wait for the prober's 5s timeout + poll cycle.
	time.Sleep(8 * time.Second)

	token := d.SignJWT("uuid-eve", "eve", "Eve", "user")

	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/api/services", addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/services: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(body.Services))
	}

	svc := body.Services[0]
	if svc.Connected {
		t.Fatal("expected connected=false for timed-out service")
	}
	if svc.UI {
		t.Fatal("expected ui=false for timed-out service")
	}
}
