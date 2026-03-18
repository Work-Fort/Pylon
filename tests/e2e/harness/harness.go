// SPDX-License-Identifier: GPL-3.0-or-later
package harness

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	cmd      *exec.Cmd
	addr     string
	xdgDir   string
	stderr   *bytes.Buffer
	stubStop func()
	signJWT  func(id, username, displayName, userType string) string
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
	stubAddr, stubStop, signJWT := StartJWKSStub()

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

	var stderrBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+filepath.Join(xdgDir, "config"),
		"XDG_STATE_HOME="+filepath.Join(xdgDir, "state"),
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
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
				cmd:      cmd,
				addr:     addr,
				xdgDir:   xdgDir,
				stderr:   &stderrBuf,
				stubStop: stubStop,
				signJWT:  signJWT,
			}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
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

// StopFatal stops the daemon and fails the test if a data race was detected.
func (d *Daemon) StopFatal(t testing.TB) {
	t.Helper()
	if err := d.Stop(); err != nil {
		t.Logf("daemon stop: %v", err)
	}
	if d.stderr != nil && strings.Contains(d.stderr.String(), "DATA RACE") {
		t.Fatal("data race detected in daemon (see stderr output above)")
	}
}

// Stop gracefully stops the daemon, JWKS stub, and cleans up the temp directory.
func (d *Daemon) Stop() error {
	if d.stubStop != nil {
		d.stubStop()
	}
	if d.cmd.Process == nil {
		return nil
	}
	d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		os.RemoveAll(d.xdgDir)
		return err
	case <-time.After(5 * time.Second):
		d.cmd.Process.Kill()
		<-done
		os.RemoveAll(d.xdgDir)
		return fmt.Errorf("daemon did not exit after SIGTERM")
	}
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
