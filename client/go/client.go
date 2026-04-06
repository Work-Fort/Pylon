// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client is an HTTP client for the Pylon service registry API.
type Client struct {
	http    http.Client
	baseURL string
	token   string
}

// New creates a Pylon client.
// pylonURL is the base URL of the Pylon daemon (e.g., "http://pylon:18000").
// token is a Passport JWT or API key sent as a Bearer token.
func New(pylonURL, token string) *Client {
	return &Client{
		http:    http.Client{Timeout: 10 * time.Second},
		baseURL: strings.TrimRight(pylonURL, "/"),
		token:   token,
	}
}

// Services returns all registered services from Pylon.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/services", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pylon request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeAPIError(resp)
	}

	var out servicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out.Services, nil
}

// decodeAPIError reads the response body and returns an *APIError. It handles
// both legacy {"error": "..."} and RFC 9457 {"detail": "..."} response formats.
func decodeAPIError(resp *http.Response) *APIError {
	var body struct {
		Error  string `json:"error"`  // legacy format
		Detail string `json:"detail"` // RFC 9457 / Huma format
	}
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck

	msg := body.Error
	if msg == "" {
		msg = body.Detail
	}

	ae := &APIError{StatusCode: resp.StatusCode, Message: msg}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		ae.sentinel = ErrUnauthorized
	case http.StatusForbidden:
		ae.sentinel = ErrForbidden
	}
	return ae
}

// ServiceByName returns a specific service by its registered name.
// Returns ErrNotFound if the service is not in the registry.
func (c *Client) ServiceByName(ctx context.Context, name string) (*Service, error) {
	services, err := c.Services(ctx)
	if err != nil {
		return nil, err
	}
	for i := range services {
		if services[i].Name == name {
			return &services[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}
