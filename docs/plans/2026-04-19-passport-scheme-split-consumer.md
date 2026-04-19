---
type: plan
step: "1"
title: "Passport scheme split — Pylon consumer migration"
status: approved
assessment_status: complete
provenance:
  source: cross-repo-coordination
  issue_id: "Cluster 3b (Passport, 2026-04-19)"
  roadmap_step: null
dates:
  created: "2026-04-19"
  approved: "2026-04-19"
  completed: null
related_plans:
  - passport/lead/docs/plans/2026-04-19-auth-scheme-dispatch.md
  - sharkfin/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - hive/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - flow/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
  - combine/lead/docs/plans/2026-04-19-passport-scheme-split-consumer.md
---

# Pylon — Passport Scheme Split Consumer Migration

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename Pylon's Go client constructor parameter `token` → `apiKey` and switch its outbound Authorization header to `ApiKey-v1`, update Pylon's daemon middleware to use `NewSchemeDispatch`, and update e2e tests that previously sent API keys via `Bearer`. Outbound consumer clients are API-key-only — no JWT-sending path is added.

**Background / Why:** Per TPM clarification 2026-04-19: only web browser clients use JWT; agents and services use API keys. Pylon's Go client (`client/go`) is consumed by Flow and other services — they all send `wf-svc_*` API keys. The current `client.New(pylonURL, token)` is type-dishonest about what `token` is and breaks against passport's new scheme-dispatch middleware (Bearer is now JWT-only). Renaming the parameter to `apiKey` and switching the wire format to `ApiKey-v1` fixes both. Inbound middleware in Pylon's daemon still needs both schemes for browser-routed traffic; that path's tests use `signJWT` and stay.

**Architecture:** Pylon's client (`client/go/client.go`) takes an opaque `token` and prefixes it with `Bearer`. Rename the parameter and the field to `apiKey`, switch the header to `ApiKey-v1`. The daemon's middleware (`internal/daemon/server.go:45`) flips from `NewFromValidators` to `NewSchemeDispatch`. The e2e tests (`tests/e2e/pylon_test.go`) that send API keys via `Bearer` need to switch to `ApiKey-v1` for those that are testing API-key authentication; the `Bearer garbage` negative case at line 208 stays exactly as-is (it's testing rejection of invalid bearer JWTs and should still 401), and JWT-bearing test sites driven by `signJWT` keep `Bearer` (those exercise the inbound JWT validator).

**Tech Stack:** Go 1.x, Huma. No new dependencies.

---

## Conventions

- Conventional Commits multi-line + `Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>` per commit.
- Verify with: `mise run lint && mise run test && mise run e2e`.
- Pin to local passport branch via `replace` until passport's tag is published.

---

## Pre-flight: pin to local passport branch

`client/go/go.mod` is a separate Go module (`module github.com/Work-Fort/Pylon/client/go`) but does NOT directly depend on `service-auth` — verified at planning time (the file is bare: `module …; go 1.25.0`). Only the **root** `go.mod` needs the `replace` directive. Re-run `grep service-auth client/go/go.mod` to confirm before editing — if a dep ever appears there, add a second `replace` to the client module too.

Add `replace github.com/Work-Fort/Passport/go/service-auth => /home/kazw/Work/WorkFort/passport/lead/go/service-auth` to the root `go.mod`. `go mod tidy` (in the root only) and commit.

---

### Task 1: Rename `client.New(pylonURL, token)` → `client.New(pylonURL, apiKey)` and send `ApiKey-v1`

**Files:**
- Modify: `client/go/client.go`
- Modify: `client/go/client_test.go` (the test at line 20 currently asserts `Bearer test-token`; update to assert `ApiKey-v1`)
- Modify: `client/go/doc.go` (example)

**Step 1: Failing test for the API-key path**

```go
func TestClient_APIKeySendsApiKeyV1(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"services":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "wf-svc_secret")
	_, _ = c.Services(context.Background())
	if gotAuth != "ApiKey-v1 wf-svc_secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "ApiKey-v1 wf-svc_secret")
	}
}
```

**Step 2: Implement the rename + scheme switch**

In `client/go/client.go`, rename the field `token` → `apiKey` and update the constructor:

```go
// New creates a Pylon client that authenticates with a Passport API key
// (Authorization: ApiKey-v1 <key>). API keys are recognizable by the
// wf-agent_ or wf-svc_ prefix.
//
// Pylon's outbound clients are API-key-only — JWTs are reserved for
// browser-routed traffic which never originates here.
func New(pylonURL, apiKey string) *Client {
	return &Client{
		http:    http.Client{Timeout: 10 * time.Second},
		baseURL: strings.TrimRight(pylonURL, "/"),
		apiKey:  apiKey,
	}
}
```

In `Services()`, replace:

```go
if c.token != "" {
	req.Header.Set("Authorization", "Bearer "+c.token)
}
```

with:

```go
if c.apiKey != "" {
	req.Header.Set("Authorization", "ApiKey-v1 "+c.apiKey)
}
```

Update `client/go/doc.go` example: the existing `pylon.New(svc.BaseURL, serviceToken)` call signature is unchanged; only the parameter name documentation flips.

Update the existing test at line 20 to assert `ApiKey-v1 test-token` instead of `Bearer test-token`.

**Step 3: Verify + commit**

```bash
mise run test -- ./client/go/...
git add client/go/client.go client/go/client_test.go client/go/doc.go
git commit -m "$(cat <<'EOF'
feat(client)!: rename token → apiKey; send ApiKey-v1 scheme

BREAKING CHANGE: client.New's second parameter is renamed from
"token" to "apiKey" (no signature change, but the wire format
changes from "Authorization: Bearer <key>" to "Authorization:
ApiKey-v1 <key>").

Per TPM clarification 2026-04-19: only web browser clients use JWT;
agents and services use API keys. Pylon's outbound consumers are
caller-side services, so the parameter is now type-honest. Required
by passport's scheme-dispatch middleware (Bearer is now JWT-only).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Switch the daemon middleware to `NewSchemeDispatch`

**Files:**
- Modify: `internal/daemon/server.go:45` (also affects the `softAuth` chain at line 51)

**Step 1: Add a failing daemon-level test**

In `internal/daemon/server_test.go` or `internal/daemon/handler_test.go`, add three tests that drive the actual daemon:

```go
func TestServer_BearerMalformedJWTReturns401NoVerify(t *testing.T) {
	// Sends "Bearer not.a.real.jwt" to /api/services, asserts 401
	// AND zero calls to the verify-api-key stub on the test passport.
}

func TestServer_ApiKeyV1Routes(t *testing.T) {
	// Sends "ApiKey-v1 wf-svc_xxx" to /api/services, asserts 200.
}

func TestServer_BearerForAPIKeyReturns401(t *testing.T) {
	// Sends "Bearer wf-svc_xxx" (the legacy misuse) to /api/services,
	// asserts 401 — proves no fallthrough at Pylon's daemon boundary.
}
```

**Step 2: Replace the constructor**

Replace `mw := auth.NewFromValidators(jwtV, akV)` (line 45) with `mw := auth.NewSchemeDispatch(jwtV, akV)`.

The `softAuth(jwtV, akV)` chain on line 51 also needs to be scheme-aware — if `softAuth` is a Pylon-local helper (verify with `grep -n 'func softAuth' internal/daemon/`), apply the same dispatch pattern there. If it's a thin wrapper that calls into `auth.NewFromValidators` internally, switch it to `auth.NewSchemeDispatch`. The contract is the same: any inbound request whose `Authorization` scheme doesn't match the credential type gets 401, no fallthrough.

Pylon's `NewServer` (verified at planning time, lines 30-43 of `internal/daemon/server.go`) returns an error if `authjwt.New` fails — i.e., `jwtV` is **always non-nil** by the time it reaches `NewSchemeDispatch`. No `auth.AlwaysFail` substitution is required here. (If a future refactor makes JWKS init non-fatal, use `auth.AlwaysFail(err)` from `service-auth` — do NOT define a local stub.)

**Step 3: Verify + commit**

```bash
mise run test && mise run e2e
git add internal/daemon/server.go internal/daemon/server_test.go
git commit -m "$(cat <<'EOF'
feat(daemon)!: dispatch inbound auth by Authorization scheme

BREAKING CHANGE: pylon's HTTP daemon now requires JWTs under "Bearer"
and API keys under "ApiKey-v1". The legacy validator chain is removed,
closing the local exposure to the Cluster 3b validator-fallthrough
class of bug. Inbound middleware still accepts both schemes
(browser-routed JWT; ApiKey-v1 from agents/services).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Add e2e coverage for the `ApiKey-v1` path; verify Bearer call sites stay as-is

**Files:**
- Add: a new test (or test block) in `tests/e2e/pylon_test.go`
- Verify (no edit): existing `tests/e2e/pylon_test.go` Bearer call sites at lines 150, 208, 258, 326, 387, 472

**Step 1: Inventory the existing Bearer call sites and confirm credential type**

Verified at planning time:

| Line | Call shape | Credential origin | Action |
| --- | --- | --- | --- |
| 150 | `Bearer `+token | `d.SignJWT(...)` — real JWT | LEAVE |
| 208 | `Bearer garbage` | malformed-JWT negative case | LEAVE |
| 258 | `Bearer `+token | `d.SignJWT(...)` — real JWT | LEAVE |
| 326 | `Bearer `+token | `d.SignJWT(...)` — real JWT | LEAVE |
| 387 | `Bearer `+token | `d.SignJWT(...)` — real JWT | LEAVE |
| 472 | `Bearer `+token | `d.SignJWT(...)` — real JWT | LEAVE |

All 5 positive `Bearer` sites carry real JWTs (issued by the JWKS stub via `signJWT`); they exercise the inbound JWT-acceptance path that's still needed for browser-routed traffic. They stay on `Bearer` unchanged. The `Bearer garbage` negative case at line 208 also stays — it asserts 401 on a malformed JWT, which is still correct.

Re-run the inventory before editing in case new tests landed:

```bash
cd /home/kazw/Work/WorkFort/pylon/lead
grep -n 'Bearer\|ApiKey-v1' tests/e2e/pylon_test.go
```

If a new positive Bearer site appeared whose token comes from an API-key path (not `signJWT`), flip it to `ApiKey-v1`.

**Step 2: Add the missing e2e coverage for the new dispatch behavior**

There is currently NO e2e test that exercises the API-key path through Pylon's daemon — every existing test is JWT-driven. Per assessor.md §8a ("every new feature has E2E tests"), Pylon's new scheme-dispatch behavior needs both:

- A positive test that `Authorization: ApiKey-v1 wf-svc_*` is accepted at `/api/services` and returns 200.
- A negative test (regression-prevention for Cluster 3b at the daemon boundary) that the same valid `wf-svc_*` string sent under `Bearer` is rejected with 401 and triggers zero calls to `verify-api-key`.

Add to `tests/e2e/pylon_test.go`:

```go
func TestPylon_ApiKeyV1AcceptedAtServices(t *testing.T) {
	d := newDaemon(t) // canonical pylon daemon harness
	defer d.Close()

	apiKey := d.MintServiceAPIKey("test-service") // canonical API-key minter on the harness
	req, _ := http.NewRequest("GET", d.URL+"/api/services", nil)
	req.Header.Set("Authorization", "ApiKey-v1 "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPylon_BearerForAPIKeyReturns401AndDoesNotVerify(t *testing.T) {
	d := newDaemon(t)
	defer d.Close()

	apiKey := d.MintServiceAPIKey("test-service")
	beforeCount := d.PassportStub.APIKeyVerifyCount()

	req, _ := http.NewRequest("GET", d.URL+"/api/services", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey) // wrong scheme for an API key
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (API key under Bearer must not be accepted)", resp.StatusCode)
	}
	if got := d.PassportStub.APIKeyVerifyCount(); got != beforeCount {
		t.Errorf("verify-api-key was called %d→%d times; expected no advance (no fallthrough)", beforeCount, got)
	}
}
```

(Adapt to the harness's actual API: `newDaemon`, `d.MintServiceAPIKey`, `d.PassportStub.APIKeyVerifyCount()` are placeholders for whatever Pylon's e2e harness exposes. Read `tests/e2e/` once before writing — if the harness exposes a different way to mint a service API key or count verify calls, use that. The shape — 200 on `ApiKey-v1`, 401 with no verify-call advance on `Bearer` — is load-bearing.)

**Step 3: Add the WWW-Authenticate hint for `ApiKey-v1`**

Per TPM directive 2026-04-19, Pylon's 401 responses should advertise `ApiKey-v1` in the `WWW-Authenticate` header alongside `Bearer`, so clients can discover the supported schemes. Update the daemon's 401 emission path (search for `WWW-Authenticate` and `writeError` / equivalent in `internal/daemon/`):

```go
w.Header().Set("WWW-Authenticate", `Bearer, ApiKey-v1`)
```

If Pylon's middleware delegates 401 emission to `service-auth`'s `writeError`, the header is set in passport's `service-auth` middleware itself — in that case no Pylon-side code change is needed, and this step is a verification step rather than an edit. Either way, add an assertion to one of the new tests above:

```go
if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer, ApiKey-v1" {
    t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer, ApiKey-v1")
}
```

**Step 4: Update `docs/http-status-codes.md` (if it references the wire format)**

`docs/http-status-codes.md` lines 40-41 reference `Bearer token` in the operator-facing status code table. Update those rows to the new wire format — service-token cases should show `ApiKey-v1`, and the table should mention both schemes are advertised in `WWW-Authenticate`.

**Step 5: Run e2e**

```
mise run e2e
```

Expected: all baseline tests PASS plus the two new tests PASS.

**Step 6: Commit**

```bash
git add tests/e2e/pylon_test.go docs/http-status-codes.md
git commit -m "$(cat <<'EOF'
test(e2e): add ApiKey-v1 coverage; assert Bearer-for-APIkey 401 + no verify

Pylon's daemon now dispatches inbound auth by Authorization scheme
(see previous commit). Adds e2e coverage for the new behavior:

- Positive: ApiKey-v1 wf-svc_* at /api/services returns 200.
- Negative: the same wf-svc_* under Bearer returns 401 AND triggers
  zero calls to verify-api-key — the load-bearing closure of
  Cluster 3b at Pylon's daemon boundary.

Existing Bearer call sites (lines 150, 208, 258, 326, 387, 472) all
carry real signJWT-issued JWTs and stay unchanged.

Updates docs/http-status-codes.md to reflect the new wire format
(both schemes advertised in WWW-Authenticate; service tokens shown
as ApiKey-v1).

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Drop replaces, bump deps, push

(Same shape as sharkfin Task 5 / hive Task 4.)

```bash
go get github.com/Work-Fort/Passport/go/service-auth@<tag>
go mod tidy
mise run lint && mise run test && mise run e2e
git commit -m "$(cat <<'EOF'
chore(deps): bump passport service-auth to <tag> (scheme dispatch)

Drops the local replace directive. Pylon's client now sends API keys
under ApiKey-v1 and the daemon dispatches inbound by Authorization
scheme.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Verification checklist

- [ ] `mise run lint` clean
- [ ] `mise run test` PASS
- [ ] `mise run e2e` PASS
- [ ] `client.New` test asserts `ApiKey-v1 <key>` on the wire
- [ ] Daemon uses `NewSchemeDispatch`
- [ ] No `replace` in `go.mod`
