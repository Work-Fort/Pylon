# Pylon Go Client Library Requirements

## Purpose

Pylon is the service registry for the WorkFort platform. Services need to
discover each other's URLs at runtime rather than hardcoding them in config.
A Go client library allows any WorkFort service to call Pylon's API and
resolve service URLs dynamically.

This follows the same pattern as:
- `passport/go/service-auth/` — Passport's Go auth library
- `hive/client/` — Hive's Go client library

## Location

`go/client/` within the Pylon repo, importable as
`github.com/Work-Fort/Pylon/go/client`.

## Client API

```go
package client

// Client connects to a Pylon instance for service discovery.
type Client struct { ... }

// New creates a Pylon client.
// pylonURL: base URL of the Pylon daemon (e.g., "http://pylon:8080")
// token: service identity token for authentication
func New(pylonURL, token string) *Client

// Services returns all registered services from Pylon's cache.
func (c *Client) Services(ctx context.Context) ([]Service, error)

// ServiceByName returns a specific service by its registered name.
// Returns ErrNotFound if the service is not in the registry.
func (c *Client) ServiceByName(ctx context.Context, name string) (*Service, error)

// Service represents a discovered WorkFort service.
type Service struct {
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

// Sentinel errors
var (
    ErrNotFound     = errors.New("service not found")
    ErrUnauthorized = errors.New("unauthorized")
)
```

## Behavior

- Client calls `GET /api/services` with `Authorization: Bearer <token>`
- Parses the `{"services": [...]}` response
- `ServiceByName` filters by the `name` field (e.g., `"hive"`, `"sharkfin"`,
  `"combine"`, `"flow"`)
- Authentication failure (no token or invalid token) returns `ErrUnauthorized`
- Service not found in the list returns `ErrNotFound`
- If Pylon is unreachable, return a descriptive error wrapping the HTTP
  failure

## Caching (optional, recommended)

The client may cache the service list with a configurable TTL to avoid
hitting Pylon on every lookup. Pylon itself polls services every 10s, so
a client-side cache of 10-30s is reasonable.

```go
func New(pylonURL, token string, opts ...Option) *Client

type Option func(*Client)

func WithCacheTTL(d time.Duration) Option
```

Default: no caching (every call hits Pylon). This keeps the client simple
for v1 — caching can be added without API changes.

## Usage Pattern

A consuming service (e.g., Flow) uses the client at startup:

```go
pylon := client.New(cfg.PylonURL, cfg.ServiceToken)

hive, err := pylon.ServiceByName(ctx, "hive")
if err != nil { ... }
hiveAdapter := hiveinfra.New(hive.BaseURL, cfg.ServiceToken)

sharkfin, err := pylon.ServiceByName(ctx, "sharkfin")
if err != nil { ... }
sharkfinAdapter := sharkfininfra.New(sharkfin.BaseURL, cfg.ServiceToken)
```

Note: the same service token is used for both Pylon discovery and
outbound service calls. Services authenticate with a single identity.

## Testing

- Unit test with a mock HTTP server returning a canned service list
- Test `ServiceByName` with matching name, missing name, empty list
- Test auth failure (401 response → `ErrUnauthorized`)
- Test Pylon unreachable (connection refused → wrapped error)

## Dependencies

- Standard library only (`net/http`, `encoding/json`)
- No external dependencies — keep it lightweight like `service-auth`

## Out of Scope

- Dynamic service registration (services don't register with Pylon via
  API — Pylon discovers them from its config)
- Health checking (Pylon handles this; the client just reads the cached
  result)
- WebSocket or streaming (the client is stateless HTTP)
