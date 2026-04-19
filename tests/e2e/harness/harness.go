// SPDX-License-Identifier: GPL-3.0-or-later
package harness

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// --- Daemon ---

type daemonConfig struct {
	serviceURLs  []string
	pollInterval string
}

// DaemonOption configures the daemon process.
type DaemonOption func(*daemonConfig)

// WithServices configures the service URLs to write into the config file.
func WithServices(urls []string) DaemonOption {
	return func(c *daemonConfig) { c.serviceURLs = urls }
}

// WithPollInterval configures the poll interval flag.
func WithPollInterval(d string) DaemonOption {
	return func(c *daemonConfig) { c.pollInterval = d }
}

// Daemon wraps a running pylon daemon process.
type Daemon struct {
	cmd               *exec.Cmd
	addr              string
	xdgDir            string
	stderrFile        *os.File // *os.File (not bytes.Buffer) — see hardening notes
	stubStop          func()
	signJWT           func(id, username, displayName, userType string) string
	apiKeyVerifyCount func() int32
}

// StartDaemon builds a config file, starts the JWKS stub, and launches the
// pylon daemon binary as a subprocess. It waits for the daemon to accept TCP
// connections before returning.
func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	cfg := &daemonConfig{
		pollInterval: "10s",
	}
	for _, o := range opts {
		o(cfg)
	}

	// Start JWKS stub server before the daemon so the initial JWKS fetch succeeds.
	stubAddr, stubStop, signJWT, apiKeyVerifyCount := StartJWKSStub()

	xdgDir, err := os.MkdirTemp("", "pylon-e2e-*")
	if err != nil {
		stubStop()
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	// Write config.yaml with service URLs into the temp config dir.
	configDir := filepath.Join(xdgDir, "config", "pylon")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	if len(cfg.serviceURLs) > 0 {
		var buf bytes.Buffer
		buf.WriteString("services:\n")
		for _, u := range cfg.serviceURLs {
			buf.WriteString(fmt.Sprintf("  - url: %q\n", u))
		}
		configPath := filepath.Join(configDir, "config.yaml")
		if err := os.WriteFile(configPath, buf.Bytes(), 0644); err != nil {
			os.RemoveAll(xdgDir)
			stubStop()
			return nil, fmt.Errorf("write config: %w", err)
		}
	}

	// Parse host:port from addr for --bind and --port flags.
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("parse addr %q: %w", addr, err)
	}

	args := []string{
		"daemon",
		"--passport-url", "http://" + stubAddr,
		"--bind", host,
		"--port", portStr,
		"--log-level", "disabled",
		"--poll-interval", cfg.pollInterval,
	}

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

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return &Daemon{
				cmd:               cmd,
				addr:              addr,
				xdgDir:            xdgDir,
				stderrFile:        stderrFile,
				stubStop:          stubStop,
				signJWT:           signJWT,
				apiKeyVerifyCount: apiKeyVerifyCount,
			}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	cmd.Wait()
	stderrFile.Close()
	os.Remove(stderrFile.Name())
	os.RemoveAll(xdgDir)
	stubStop()
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
}

// Addr returns the daemon's listen address.
func (d *Daemon) Addr() string { return d.addr }

// XDGDir returns the temporary XDG directory.
func (d *Daemon) XDGDir() string { return d.xdgDir }

// SignJWT creates a signed JWT with the given identity claims.
// The token is valid for 1 hour and signed with the JWKS stub's private key.
func (d *Daemon) SignJWT(id, username, displayName, userType string) string {
	return d.signJWT(id, username, displayName, userType)
}

// MintServiceAPIKey returns a service API key string that the JWKS stub will
// accept. The stub accepts any key with the "wf_" prefix, so any value
// generated here is deterministically valid for the lifetime of this daemon.
func (d *Daemon) MintServiceAPIKey(name string) string {
	return "wf_svc-" + name
}

// APIKeyVerifyCount returns the total number of POST /v1/verify-api-key calls
// received by the JWKS stub since it started. Use this to assert that the
// API-key validator was (or was not) called for a given request.
func (d *Daemon) APIKeyVerifyCount() int32 {
	return d.apiKeyVerifyCount()
}

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

// --- Helpers ---

// FreePort returns a free TCP address on localhost.
func FreePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr, nil
}
