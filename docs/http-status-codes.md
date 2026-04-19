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

```json
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
```

## Outbound: Pylon → Scope

Scope calls `GET /api/services` on Pylon. Pylon advertises supported
Authorization schemes in `WWW-Authenticate: Bearer, ApiKey-v1` on all
401 responses.

| Condition                     | Status | Response body                       |
|-------------------------------|--------|-------------------------------------|
| No Authorization header       | 200    | `{"passport_url": "https://..."}`   |
| Valid `Bearer <jwt>`          | 200    | `{"services": [...]}`               |
| Valid `ApiKey-v1 <wf-svc_*>`  | 200    | `{"services": [...]}`               |
| Invalid or wrong-scheme token | 401    | `{"error": "invalid token"}`        |

Scheme routing is strict: a `wf-svc_*` API key sent under `Bearer` is
rejected (401) without forwarding to the API-key validator. Only web
browser clients use JWT (`Bearer`); agents and services use API keys
(`ApiKey-v1`).

## Pylon health

Scope (or any monitoring tool) can check Pylon itself at
`GET /v1/health` (unauthenticated):

| Status | Meaning |
|--------|---------|
| 200    | Healthy |

## Migration note

HTTP 267 replaces the previous convention of HTTP 503 for "registered but
no UI." All services serving `/ui/health` must be updated accordingly.
