// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/Pylon/internal/domain"
)

// Registry holds the cached service list and runs the fan-out poller.
type Registry struct {
	prober domain.Prober
	urls   []string

	mu       sync.RWMutex
	services []domain.ServiceEntry
}

// NewRegistry creates a registry for the given service URLs.
func NewRegistry(prober domain.Prober, urls []string) *Registry {
	return &Registry{
		prober: prober,
		urls:   urls,
	}
}

// Services returns a snapshot of the current service list.
func (r *Registry) Services() []domain.ServiceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.ServiceEntry, len(r.services))
	copy(out, r.services)
	return out
}

// Poll probes all services concurrently and updates the cache.
func (r *Registry) Poll(ctx context.Context) {
	type indexed struct {
		idx    int
		result domain.ProbeResult
		url    string
	}

	results := make([]indexed, len(r.urls))
	var wg sync.WaitGroup

	for i, url := range r.urls {
		results[i] = indexed{idx: i, url: url}
		wg.Add(1)
		go func(ix int, u string) {
			defer wg.Done()
			results[ix].result = r.prober.Probe(ctx, u)
		}(i, url)
	}

	wg.Wait()

	entries := make([]domain.ServiceEntry, len(r.urls))
	for _, res := range results {
		pr := res.result
		entries[res.idx] = domain.ServiceEntry{
			Name:             pr.Manifest.Name,
			Label:            pr.Manifest.Label,
			Route:            pr.Manifest.Route,
			BaseURL:          res.url,
			UI:               pr.UI,
			Connected:        pr.Connected,
			SetupMode:        pr.Manifest.SetupMode,
			AdminOnly:        pr.Manifest.AdminOnly,
			Display:          pr.Manifest.Display,
			WSPaths:          pr.Manifest.WSPaths,
			NotificationPath: pr.Manifest.NotificationPath,
		}
	}

	r.mu.Lock()
	r.services = entries
	r.mu.Unlock()

	log.Debug("poll complete", "services", len(entries))
}

// Start runs the poll loop at the given interval until ctx is cancelled.
// It performs an initial poll immediately.
func (r *Registry) Start(ctx context.Context, interval time.Duration) {
	r.Poll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Poll(ctx)
		}
	}
}
