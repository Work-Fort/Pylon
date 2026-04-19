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
