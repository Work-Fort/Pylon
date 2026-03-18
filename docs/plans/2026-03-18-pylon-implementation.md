# Pylon Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the Pylon service registry daemon — polls local services at `/ui/health`, serves aggregated listing at `GET /api/services` with Passport auth.

**Architecture:** Hexagonal (ports & adapters). Domain layer defines a `Prober` port interface and types. Infra layer implements the HTTP prober. Daemon layer runs the fan-out poll loop and serves the API. Cmd layer wires everything.

**Tech Stack:** Go, Cobra/Viper (CLI/config), Huma v2 (HTTP API), Passport service-auth (JWT + API key middleware), charmbracelet/log (logging), mise (task runner).

---

### Task 1: Go module and mise tasks

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `.mise/tasks/test`
- Create: `.mise/tasks/lint`
- Create: `.mise/tasks/ci`
- Create: `.mise/tasks/build/dev`
- Create: `.mise/tasks/build/release`
- Create: `.mise/tasks/clean`
- Modify: `mise.toml`

**Step 1: Initialize the Go module**

```bash
cd /home/kazw/Work/WorkFort/pylon/lead
go mod init github.com/Work-Fort/Pylon
```

**Step 2: Create main.go**

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import "github.com/Work-Fort/Pylon/cmd"

func main() {
	cmd.Execute()
}
```

**Step 3: Create mise task scripts**

Create `.mise/tasks/test`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Run tests with coverage"
set -euo pipefail

BUILD_DIR=build
mkdir -p "$BUILD_DIR"

go test -v -race -coverprofile="$BUILD_DIR/coverage.out" ./...
```

Create `.mise/tasks/lint`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Run linters"
set -euo pipefail

UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "Unformatted files:"
  echo "$UNFORMATTED"
  exit 1
fi

go vet ./...
```

Create `.mise/tasks/ci`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Run all CI checks"
#MISE depends=["lint", "test"]
set -euo pipefail
```

Create `.mise/tasks/build/dev`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Build pylon (dev, dynamic)"
#MISE sources=["**/*.go", "go.mod", "go.sum"]
#MISE outputs=["build/pylon"]
set -euo pipefail

BUILD_DIR=build
BINARY_NAME=pylon
GIT_SHORT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION="${VERSION:-dev-$GIT_SHORT_SHA}"

mkdir -p "$BUILD_DIR"

go build -ldflags "-X github.com/Work-Fort/Pylon/cmd.Version=$VERSION" -o "$BUILD_DIR/$BINARY_NAME"
echo "Built $BUILD_DIR/$BINARY_NAME"
```

Create `.mise/tasks/build/release`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Build optimized static release binary"
#MISE sources=["**/*.go", "go.mod", "go.sum"]
#MISE outputs=["build/pylon"]
set -euo pipefail

BUILD_DIR=build
BINARY_NAME=pylon
GIT_SHORT_SHA=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION="${VERSION:-dev-$GIT_SHORT_SHA}"

mkdir -p "$BUILD_DIR"

CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/Work-Fort/Pylon/cmd.Version=$VERSION" \
    -o "$BUILD_DIR/$BINARY_NAME"
echo "Built $BUILD_DIR/$BINARY_NAME (static, stripped)"
```

Create `.mise/tasks/clean`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Clean build artifacts"
set -euo pipefail

rm -rf build
```

**Step 4: Run lint to verify setup**

Run: `mise run lint`
Expected: PASS (no Go files to check yet, but should not error)

**Step 5: Commit**

```bash
git add main.go go.mod .mise/ mise.toml
git commit -m "feat: initialize Go module and mise tasks"
```

---

### Task 2: Config package

**Files:**
- Create: `internal/config/config.go`

**Step 1: Write config package**

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	EnvPrefix      = "PYLON"
	ConfigFileName = "config"
	ConfigType     = "yaml"
	DefaultBind    = "127.0.0.1"
	DefaultPort    = 18000
)

// ServiceConfig is a single service URL entry from the config file.
type ServiceConfig struct {
	URL string `mapstructure:"url"`
}

// Paths holds XDG-compliant directory paths.
type Paths struct {
	ConfigDir string
	StateDir  string
}

var GlobalPaths *Paths

func init() {
	GlobalPaths = GetPaths()
}

// GetPaths returns XDG-compliant directory paths.
func GetPaths() *Paths {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
			os.Exit(1)
		}
		configHome = filepath.Join(home, ".config")
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
			os.Exit(1)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}

	return &Paths{
		ConfigDir: filepath.Join(configHome, "pylon"),
		StateDir:  filepath.Join(stateHome, "pylon"),
	}
}

// InitDirs creates all necessary directories.
func InitDirs() error {
	dirs := []string{
		GlobalPaths.ConfigDir,
		GlobalPaths.StateDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// InitViper sets up viper defaults and config file search paths.
func InitViper() {
	viper.SetDefault("bind", DefaultBind)
	viper.SetDefault("port", DefaultPort)
	viper.SetDefault("log-level", "debug")
	viper.SetDefault("passport-url", "")
	viper.SetDefault("poll-interval", "10s")

	viper.SetConfigName(ConfigFileName)
	viper.SetConfigType(ConfigType)
	viper.AddConfigPath(GlobalPaths.ConfigDir)

	viper.SetEnvPrefix(EnvPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
}

// LoadConfig reads the config file if present.
func LoadConfig() error {
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	return nil
}

// BindFlags binds cobra flags to viper.
func BindFlags(flags *pflag.FlagSet) error {
	flagsToBind := []string{"log-level"}
	for _, name := range flagsToBind {
		if err := viper.BindPFlag(name, flags.Lookup(name)); err != nil {
			return fmt.Errorf("bind flag %s: %w", name, err)
		}
	}
	return nil
}

// Services returns the configured service URLs from the config file.
func Services() []ServiceConfig {
	var services []ServiceConfig
	viper.UnmarshalKey("services", &services)
	return services
}
```

**Step 2: Run lint**

Run: `mise run lint`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with XDG paths and viper setup"
```

---

### Task 3: Domain layer — types, ports, errors

**Files:**
- Create: `internal/domain/types.go`
- Create: `internal/domain/ports.go`
- Create: `internal/domain/errors.go`

**Step 1: Write domain types**

`internal/domain/types.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package domain

// HealthManifest is the JSON body returned by a service's GET /ui/health.
type HealthManifest struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Route            string   `json:"route"`
	SetupMode        bool     `json:"setup_mode"`
	AdminOnly        bool     `json:"admin_only"`
	Display          string   `json:"display"`
	WSPaths          []string `json:"ws_paths"`
	NotificationPath *string  `json:"notification_path,omitempty"`
}

// ProbeResult is the outcome of probing a single service.
type ProbeResult struct {
	Manifest  HealthManifest
	Connected bool
	UI        bool
}

// ServiceEntry is the aggregated view of a service, ready to serve to Scope.
type ServiceEntry struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Route            string   `json:"route"`
	BaseURL          string   `json:"base_url"`
	UI               bool     `json:"ui"`
	Connected        bool     `json:"connected"`
	SetupMode        bool     `json:"setup_mode"`
	AdminOnly        bool     `json:"admin_only"`
	Display          string   `json:"display"`
	WSPaths          []string `json:"ws_paths"`
	NotificationPath *string  `json:"notification_path,omitempty"`
}
```

`internal/domain/ports.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package domain

import "context"

// Prober probes a service's /ui/health endpoint and returns the result.
type Prober interface {
	Probe(ctx context.Context, baseURL string) ProbeResult
}
```

`internal/domain/errors.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package domain

import "errors"

var (
	// ErrUnreachable is returned when a service cannot be reached.
	ErrUnreachable = errors.New("service unreachable")
)
```

**Step 2: Run lint**

Run: `mise run lint`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/domain/
git commit -m "feat: add domain layer with types, ports, and errors"
```

---

### Task 4: Infra layer — HTTP prober

**Files:**
- Create: `internal/infra/httpprober/prober.go`
- Create: `internal/infra/httpprober/prober_test.go`

**Step 1: Write the failing test**

`internal/infra/httpprober/prober_test.go`:
```go
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
			"name":    "sharkfin",
			"label":   "Chat",
			"route":   "/chat",
			"display": "nav",
			"ws_paths": []string{"/ws"},
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
```

**Step 2: Run test to verify it fails**

Run: `mise run test`
Expected: FAIL — `httpprober` package doesn't exist yet

**Step 3: Write the implementation**

`internal/infra/httpprober/prober.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package httpprober

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Work-Fort/Pylon/internal/domain"
)

// Prober implements domain.Prober via HTTP.
type Prober struct {
	client *http.Client
}

// New creates a Prober with a 5-second timeout.
func New() *Prober {
	return &Prober{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Probe hits GET {baseURL}/ui/health and interprets the response.
func (p *Prober) Probe(ctx context.Context, baseURL string) domain.ProbeResult {
	url := strings.TrimRight(baseURL, "/") + "/ui/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.ProbeResult{}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return domain.ProbeResult{}
	}
	defer resp.Body.Close()

	var manifest domain.HealthManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return domain.ProbeResult{Connected: true}
	}

	if manifest.Display == "" {
		manifest.Display = "nav"
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return domain.ProbeResult{Manifest: manifest, Connected: true, UI: true}
	case 267:
		return domain.ProbeResult{Manifest: manifest, Connected: true, UI: false}
	default:
		return domain.ProbeResult{}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `mise run test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/infra/
git commit -m "feat: add HTTP prober for /ui/health with 200/267 support"
```

---

### Task 5: Daemon layer — registry with fan-out poller

**Files:**
- Create: `internal/daemon/registry.go`
- Create: `internal/daemon/registry_test.go`

**Step 1: Write the failing test**

`internal/daemon/registry_test.go` — tests the registry's `Poll` and `Services` methods using a fake prober:

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/Work-Fort/Pylon/internal/daemon"
	"github.com/Work-Fort/Pylon/internal/domain"
)

type fakeProber struct {
	results map[string]domain.ProbeResult
}

func (f *fakeProber) Probe(_ context.Context, baseURL string) domain.ProbeResult {
	if r, ok := f.results[baseURL]; ok {
		return r
	}
	return domain.ProbeResult{}
}

func TestRegistry_Poll(t *testing.T) {
	prober := &fakeProber{results: map[string]domain.ProbeResult{
		"http://svc-a:3000": {
			Manifest:  domain.HealthManifest{Name: "auth", Label: "Auth", Route: "/auth", Display: "nav"},
			Connected: true,
			UI:        true,
		},
		"http://svc-b:4000": {
			Manifest:  domain.HealthManifest{Name: "chat", Label: "Chat", Route: "/chat", Display: "nav"},
			Connected: true,
			UI:        false,
		},
	}}

	urls := []string{"http://svc-a:3000", "http://svc-b:4000", "http://svc-c:5000"}
	reg := daemon.NewRegistry(prober, urls)
	reg.Poll(context.Background())

	services := reg.Services()
	if len(services) != 3 {
		t.Fatalf("got %d services, want 3", len(services))
	}

	// Find by name
	byName := map[string]domain.ServiceEntry{}
	for _, s := range services {
		byName[s.Name] = s
	}

	auth := byName["auth"]
	if !auth.Connected || !auth.UI {
		t.Errorf("auth: connected=%v ui=%v, want true/true", auth.Connected, auth.UI)
	}
	if auth.BaseURL != "http://svc-a:3000" {
		t.Errorf("auth base_url = %q", auth.BaseURL)
	}

	chat := byName["chat"]
	if !chat.Connected || chat.UI {
		t.Errorf("chat: connected=%v ui=%v, want true/false", chat.Connected, chat.UI)
	}

	// svc-c is unreachable — should still appear with connected=false
	found := false
	for _, s := range services {
		if s.BaseURL == "http://svc-c:5000" && !s.Connected {
			found = true
		}
	}
	if !found {
		t.Error("expected unreachable service entry for svc-c")
	}
}

func TestRegistry_StartStop(t *testing.T) {
	prober := &fakeProber{results: map[string]domain.ProbeResult{
		"http://svc:3000": {
			Manifest:  domain.HealthManifest{Name: "test", Label: "Test", Route: "/test", Display: "nav"},
			Connected: true,
			UI:        true,
		},
	}}

	reg := daemon.NewRegistry(prober, []string{"http://svc:3000"})

	ctx, cancel := context.WithCancel(context.Background())
	go reg.Start(ctx, 50*time.Millisecond)

	// Wait for at least one poll cycle
	time.Sleep(100 * time.Millisecond)

	services := reg.Services()
	if len(services) != 1 {
		t.Fatalf("got %d services, want 1", len(services))
	}
	if services[0].Name != "test" {
		t.Errorf("name = %q, want test", services[0].Name)
	}

	cancel()
}
```

**Step 2: Run test to verify it fails**

Run: `mise run test`
Expected: FAIL — `daemon` package doesn't exist

**Step 3: Write the implementation**

`internal/daemon/registry.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/Pylon/internal/domain"
)

// Registry holds the cached service list and runs the fan-out poller.
type Registry struct {
	prober domain.Prober
	urls   []string

	mu       sync.RWMutex
	services []domain.ServiceEntry
}

// NewRegistry creates a registry for the given service URLs.
func NewRegistry(prober domain.Prober, urls []string) *Registry {
	return &Registry{
		prober: prober,
		urls:   urls,
	}
}

// Services returns a snapshot of the current service list.
func (r *Registry) Services() []domain.ServiceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.ServiceEntry, len(r.services))
	copy(out, r.services)
	return out
}

// Poll probes all services concurrently and updates the cache.
func (r *Registry) Poll(ctx context.Context) {
	type indexed struct {
		idx    int
		result domain.ProbeResult
		url    string
	}

	results := make([]indexed, len(r.urls))
	var wg sync.WaitGroup

	for i, url := range r.urls {
		results[i] = indexed{idx: i, url: url}
		wg.Add(1)
		go func(ix int, u string) {
			defer wg.Done()
			results[ix].result = r.prober.Probe(ctx, u)
		}(i, url)
	}

	wg.Wait()

	entries := make([]domain.ServiceEntry, len(r.urls))
	for _, res := range results {
		pr := res.result
		entries[res.idx] = domain.ServiceEntry{
			Name:             pr.Manifest.Name,
			Label:            pr.Manifest.Label,
			Route:            pr.Manifest.Route,
			BaseURL:          res.url,
			UI:               pr.UI,
			Connected:        pr.Connected,
			SetupMode:        pr.Manifest.SetupMode,
			AdminOnly:        pr.Manifest.AdminOnly,
			Display:          pr.Manifest.Display,
			WSPaths:          pr.Manifest.WSPaths,
			NotificationPath: pr.Manifest.NotificationPath,
		}
	}

	r.mu.Lock()
	r.services = entries
	r.mu.Unlock()

	log.Debug("poll complete", "services", len(entries))
}

// Start runs the poll loop at the given interval until ctx is cancelled.
// It performs an initial poll immediately.
func (r *Registry) Start(ctx context.Context, interval time.Duration) {
	r.Poll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Poll(ctx)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `mise run test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat: add registry with fan-out poller"
```

---

### Task 6: Daemon layer — HTTP server and handlers

**Files:**
- Create: `internal/daemon/server.go`
- Create: `internal/daemon/handler.go`
- Create: `internal/daemon/health.go`
- Create: `internal/daemon/handler_test.go`

**Step 1: Write the failing test**

`internal/daemon/handler_test.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Work-Fort/Pylon/internal/daemon"
	"github.com/Work-Fort/Pylon/internal/domain"
)

func TestHandleServices_Authenticated(t *testing.T) {
	prober := &fakeProber{results: map[string]domain.ProbeResult{
		"http://svc:3000": {
			Manifest:  domain.HealthManifest{Name: "chat", Label: "Chat", Route: "/chat", Display: "nav"},
			Connected: true,
			UI:        true,
		},
	}}
	reg := daemon.NewRegistry(prober, []string{"http://svc:3000"})
	reg.Poll(t.Context())

	handler := daemon.HandleServices(reg, "http://passport:3000")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("X-Pylon-Authenticated", "true")

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var body struct {
		Services []domain.ServiceEntry `json:"services"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(body.Services))
	}
	if body.Services[0].Name != "chat" {
		t.Errorf("name = %q, want chat", body.Services[0].Name)
	}
}

func TestHandleServices_Unauthenticated(t *testing.T) {
	reg := daemon.NewRegistry(&fakeProber{}, nil)
	handler := daemon.HandleServices(reg, "http://passport:3000")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	// No auth header

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var body struct {
		PassportURL string `json:"passport_url"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.PassportURL != "http://passport:3000" {
		t.Errorf("passport_url = %q", body.PassportURL)
	}
}
```

Note: These tests use a header `X-Pylon-Authenticated` as a stand-in for
the real Passport middleware. The handler checks whether the request has an
identity in context (set by auth middleware). In tests, we simulate this.
The actual auth wiring happens in `server.go`.

**Step 2: Run test to verify it fails**

Run: `mise run test`
Expected: FAIL — handler functions don't exist

**Step 3: Write handler, health, and server**

`internal/daemon/handler.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"

	auth "github.com/Work-Fort/Passport/go/service-auth"
)

// HandleServices returns the service listing (authenticated) or
// the passport URL (unauthenticated).
func HandleServices(reg *Registry, passportURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check if the request has an authenticated identity.
		_, err := auth.IdentityFromContext(r.Context())
		if err != nil {
			// Unauthenticated — return passport URL for discovery.
			json.NewEncoder(w).Encode(map[string]string{
				"passport_url": passportURL,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"services": reg.Services(),
		})
	}
}
```

`internal/daemon/health.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"
)

// HandleHealth returns Pylon's own health status.
func HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
		})
	}
}
```

`internal/daemon/server.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	authapikey "github.com/Work-Fort/Passport/go/service-auth/apikey"
	authjwt "github.com/Work-Fort/Passport/go/service-auth/jwt"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Bind        string
	Port        int
	PassportURL string
	Registry    *Registry
}

// NewServer creates and configures the HTTP server.
func NewServer(ctx context.Context, cfg ServerConfig) (*http.Server, error) {
	mux := http.NewServeMux()

	// Health — unauthenticated
	mux.HandleFunc("GET /v1/health", HandleHealth())

	// Services — requires auth middleware to set identity in context,
	// but the handler itself checks for identity presence to decide
	// what to return (full listing vs passport URL).
	mux.HandleFunc("GET /api/services", HandleServices(cfg.Registry, cfg.PassportURL))

	// Set up Passport auth middleware.
	opts := auth.DefaultOptions(cfg.PassportURL)
	jwtV, err := authjwt.New(ctx, opts.JWKSURL, opts.JWKSRefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("init JWT validator: %w", err)
	}
	akV := authapikey.New(opts.VerifyAPIKeyURL, opts.APIKeyCacheTTL)
	mw := auth.NewFromValidators(jwtV, akV)

	// Wrap mux: auth middleware for /api/*, skip for /v1/health
	handler := publicPathSkip(mw(mux), mux)

	addr := fmt.Sprintf("%s:%d", cfg.Bind, cfg.Port)

	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}, nil
}

// publicPathSkip routes public paths directly to the mux (no auth),
// and everything else through the auth-wrapped handler.
func publicPathSkip(authed, unauthed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			unauthed.ServeHTTP(w, r)
		default:
			authed.ServeHTTP(w, r)
		}
	})
}

// ListenAndServe starts the server on the configured address.
func ListenAndServe(srv *http.Server) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	}
	log.Info("pylon listening", "addr", ln.Addr())
	return srv.Serve(ln)
}
```

**Step 4: Update handler test to use auth context helper**

The test needs to inject an identity into the request context for the
"authenticated" case. Update `TestHandleServices_Authenticated` to use
`auth.ContextWithIdentity` instead of the placeholder header.

**Step 5: Run test to verify it passes**

Run: `mise run test`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/daemon/
git commit -m "feat: add HTTP server, service listing handler, and health endpoint"
```

---

### Task 7: Cmd layer — root command and daemon subcommand

**Files:**
- Create: `cmd/root.go`
- Create: `cmd/daemon/daemon.go`

**Step 1: Write root command**

`cmd/root.go`:
```go
// SPDX-License-Identifier: GPL-3.0-or-later
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	daemonCmd "github.com/Work-Fort/Pylon/cmd/daemon"
	"github.com/Work-Fort/Pylon/internal/config"
)

var Version string

var rootCmd = &cobra.Command{
	Use:   "pylon",
	Short: "Pylon service registry daemon",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := config.InitDirs(); err != nil {
			return err
		}
		if err := config.LoadConfig(); err != nil {
			return err
		}

		ll := viper.GetString("log-level")
		if ll == "disabled" {
			log.SetOutput(io.Discard)
			return nil
		}

		var level log.Level
		switch ll {
		case "debug":
			level = log.DebugLevel
		case "info":
			level = log.InfoLevel
		case "warn":
			level = log.WarnLevel
		case "error":
			level = log.ErrorLevel
		default:
			level = log.DebugLevel
		}

		logPath := filepath.Join(config.GlobalPaths.StateDir, "debug.log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}

		logger := log.NewWithOptions(f, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "2006-01-02T15:04:05.000Z07:00",
			Level:           level,
			ReportCaller:    true,
			Formatter:       log.JSONFormatter,
		})
		log.SetDefault(logger)

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	config.InitViper()

	rootCmd.PersistentFlags().StringP("log-level", "l", "debug",
		"Log level: disabled, debug, info, warn, error")

	if err := config.BindFlags(rootCmd.PersistentFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	if Version != "" {
		rootCmd.Version = Version
	} else {
		rootCmd.Version = "dev"
	}
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	rootCmd.AddCommand(daemonCmd.NewCmd())
}
```

`cmd/daemon/daemon.go` — wires config, prober, registry, and server:

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/Pylon/internal/config"
	pylonDaemon "github.com/Work-Fort/Pylon/internal/daemon"
	"github.com/Work-Fort/Pylon/internal/infra/httpprober"
)

func NewCmd() *cobra.Command {
	var bind string
	var port int
	var passportURL string
	var pollInterval string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the Pylon daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("bind") {
				bind = viper.GetString("bind")
			}
			if !cmd.Flags().Changed("port") {
				port = viper.GetInt("port")
			}
			if !cmd.Flags().Changed("passport-url") {
				passportURL = viper.GetString("passport-url")
			}
			if !cmd.Flags().Changed("poll-interval") {
				pollInterval = viper.GetString("poll-interval")
			}
			if passportURL == "" {
				return fmt.Errorf("--passport-url is required")
			}
			return run(bind, port, passportURL, pollInterval)
		},
	}

	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Bind address")
	cmd.Flags().IntVar(&port, "port", 18000, "Listen port")
	cmd.Flags().StringVar(&passportURL, "passport-url", "",
		"Passport auth service URL (required)")
	cmd.Flags().StringVar(&pollInterval, "poll-interval", "10s",
		"Service polling interval")

	return cmd
}

func run(bind string, port int, passportURL, pollIntervalStr string) error {
	pollInterval, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		return fmt.Errorf("invalid poll-interval %q: %w", pollIntervalStr, err)
	}

	// Read service URLs from config.
	services := config.Services()
	urls := make([]string, len(services))
	for i, svc := range services {
		urls[i] = svc.URL
	}

	if len(urls) == 0 {
		log.Warn("no services configured — pylon will serve an empty listing")
	}

	// Wire hexagonal layers.
	prober := httpprober.New()
	registry := pylonDaemon.NewRegistry(prober, urls)

	srv, err := pylonDaemon.NewServer(context.Background(), pylonDaemon.ServerConfig{
		Bind:        bind,
		Port:        port,
		PassportURL: passportURL,
		Registry:    registry,
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Start poller.
	pollCtx, pollCancel := context.WithCancel(context.Background())
	defer pollCancel()
	go registry.Start(pollCtx, pollInterval)

	// Start HTTP server.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		if err := pylonDaemon.ListenAndServe(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case sig := <-sigCh:
		log.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("http shutdown", "err", err)
	}

	return nil
}
```

**Step 2: Run lint and build**

Run: `mise run lint && mise run build/dev`
Expected: PASS — binary built at `build/pylon`

**Step 3: Commit**

```bash
git add cmd/ main.go
git commit -m "feat: add CLI with root command and daemon subcommand"
```

---

### Task 8: HTTP status code reference document

**Files:**
- Create: `docs/http-status-codes.md`

**Step 1: Write the reference document**

`docs/http-status-codes.md`:
```markdown
# HTTP Status Code Contract

This document defines the HTTP status codes used between services, Pylon,
and Scope. It is the single source of truth for this contract.

## Inbound: Services → Pylon

Pylon polls each service at `GET /ui/health`. These are the expected
responses:

| Status | Meaning                          | Pylon interpretation          |
|--------|----------------------------------|-------------------------------|
| 200    | Service healthy, UI available    | `connected: true, ui: true`   |
| 267    | Service healthy, no UI yet       | `connected: true, ui: false`  |
| Other  | Unexpected state                 | `connected: false, ui: false` |
| N/A    | Unreachable / timeout            | `connected: false, ui: false` |

Both 200 and 267 responses must include a JSON body with the health
manifest:

    {
      "name": "string (required)",
      "label": "string (required)",
      "route": "string (required)",
      "setup_mode": false,
      "admin_only": false,
      "display": "nav",
      "ws_paths": [],
      "notification_path": null
    }

## Outbound: Pylon → Scope

Scope calls `GET /api/services` on Pylon.

| Condition          | Status | Response body                       |
|--------------------|--------|-------------------------------------|
| No Bearer token    | 200    | `{"passport_url": "https://..."}`   |
| Valid Bearer token | 200    | `{"services": [...]}`               |
| Invalid token      | 401    | `{"error": "invalid token"}`        |

## Pylon health

Scope (or any monitoring tool) can check Pylon itself at
`GET /v1/health` (unauthenticated):

| Status | Meaning |
|--------|---------|
| 200    | Healthy |

## Migration note

HTTP 267 replaces the previous convention of HTTP 503 for "registered but
no UI." All services serving `/ui/health` must be updated accordingly.
```

**Step 2: Commit**

```bash
git add docs/http-status-codes.md
git commit -m "docs: add HTTP status code contract reference"
```

---

### Task 9: Go dependency resolution and full CI check

**Step 1: Fetch all dependencies**

```bash
go mod tidy
```

**Step 2: Run full CI**

Run: `mise run ci`
Expected: PASS — lint, then all tests

**Step 3: Commit go.sum**

```bash
git add go.mod go.sum
git commit -m "chore: add go.sum after dependency resolution"
```

---

### Task 10: End-to-end test suite

Standalone e2e test module — separate go.mod, zero imports from Pylon
internals. Builds the binary, spawns it as a subprocess, tests over HTTP.
Can survive a full rewrite of Pylon in another language.

**Files:**
- Create: `tests/e2e/go.mod`
- Create: `tests/e2e/harness/harness.go`
- Create: `tests/e2e/harness/jwks_stub.go`
- Create: `tests/e2e/pylon_test.go`
- Create: `.mise/tasks/e2e`

**Step 1: Create the e2e Go module**

`tests/e2e/go.mod`:
```
module github.com/Work-Fort/Pylon/tests/e2e

go 1.26.0

require (
	github.com/lestrrat-go/jwx/v2 v2.1.6
)
```

Then: `cd tests/e2e && go mod tidy`

**Step 2: Write the JWKS stub**

`tests/e2e/harness/jwks_stub.go` — starts a local HTTP server that serves
a JWKS endpoint and provides a `SignJWT` function for test tokens. Follow
Sharkfin's pattern exactly (same RSA key generation, same JWT claims
structure).

**Step 3: Write the test harness**

`tests/e2e/harness/harness.go`:
- `StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error)`
  — starts the JWKS stub, writes a temp config with service URLs, spawns
  the pylon binary with `--passport-url` pointing at the stub, waits for
  TCP readiness
- `Daemon.Stop()` — sends SIGTERM, waits, cleans up temp dirs
- `Daemon.SignJWT(id, username, displayName, userType string) string`
- `Daemon.Addr() string`
- `FreePort() (string, error)`
- `DaemonOption`: `WithServices(urls []string)` to configure which fake
  services the daemon should poll

**Step 4: Write the e2e tests**

`tests/e2e/pylon_test.go`:

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package e2e

import (
	"encoding/json"
	"fmt"
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
	wd, _ := os.Getwd()
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
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "healthy" {
		t.Errorf("status = %q", body["status"])
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
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		PassportURL string `json:"passport_url"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.PassportURL == "" {
		t.Error("expected passport_url in unauthenticated response")
	}
}

func TestServices_Authenticated(t *testing.T) {
	// Start a fake service that serves /ui/health
	fakeSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":    "fakesvc",
			"label":   "Fake",
			"route":   "/fake",
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
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Wait for at least one poll cycle
	time.Sleep(2 * time.Second)

	token := d.SignJWT("user-1", "testuser", "Test", "user")
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("http://%s/api/services", addr), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(body.Services))
	}
	svc := body.Services[0]
	if svc.Name != "fakesvc" {
		t.Errorf("name = %q, want fakesvc", svc.Name)
	}
	if !svc.Connected || !svc.UI {
		t.Errorf("connected=%v ui=%v, want true/true", svc.Connected, svc.UI)
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

	req, _ := http.NewRequest("GET",
		fmt.Sprintf("http://%s/api/services", addr), nil)
	req.Header.Set("Authorization", "Bearer garbage-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServices_267NoUI(t *testing.T) {
	// Fake service returning 267 (registered, no UI)
	fakeSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(267)
		json.NewEncoder(w).Encode(map[string]any{
			"name":  "noui",
			"label": "No UI",
			"route": "/noui",
		})
	}))
	defer fakeSvc.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(pylonBin, addr,
		harness.WithServices([]string{fakeSvc.URL}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	time.Sleep(2 * time.Second)

	token := d.SignJWT("user-1", "testuser", "Test", "user")
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("http://%s/api/services", addr), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Services []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
			UI        bool   `json:"ui"`
		} `json:"services"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(body.Services))
	}
	svc := body.Services[0]
	if !svc.Connected {
		t.Error("expected connected=true")
	}
	if svc.UI {
		t.Error("expected ui=false for 267 service")
	}
}
```

**Step 5: Create the e2e mise task**

`.mise/tasks/e2e`:
```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#MISE description="Run end-to-end tests"
#MISE depends=["build/dev"]
#MISE dir="tests/e2e"
set -euo pipefail

go test -v -race -timeout 180s
```

**Step 6: Run e2e tests**

Run: `mise run e2e`
Expected: PASS — all five tests pass

**Step 7: Commit**

```bash
git add tests/e2e/ .mise/tasks/e2e
git commit -m "test: add standalone e2e test suite with JWKS stub and fake services"
```
