// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/Pylon/internal/domain"
)

// --- stub validators for scheme-dispatch tests ---

type stubJWTValidator struct {
	callCount atomic.Int32
}

// Validate accepts any token starting with "jwt-", rejects everything else.
func (s *stubJWTValidator) Validate(_ context.Context, token string) (auth.Identity, error) {
	s.callCount.Add(1)
	if len(token) >= 4 && token[:4] == "jwt-" {
		return auth.Identity{ID: "test-user", Username: "testuser", Name: "Test", Type: auth.TypeUser}, nil
	}
	return auth.Identity{}, auth.ErrInvalidToken
}

type stubAPIKeyValidator struct {
	callCount atomic.Int32
}

// Validate accepts any key starting with "wf-svc_", rejects everything else.
func (s *stubAPIKeyValidator) Validate(_ context.Context, token string) (auth.Identity, error) {
	s.callCount.Add(1)
	if len(token) >= 7 && token[:7] == "wf-svc_" {
		return auth.Identity{ID: "svc-acct", Username: "service", Name: "Service", Type: auth.TypeService}, nil
	}
	return auth.Identity{}, auth.ErrInvalidToken
}

// nopProber is a no-op service prober for tests.
type nopProber struct{}

func (n *nopProber) Probe(_ context.Context, _ string) domain.ProbeResult {
	return domain.ProbeResult{}
}

// buildTestHandler constructs a handler using NewSchemeDispatch wired through
// routeAuth+softAuth exactly as NewServer does. Allows unit tests to drive
// the middleware without a real Passport URL or network.
func buildTestHandler(jwtV auth.Validator, akV auth.Validator) http.Handler {
	reg := NewRegistry(&nopProber{}, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", HandleHealth())
	mux.HandleFunc("GET /api/services", HandleServices(reg, "http://passport:3000"))

	mw := auth.NewSchemeDispatch(jwtV, akV)
	return routeAuth(mw(mux), softAuth(jwtV, akV)(mux), mux)
}

// TestServer_BearerMalformedJWTReturns401NoVerify verifies that sending a
// Bearer token that fails JWT validation returns 401 and does NOT fall
// through to the API-key validator — this is the Cluster 3b closure.
func TestServer_BearerMalformedJWTReturns401NoVerify(t *testing.T) {
	jwtV := &stubJWTValidator{}
	akV := &stubAPIKeyValidator{}
	handler := buildTestHandler(jwtV, akV)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer not.a.real.jwt")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if akV.callCount.Load() != 0 {
		t.Errorf("api-key validator called %d times; expected 0 (no fallthrough)", akV.callCount.Load())
	}
}

// TestServer_ApiKeyV1Routes verifies that a valid ApiKey-v1 token is accepted
// and returns 200 with the service listing.
func TestServer_ApiKeyV1Routes(t *testing.T) {
	jwtV := &stubJWTValidator{}
	akV := &stubAPIKeyValidator{}
	handler := buildTestHandler(jwtV, akV)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "ApiKey-v1 wf-svc_xxx")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		body := rr.Body.String()
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, body)
	}

	var resp struct {
		Services []json.RawMessage `json:"services"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if jwtV.callCount.Load() != 0 {
		t.Errorf("jwt validator called %d times; expected 0 for ApiKey-v1 request", jwtV.callCount.Load())
	}
}

// TestServer_BearerForAPIKeyReturns401 verifies that an API key sent under
// the Bearer scheme is rejected with 401 — no fallthrough to the API-key
// validator. This is the load-bearing Cluster 3b regression-prevention test.
func TestServer_BearerForAPIKeyReturns401(t *testing.T) {
	jwtV := &stubJWTValidator{}
	akV := &stubAPIKeyValidator{}
	handler := buildTestHandler(jwtV, akV)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer wf-svc_xxx")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (API key under Bearer must not be accepted)", rr.Code)
	}
	if akV.callCount.Load() != 0 {
		t.Errorf("api-key validator called %d times; expected 0 (no fallthrough)", akV.callCount.Load())
	}
}
