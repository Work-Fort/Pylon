// SPDX-License-Identifier: GPL-3.0-or-later
package harness

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// StartJWKSStub starts a JWKS stub server that serves:
//   - GET /v1/jwks — the public key in JWKS format
//   - POST /v1/verify-api-key — accepts any key with wf_ prefix; rejects others
//
// It returns:
//   - addr: the server address (host:port)
//   - stop: function to stop the server
//   - signJWT: function to create signed JWTs with the expected claims
//   - apiKeyVerifyCount: function that returns the number of verify-api-key calls made
func StartJWKSStub() (addr string, stop func(), signJWT func(id, username, displayName, userType string) string, apiKeyVerifyCount func() int32) {
	// Generate RSA key pair.
	rawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("jwks_stub: generate RSA key: %v", err))
	}

	// Build JWK from the private key with kid and algorithm set.
	privJWK, err := jwk.FromRaw(rawKey)
	if err != nil {
		panic(fmt.Sprintf("jwks_stub: create JWK from private key: %v", err))
	}
	_ = privJWK.Set(jwk.KeyIDKey, "test-key-1")
	_ = privJWK.Set(jwk.AlgorithmKey, jwa.RS256)

	privSet := jwk.NewSet()
	_ = privSet.AddKey(privJWK)

	pubSet, err := jwk.PublicSetOf(privSet)
	if err != nil {
		panic(fmt.Sprintf("jwks_stub: derive public key set: %v", err))
	}

	// Pre-marshal the public JWKS response.
	jwksBytes, err := json.Marshal(pubSet)
	if err != nil {
		panic(fmt.Sprintf("jwks_stub: marshal JWKS: %v", err))
	}

	// Default identity for API key verification.
	apiKeyIdentity := map[string]any{
		"valid": true,
		"key": map[string]any{
			"userId": "00000000-0000-0000-0000-000000000001",
			"metadata": map[string]any{
				"username":     "service",
				"name":         "Service Account",
				"display_name": "Service",
				"type":         "service",
			},
		},
	}

	// Counter for verify-api-key calls (used by e2e tests to assert no fallthrough).
	var verifyCount atomic.Int32

	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBytes)
	})

	mux.HandleFunc("POST /v1/verify-api-key", func(w http.ResponseWriter, r *http.Request) {
		verifyCount.Add(1)
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": "invalid request"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Only accept keys with "wf_" prefix to simulate real Passport behaviour.
		// Random tokens like "garbage" are rejected.
		if len(req.Key) < 3 || req.Key[:3] != "wf_" {
			json.NewEncoder(w).Encode(map[string]any{"valid": false, "error": "invalid key"})
			return
		}
		json.NewEncoder(w).Encode(apiKeyIdentity)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("jwks_stub: listen: %v", err))
	}

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	stopFn := func() {
		srv.Close()
	}

	signFn := func(id, username, displayName, userType string) string {
		now := time.Now()
		tok, err := jwt.NewBuilder().
			Subject(id).
			Issuer("passport-stub").
			IssuedAt(now).
			Expiration(now.Add(1*time.Hour)).
			Claim("username", username).
			Claim("name", displayName).
			Claim("display_name", displayName).
			Claim("type", userType).
			Build()
		if err != nil {
			panic(fmt.Sprintf("jwks_stub: build JWT: %v", err))
		}

		signedBytes, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privJWK))
		if err != nil {
			panic(fmt.Sprintf("jwks_stub: sign JWT: %v", err))
		}
		return string(signedBytes)
	}

	countFn := func() int32 {
		return verifyCount.Load()
	}

	return ln.Addr().String(), stopFn, signFn, countFn
}
