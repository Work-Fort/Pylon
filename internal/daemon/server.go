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

	// Services endpoint
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
