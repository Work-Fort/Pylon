// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/Pylon/internal/config"
	pylonDaemon "github.com/Work-Fort/Pylon/internal/daemon"
	"github.com/Work-Fort/Pylon/internal/infra/httpprober"
)

func NewCmd() *cobra.Command {
	var bind string
	var port int
	var passportURL string
	var pollInterval string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the Pylon daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("bind") {
				bind = viper.GetString("bind")
			}
			if !cmd.Flags().Changed("port") {
				port = viper.GetInt("port")
			}
			if !cmd.Flags().Changed("passport-url") {
				passportURL = viper.GetString("passport-url")
			}
			if !cmd.Flags().Changed("poll-interval") {
				pollInterval = viper.GetString("poll-interval")
			}
			if passportURL == "" {
				return fmt.Errorf("--passport-url is required")
			}
			return run(bind, port, passportURL, pollInterval)
		},
	}

	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "Bind address")
	cmd.Flags().IntVar(&port, "port", 18000, "Listen port")
	cmd.Flags().StringVar(&passportURL, "passport-url", "",
		"Passport auth service URL (required)")
	cmd.Flags().StringVar(&pollInterval, "poll-interval", "10s",
		"Service polling interval")

	return cmd
}

func run(bind string, port int, passportURL, pollIntervalStr string) error {
	pollInterval, err := time.ParseDuration(pollIntervalStr)
	if err != nil {
		return fmt.Errorf("invalid poll-interval %q: %w", pollIntervalStr, err)
	}

	services := config.Services()
	urls := make([]string, len(services))
	for i, svc := range services {
		urls[i] = svc.URL
	}

	if len(urls) == 0 {
		log.Warn("no services configured — pylon will serve an empty listing")
	}

	prober := httpprober.New()
	registry := pylonDaemon.NewRegistry(prober, urls)

	srv, err := pylonDaemon.NewServer(context.Background(), pylonDaemon.ServerConfig{
		Bind:        bind,
		Port:        port,
		PassportURL: passportURL,
		Registry:    registry,
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	defer pollCancel()
	go registry.Start(pollCtx, pollInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		if err := pylonDaemon.ListenAndServe(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case sig := <-sigCh:
		log.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("http shutdown", "err", err)
	}

	return nil
}
