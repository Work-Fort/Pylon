// SPDX-License-Identifier: GPL-3.0-or-later

// Package client provides a Go HTTP client for the Pylon service registry API.
// It has zero dependencies on internal Pylon packages and can be imported
// freely by external consumers.
package client

// Service represents a discovered WorkFort service.
type Service struct {
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

// servicesResponse is the JSON envelope returned by GET /api/services.
type servicesResponse struct {
	Services []Service `json:"services"`
}
