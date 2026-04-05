# Service Listing Requirements

Pylon is the service registry for a fort. Instead of Scope probing individual
services via `/ui/health`, it points at a single Pylon instance that provides
the full service listing for the fort.

## Current local config (what Pylon replaces)

Today each fort lists service URLs in `~/.config/workfort/config.yaml`:

```yaml
forts:
  local:
    local: true
    services:
      - url: "http://passport.nexus:3000"
      - url: "http://127.0.0.1:16000"
```

Scope's BFF probes each URL at `/ui/health` every 10 seconds and assembles
the service list. With Pylon, Scope gets that list directly — Pylon owns
the probing.

## Service health manifest

Every service that wants to appear in the shell must serve
`GET /ui/health` returning JSON:

```json
{
  "name": "sharkfin",
  "label": "Chat",
  "route": "/chat",
  "setup_mode": false,
  "admin_only": false,
  "display": "nav",
  "ws_paths": ["/ws", "/presence"],
  "notification_path": "/notifications/subscribe"
}
```

| Field               | Type       | Default  | Description                                              |
|---------------------|------------|----------|----------------------------------------------------------|
| `name`              | string     | required | Unique service identifier, used in proxy paths           |
| `label`             | string     | required | Human-readable name shown in the shell nav               |
| `route`             | string     | required | Shell route path (e.g. `/chat`)                          |
| `setup_mode`        | bool       | `false`  | If true, shell shows setup UI instead of the service     |
| `admin_only`        | bool       | `false`  | If true, service is only visible to admin users          |
| `display`           | string     | `"nav"`  | `"nav"` shows in navbar, `"menu"` shows in overflow menu |
| `ws_paths`          | string[]   | `[]`     | WebSocket paths the BFF should proxy                     |
| `notification_path` | string?    | `null`   | WebSocket path for notification subscription             |

A successful `200` response means the service has a UI (Module Federation
remote). The BFF will attempt to load `remoteEntry.js` from `/ui/remoteEntry.js`
on the same origin.

A `503` with the same JSON body means the service is registered but has no
UI available yet.

## What Pylon serves

Pylon aggregates these manifests and serves the assembled list. Scope will
call Pylon the same way it currently calls its own `/api/services` endpoint.

### `GET /api/services`

```json
{
  "services": [
    {
      "name": "auth",
      "label": "Auth",
      "route": "/auth",
      "base_url": "http://passport.nexus:3000",
      "ui": true,
      "connected": true,
      "setup_mode": false,
      "admin_only": false,
      "display": "nav",
      "ws_paths": [],
      "notification_path": null
    },
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

| Field       | Type   | Description                                                      |
|-------------|--------|------------------------------------------------------------------|
| `ui`        | bool   | `true` if `/ui/health` returned 200 (has remoteEntry.js)        |
| `connected` | bool   | `true` if the service is reachable                               |
| `base_url`  | string | Origin URL of the service — used by the BFF for proxying         |

All other fields are passed through from the service's health manifest.

## How Scope connects to Pylon

In the fort config, a non-local fort with a `pylon` URL points at Pylon:

```yaml
forts:
  acme:
    local: false
    pylon: "https://pylon.acme.workfort.dev"
```

When `local` is `false`, Scope fetches the service list from Pylon
instead of probing individual URLs.

**Pylon is not a proxy in this version.** It only serves the service listing.
The `base_url` in each service entry is the direct URL to the service.
Scope's BFF uses those URLs to proxy traffic to each service individually,
the same way it does in local mode today. A future version may add proxy
support to Pylon, but that is out of scope here.

## Scope-side requirements

### Fort selection

When the user opens the fort picker, Scope must contact the Pylon server
**before** displaying the fort list. This ensures the service URLs are
current — services may have been added, removed, or moved since the last
check.

### HTTP URL warning

If Pylon returns any `base_url` using `http://` (not `https://`), Scope
must display a warning in the fort selector. Traffic to those services
will not be encrypted.

### Service polling

After a fort is selected, Scope polls Pylon every **2 minutes** for updated
service listings. If new services appear, they are added to the shell
immediately (registered as MF remotes and shown in navigation). Removed
services should be hidden from navigation but do not need to be unloaded
if already mounted.

## Proxy path contract

Scope constructs proxy paths as:

```
/forts/{fort}/api/{service}/...
```

For example, to reach Sharkfin's WebSocket:

```
/forts/acme/api/sharkfin/ws
```

Scope's BFF proxies directly to `{base_url}/{path}` using the URL from
the service listing. Pylon is not in the request path.
