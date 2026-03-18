// SPDX-License-Identifier: GPL-3.0-or-later
package domain

import "context"

// Prober probes a service's /ui/health endpoint and returns the result.
type Prober interface {
	Probe(ctx context.Context, baseURL string) ProbeResult
}
