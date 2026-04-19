// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"encoding/json"
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

	// Services endpoint
	mux.HandleFunc("GET /api/services", HandleServices(cfg.Registry, cfg.PassportURL))

	// Set up Passport auth middleware.
	opts := auth.DefaultOptions(cfg.PassportURL)
	jwtV, err := authjwt.New(ctx, opts.JWKSURL, opts.JWKSRefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("init JWT validator: %w", err)
	}
	akV := authapikey.New(opts.VerifyAPIKeyURL, opts.APIKeyCacheTTL)
	mw := auth.NewSchemeDispatch(jwtV, akV)

	// /api/services uses soft auth: inject identity if token is valid,
	// pass through if not. The handler decides what to return based on
	// whether an identity is present.
	// All other /api/* paths use the hard auth middleware.
	handler := routeAuth(mw(mux), softAuth(jwtV, akV)(mux), mux)

	addr := fmt.Sprintf("%s:%d", cfg.Bind, cfg.Port)

	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}, nil
}

// routeAuth selects the auth strategy per path:
//   - /v1/health: no auth
//   - /api/services: soft auth (inject identity if valid, pass through if not)
//   - everything else: hard auth (401 if no valid token)
func routeAuth(hardAuthed, softAuthed, unauthed http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			unauthed.ServeHTTP(w, r)
		case "/api/services":
			softAuthed.ServeHTTP(w, r)
		default:
			hardAuthed.ServeHTTP(w, r)
		}
	})
}

// softAuth dispatches inbound auth by Authorization scheme: "Bearer <jwt>"
// goes to jwtV, "ApiKey-v1 <key>" goes to akV. If no Authorization header
// is present the request passes through unauthenticated (allowing handlers
// to branch on identity presence). If a header is present but the scheme is
// unknown, or validation fails, 401 is returned — no cross-scheme fallthrough.
func softAuth(jwtV, akV auth.Validator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" {
				next.ServeHTTP(w, r)
				return
			}

			var v auth.Validator
			var token string
			switch {
			case len(h) > 7 && h[:7] == "Bearer ":
				token = h[7:]
				v = jwtV
			case len(h) > 10 && h[:10] == "ApiKey-v1 ":
				token = h[10:]
				v = akV
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
				return
			}

			id, err := v.Validate(r.Context(), token)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
				return
			}

			ctx := auth.ContextWithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
