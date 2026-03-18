// SPDX-License-Identifier: GPL-3.0-or-later
package domain

// HealthManifest is the JSON body returned by a service's GET /ui/health.
type HealthManifest struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Route            string   `json:"route"`
	SetupMode        bool     `json:"setup_mode"`
	AdminOnly        bool     `json:"admin_only"`
	Display          string   `json:"display"`
	WSPaths          []string `json:"ws_paths"`
	NotificationPath *string  `json:"notification_path,omitempty"`
}

// ProbeResult is the outcome of probing a single service.
type ProbeResult struct {
	Manifest  HealthManifest
	Connected bool
	UI        bool
}

// ServiceEntry is the aggregated view of a service, ready to serve to Scope.
type ServiceEntry struct {
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	Route            string   `json:"route"`
	BaseURL          string   `json:"base_url"`
	UI               bool     `json:"ui"`
	Connected        bool     `json:"connected"`
	SetupMode        bool     `json:"setup_mode"`
	AdminOnly        bool     `json:"admin_only"`
	Display          string   `json:"display"`
	WSPaths          []string `json:"ws_paths"`
	NotificationPath *string  `json:"notification_path,omitempty"`
}
