// SPDX-License-Identifier: GPL-3.0-or-later
package httpprober

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Work-Fort/Pylon/internal/domain"
)

// Prober implements domain.Prober via HTTP.
type Prober struct {
	client *http.Client
}

// New creates a Prober with a 5-second timeout.
func New() *Prober {
	return &Prober{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Probe hits GET {baseURL}/ui/health and interprets the response.
func (p *Prober) Probe(ctx context.Context, baseURL string) domain.ProbeResult {
	url := strings.TrimRight(baseURL, "/") + "/ui/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.ProbeResult{}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return domain.ProbeResult{}
	}
	defer resp.Body.Close()

	var manifest domain.HealthManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return domain.ProbeResult{Connected: true}
	}

	if manifest.Display == "" {
		manifest.Display = "nav"
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return domain.ProbeResult{Manifest: manifest, Connected: true, UI: true}
	case 267:
		return domain.ProbeResult{Manifest: manifest, Connected: true, UI: false}
	default:
		return domain.ProbeResult{}
	}
}
