# Pylon Design

Pylon is the service registry daemon for WorkFort. Every fort points at a
Pylon instance via a gateway URL. Pylon polls local services at `/ui/health`,
caches their state, and serves the aggregated listing to Scope.

## Architecture

Hexagonal architecture with four layers:

```
cmd/daemon/         Composition root — wires config, poller, server
internal/config/    XDG paths, Viper defaults, YAML config loading
internal/domain/    Port interfaces, types, domain errors (no external deps)
internal/infra/     Outbound adapter: HTTP prober for /ui/health
internal/daemon/    Inbound adapter: HTTP server, registry, handlers
```

### Domain layer

- `Prober` interface — the single outbound port. Takes a service URL, returns
  a `ProbeResult` (health manifest + status).
- `HealthManifest` — the JSON body from `/ui/health`: name, label, route,
  setup_mode, admin_only, display, ws_paths, notification_path.
- `ServiceEntry` — the aggregated view: health manifest fields + base_url,
  ui, connected.

### Infra layer

- `httpprober.Prober` — implements `domain.Prober` via HTTP GET to
  `{base_url}/ui/health` with a 5-second timeout per probe.

### Daemon layer

- `Registry` — holds `[]ServiceEntry` in memory behind a `sync.RWMutex`.
  Runs a fan-out poll loop: every 10 seconds, spawns one goroutine per
  configured service URL, probes concurrently, replaces the cached list
  atomically.
- `GET /api/services` handler — reads from the registry. Auth-gated: returns
  full listing with valid JWT/API key, returns `{"passport_url": "..."}` with
  no token, returns 401 with invalid token.
- `GET /v1/health` handler — unauthenticated, follows Hive/Nexus pattern.
- Auth middleware from `github.com/Work-Fort/Passport/go/service-auth` with
  JWT + API key validators.

### Cmd layer

- Cobra root command with `daemon` subcommand.
- Reads YAML config for service URLs, wires infra prober into registry,
  starts HTTP server.

## Config

### YAML (`~/.config/pylon/config.yaml`)

```yaml
services:
  - url: "http://passport.nexus:3000"
  - url: "http://127.0.0.1:16000"
```

### CLI flags / environment variables

| Flag               | Env                    | Default       | Description                        |
|--------------------|------------------------|---------------|------------------------------------|
| `--bind`           | `PYLON_BIND`           | `127.0.0.1`   | Bind address                       |
| `--port`           | `PYLON_PORT`           | `18000`        | Listen port                        |
| `--passport-url`   | `PYLON_PASSPORT_URL`   | (required)    | Passport service URL for auth      |
| `--poll-interval`  | `PYLON_POLL_INTERVAL`  | `10s`          | How often to poll services         |
| `--log-level`      | `PYLON_LOG_LEVEL`      | `debug`        | Log level                          |

## Auth

Pylon uses `github.com/Work-Fort/Passport/go/service-auth` with JWT and
API key validators, same as Sharkfin and Hive.

`GET /api/services` behavior:

| Token state     | Response                                     | Status |
|-----------------|----------------------------------------------|--------|
| No token        | `{"passport_url": "https://..."}`            | 200    |
| Valid token      | `{"services": [...]}`                        | 200    |
| Invalid token    | `{"error": "invalid token"}`                 | 401    |

The passport URL returned to unauthenticated callers is the same
`--passport-url` value used to configure the auth middleware.

## Polling

Fan-out model: a single ticker fires every `--poll-interval` (default 10s).
Each tick spawns one goroutine per configured service URL. All probes run
concurrently with a 5-second per-probe HTTP timeout. Results replace the
cached service list atomically once all probes complete.

### Probe interpretation

| `/ui/health` result        | `connected` | `ui`  |
|----------------------------|-------------|-------|
| HTTP 200 + valid JSON      | `true`      | `true`  |
| HTTP 267 + valid JSON      | `true`      | `false` |
| Unreachable / timeout      | `false`     | `false` |
| Any other HTTP status      | `false`     | `false` |

## Response format

`GET /api/services` (authenticated) returns JSON matching Scope's
`TrackedService` struct:

```json
{
  "services": [
    {
      "name": "sharkfin",
      "label": "Chat",
      "route": "/chat",
      "base_url": "http://127.0.0.1:16000",
      "ui": true,
      "connected": true,
      "setup_mode": false,
      "admin_only": false,
      "display": "nav",
      "ws_paths": ["/ws", "/presence"],
      "notification_path": "/notifications/subscribe"
    }
  ]
}
```

## Cross-repo migration: 503 → 267

All services serving `/ui/health` currently use HTTP 503 to indicate
"registered but no UI available." This must change to HTTP 267 (unassigned
in IANA registry, safe to use). This migration is tracked separately from
Pylon's implementation.

## Reference documents

`docs/http-status-codes.md` — single source of truth for the HTTP status
code contract between services, Pylon, and Scope. To be created as part of
the implementation.
