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
