---
type: plan
step: "1"
title: "pylon e2e harness — orphan-leak hardening"
status: approved
assessment_status: complete
provenance:
  source: roadmap
  issue_id: null
  roadmap_step: null
dates:
  created: "2026-04-19"
  approved: "2026-04-19"
  completed: null
related_plans: []
---

# Pylon E2E Harness — Orphan-Leak Hardening

**Goal:** Stop the e2e harness from leaking orphan processes when the
`pylon daemon` subprocess exits before its stderr buffer drains.
The current `tests/e2e/harness/harness.go` wires `cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)`
(line 115), which makes `exec.Cmd` create an OS pipe and a copy
goroutine. The cleanup at line 171-191 sends SIGTERM to the daemon
PID only — leaked descendants would inherit the pipe write end and
keep `cmd.Wait()` blocked until the workflow timeout fires. Pylon
doesn't fork helpers today, but the bug class still triggers any
time the daemon's own writes outlive the harness's expectation
(panics, log flushes after fatal).

**Canonical fix** (see `/home/kazw/Work/WorkFort/skills/lead/go-service-architecture/references/architecture-reference.md` — section
"Orphan-Process Hardening (Required)"):

1. **`Setpgid: true`** in `cmd.SysProcAttr`.
2. **`*os.File` for stdout/stderr**, not `io.MultiWriter`.
3. **Negative-pid kill** (`syscall.Kill(-pgid, sig)`).
4. **`cmd.WaitDelay = 10 * time.Second`** safety net.

All four parts are load-bearing.

**Repo specifics.** Pylon's harness is the cleanest of the six — single
spawn, single Stop, no Bridge or restart variants. The shape mirrors
Flow's daemon harness almost exactly. The `Stop` method also tears
down the JWKS stub, which has nothing to do with the leak fix and
stays unchanged.

**Tech stack:** Go 1.25.0 (e2e nested module), `os/exec`, `syscall`.
No new dependencies.

**Commands:** `mise run e2e` (the existing task at `.mise/tasks/e2e`)
runs `mise run build:dev` then `cd tests/e2e && go test -v -race
-timeout 180s`. Targeted runs use `cd tests/e2e && go test -run TestX
-count=1 ./harness/...`.

---

## Prerequisites

- `tests/e2e/go.mod` (Go 1.25.0) — `cmd.WaitDelay` (Go 1.20+) is
  available.
- The pylon binary is built by `mise run build:dev` and located via
  the existing harness setup.

---

## Conventions

- Run all build/test commands via `mise run <task>` from `pylon/lead/`.
  Targeted go test runs are permitted from inside `tests/e2e/`.
- Commit after each task with the multi-line conventional-commits
  HEREDOC and the Co-Authored-By trailer below.

```bash
git add <files>
git commit -m "$(cat <<'EOF'
<type>(<scope>): <description>

<body explaining why, not what>

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task Breakdown

### Task 1: Write the failing leak-detection test

**Files:**
- Create: `tests/e2e/harness/daemon_leak_test.go`

**Step 1: Write the test**

The test starts a daemon, calls `Stop`, then asserts no live process
remains in the daemon's pgid. Without `Setpgid`,
`syscall.Getpgid(daemonPID)` returns the harness's group, not the
daemon's PID — the test fails immediately.

```go
// SPDX-License-Identifier: GPL-3.0-or-later
package harness

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestDaemonStop_KillsProcessGroup(t *testing.T) {
	binary := os.Getenv("PYLON_BINARY")
	if binary == "" {
		t.Skip("PYLON_BINARY not set; run via 'mise run e2e'")
	}

	addr, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort: %v", err)
	}

	d, err := StartDaemon(binary, addr)
	if err != nil {
		t.Fatalf("StartDaemon: %v", err)
	}
	pid := d.cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	if pgid != pid {
		t.Fatalf("daemon pgid = %d, want %d (Setpgid not set)", pgid, pid)
	}
	// Defence against the (vanishingly rare) case where the test
	// process itself is in a group whose id equals the daemon PID —
	// pgid == pid would pass spuriously.
	if pgid == os.Getpid() {
		t.Fatalf("daemon pgid (%d) equals harness pid; daemon inherited harness group", pgid)
	}

	// Stop returns (stderrBytes, err); we ignore the bytes here —
	// the load-bearing assertion is on group emptiness.
	// Note: the kill assertion only covers the subprocess group;
	// in-process stubs (JWKS) are torn down by Stop independently.
	if _, err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Use errors.Is (not direct ==) because syscall.Errno implements
	// the errors.Is contract and errors.Is is the idiomatic Go choice.
	if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("kill(-%d, 0) = %v, want ESRCH (group still has live members)", pgid, err)
	}
}
```

**Step 2: Run the test to verify it fails**

The pylon binary needs to be built. The simplest path:

```
cd pylon/lead && go build -o /tmp/pylon ./
PYLON_BINARY=/tmp/pylon go test -run TestDaemonStop_KillsProcessGroup \
  -count=1 ./tests/e2e/harness/...
```

Expected: FAIL with `daemon pgid = <harness_pgid>, want <daemon_pid>
(Setpgid not set)`.

**Step 3: Commit the failing test**

```bash
git add tests/e2e/harness/daemon_leak_test.go
git commit -m "$(cat <<'EOF'
test(e2e): add failing TestDaemonStop_KillsProcessGroup

Asserts the pylon daemon spawns into its own process group and that
Stop empties the group. Currently fails because StartDaemon does not
set Setpgid; the next task fixes the harness.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Apply the four-part canonical fix to `StartDaemon`/`Stop`

**Depends on:** Task 1

**Files:**
- Modify: `tests/e2e/harness/harness.go` — `Daemon` struct (lines
  39-46), `StartDaemon` (lines 51-145), `Stop` (lines 171-191).

**Step 1: Update the imports**

Replace `bytes` and `io` (used only for the stderr buffer + multi-
writer) with `bytes` (kept for the read-back DATA RACE check) and
`syscall`. Final import block:

```go
import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)
```

`io` goes away. `bytes` stays (used by `bytes.Contains` in `StopFatal`).

**Step 2: Replace the `Daemon` struct's stderr field**

The current struct (lines 39-46) holds `stderr *bytes.Buffer`. Swap
for `stderrFile *os.File`:

```go
// Daemon wraps a running pylon daemon process.
type Daemon struct {
	cmd        *exec.Cmd
	addr       string
	xdgDir     string
	stderrFile *os.File // *os.File (not bytes.Buffer) — see hardening notes
	stubStop   func()
	signJWT    func(id, username, displayName, userType string) string
}
```

**Step 3: Rewrite the spawn block in `StartDaemon`**

Replace lines 107-121 (the `var stderrBuf bytes.Buffer`,
`cmd := exec.Command(...)`, env append, `cmd.Stdout`/`cmd.Stderr`
assignment, and `cmd.Start` block) with:

```go
	stderrFile, err := os.CreateTemp("", "pylon-e2e-stderr-*")
	if err != nil {
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("create stderr temp file: %w", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+filepath.Join(xdgDir, "config"),
		"XDG_STATE_HOME="+filepath.Join(xdgDir, "state"),
	)
	// *os.File (not io.MultiWriter) so exec.Cmd does not create a
	// copy goroutine; Setpgid puts the daemon and any descendants
	// in a fresh process group; WaitDelay force-closes any inherited
	// fds after the daemon exits. See the orphan-process hardening
	// section of go-service-architecture.
	cmd.Stdout = stderrFile
	cmd.Stderr = stderrFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second

	if err := cmd.Start(); err != nil {
		stderrFile.Close()
		os.Remove(stderrFile.Name())
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("start daemon: %w", err)
	}
```

**Step 4: Update the readiness loop**

The successful return literal at lines 128-135 needs to use the new
field:

```go
		if err == nil {
			conn.Close()
			return &Daemon{
				cmd:        cmd,
				addr:       addr,
				xdgDir:     xdgDir,
				stderrFile: stderrFile,
				stubStop:   stubStop,
				signJWT:    signJWT,
			}, nil
		}
```

The not-ready failure path (lines 140-144) needs the group-kill and
file cleanup:

```go
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	cmd.Wait()
	stderrFile.Close()
	os.Remove(stderrFile.Name())
	os.RemoveAll(xdgDir)
	stubStop()
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
```

**Step 5: Rewrite `Stop` to return captured stderr**

Replace `Stop` (lines 171-191) with a signature that returns the
captured stderr bytes alongside the wait error. This is a one-line
API change but it lets `StopFatal` read the daemon's full output —
including bytes written between SIGTERM and exit, which is exactly
where DATA RACE markers from teardown panics surface.

```go
// Stop gracefully stops the daemon, JWKS stub, and cleans up the
// temp directory. Sends SIGTERM to the daemon's process group, waits
// up to 5s, then SIGKILLs the group. Returns the captured
// stdout+stderr bytes (read after cmd.Wait so teardown writes are
// included) alongside the wait error.
func (d *Daemon) Stop() ([]byte, error) {
	if d.stubStop != nil {
		d.stubStop()
	}
	if d.cmd.Process == nil {
		// Drain stderr file even if process is gone, for symmetry.
		var stderrBytes []byte
		if d.stderrFile != nil {
			stderrBytes, _ = os.ReadFile(d.stderrFile.Name())
			d.stderrFile.Close()
			os.Remove(d.stderrFile.Name())
		}
		return stderrBytes, nil
	}
	pgid := d.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
		waitErr = fmt.Errorf("daemon did not exit after SIGTERM")
	}
	var stderrBytes []byte
	if d.stderrFile != nil {
		// Read AFTER cmd.Wait so any teardown writes (panics, defer
		// flushes, race-detector output) are captured.
		stderrBytes, _ = os.ReadFile(d.stderrFile.Name())
		d.stderrFile.Close()
		os.Remove(d.stderrFile.Name())
	}
	os.RemoveAll(d.xdgDir)
	return stderrBytes, waitErr
}
```

Update existing callers (any `d.Stop()` outside `StopFatal`) to
discard the bytes: `_, err := d.Stop()`. There is one such caller in
the test files; the change is mechanical.

**Step 6: Update `StopFatal` to use the post-Stop bytes**

`StopFatal` (lines 160-168) currently checks `d.stderr.String()` for
`DATA RACE`. With `Stop` returning the bytes after `cmd.Wait`, the
DATA RACE check now sees the daemon's complete output including
anything written during teardown:

```go
// StopFatal stops the daemon and fails the test if a data race was
// detected.
func (d *Daemon) StopFatal(t testing.TB) {
	t.Helper()
	stderrBytes, err := d.Stop()
	if err != nil {
		t.Logf("daemon stop: %v", err)
	}
	if bytes.Contains(stderrBytes, []byte("DATA RACE")) {
		t.Fatalf("data race detected in daemon stderr:\n%s", stderrBytes)
	}
}
```

This catches DATA RACE markers printed during the daemon's panic
teardown — the bytes a snapshot-before-Stop would miss.

**Step 7: Run the leak test to verify it passes**

```
cd pylon/lead && go build -o /tmp/pylon ./
PYLON_BINARY=/tmp/pylon go test -run TestDaemonStop_KillsProcessGroup \
  -count=1 ./tests/e2e/harness/...
```

Expected: PASS.

**Step 8: Run the full e2e suite to verify no regression**

Run from `pylon/lead/`:

```
mise run e2e
```

Expected: PASS. Existing tests still see the daemon start, hit
endpoints, and shut down cleanly.

**Step 9: Commit**

```bash
git add tests/e2e/harness/harness.go
git commit -m "$(cat <<'EOF'
fix(e2e): harden daemon harness against orphan-process leaks

Spawn the pylon daemon into its own process group (Setpgid),
capture stdout+stderr to an *os.File instead of an io.MultiWriter
(eliminates the copy goroutine that holds pipe fds), signal the
whole group on shutdown (kill(-pgid, ...)), and set WaitDelay so
cmd.Wait force-closes any inherited fd after the daemon exits.

Without this, a daemon that buffers output through a pipe — or any
descendant that inherits the write end — can leave the harness
blocked in cmd.Wait until the CI workflow timeout fires. Stop now
returns (stderrBytes, err) so StopFatal can see the daemon's full
output including anything written between SIGTERM and exit — exactly
where DATA RACE markers from teardown panics surface.

Implements the canonical e2e-harness orphan-leak hardening pattern
documented in skills/lead/go-service-architecture/references/architecture-reference.md
(section "Orphan-Process Hardening (Required)").

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Verify cleanup is bounded under simulated test failure

**Depends on:** Task 2

**Files:**
- (Temporary, reverted) inject `t.Fatal` into the cheapest existing
  e2e test in `tests/e2e/`.

**Step 1: Confirm working tree is clean**

Run `git status`. Expected: clean.

**Step 2: Inject a forced failure**

Pick a small existing test in `tests/e2e/` that calls `StartDaemon`
and add `t.Fatal("synthetic failure to verify cleanup bound")`
immediately after `StartDaemon` returns. Do not commit. Optionally
`git stash push -k -m "synthetic-failure"` then `git stash pop` so
the diff is recoverable.

**Step 3: Time the e2e run**

Run from `pylon/lead/`:

```
time mise run e2e
```

Expected:

- The synthetic test FAILs.
- Total wall clock under 30 seconds (typically 5-10s including
  build:dev). The harness's 5-second SIGKILL deadline is the worst
  case; the daemon should respond to SIGTERM in well under a second.

If the run exceeds 30 seconds, inspect with `ps -o pid,pgid,cmd
-p $(pgrep -f pylon.*daemon)` and re-check the four parts.

**Step 4: Revert the synthetic failure**

`git checkout -- <test_file>` to restore. Run `git status` and
confirm the working tree is clean.

**Step 5: Final regression run**

Run: `mise run e2e`
Expected: PASS, all tests green.

No commit for this task — verification only.

---

## Verification Checklist

After all tasks complete:

- [ ] `mise run e2e` passes from `pylon/lead/`.
- [ ] `TestDaemonStop_KillsProcessGroup` passes; reverting the
  `Setpgid` line in `harness.go` makes it fail with the expected
  message.
- [ ] `Daemon.stderr` is gone; `Daemon.stderrFile` is `*os.File`.
- [ ] `cmd.SysProcAttr.Setpgid == true`, `cmd.WaitDelay == 10s`,
  `cmd.Stdout`/`cmd.Stderr` both `*os.File`.
- [ ] `Stop` and the readiness-failure path use `syscall.Kill(-pgid,
  sig)`, never `cmd.Process.Signal`/`cmd.Process.Kill`.
- [ ] `Stop` returns `([]byte, error)`; `StopFatal` reads the
  returned bytes (post-`cmd.Wait`) for the DATA RACE check, so
  teardown writes are included.
- [ ] `time mise run e2e` with an injected `t.Fatal` returns in
  under 30 seconds (Task 3 spot check).

## Out of Scope

- Adding new test coverage beyond the leak test.
- Changes outside `tests/e2e/harness/harness.go` and the new test.
