// SPDX-License-Identifier: GPL-3.0-or-later
package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	auth "github.com/Work-Fort/Passport/go/service-auth"
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

	// Inject authenticated identity into context
	id := auth.Identity{ID: "user-1", Username: "testuser", Name: "Test", Type: auth.TypeUser}
	ctx := auth.ContextWithIdentity(req.Context(), id)
	req = req.WithContext(ctx)

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
