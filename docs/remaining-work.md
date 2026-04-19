# Pylon — Remaining Work

## Test Coverage Gaps

**Convention:** Unit tests live alongside source (`_test.go` suffix, same package
or `_test` external package). E2E tests live under `tests/e2e/` with their own
`go.mod`. Test helpers that construct concrete adapter types (e.g.
`httpprober.New`) must return `domain.Prober` — never the concrete
`*httpprober.Prober` — so that tests do not leak the adapter into their
signatures (enforced by `.golangci.yml` forbidigo rule).

### Conditional skips (`t.Skip`)

| File | Reason |
|------|--------|
| `tests/e2e/harness/daemon_leak_test.go:14` | `PYLON_BINARY` env var not set; test requires the compiled binary. Run via `mise run e2e`. |

### Known coverage gaps

- `internal/daemon/server.go` — server wiring and graceful-shutdown logic are
  not covered by unit tests (covered at the E2E level only).
- `internal/daemon/health.go` — health endpoint handler has no dedicated unit
  test.
- `internal/config/config.go` — config loading/validation has no unit tests.
- `cmd/` and `main.go` — CLI flag parsing and wiring have no unit tests
  (covered at the E2E level via the binary harness).
- `internal/daemon` overall coverage: 48.1 % (measured 2026-04-18).
- `internal/infra/httpprober` overall coverage: 83.3 % (measured 2026-04-18);
  the `err != nil` early-return path in `Probe` for `http.NewRequestWithContext`
  failures is not exercised.
