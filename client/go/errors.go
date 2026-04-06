// SPDX-License-Identifier: Apache-2.0
package client

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned by ServiceByName when the named service
	// is not in the registry.
	ErrNotFound = errors.New("service not found")

	// ErrUnauthorized is returned when Pylon responds with 401.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned when Pylon responds with 403.
	ErrForbidden = errors.New("forbidden")
)

// APIError is returned for non-2xx HTTP responses from Pylon.
type APIError struct {
	StatusCode int
	Message    string
	sentinel   error
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("pylon api error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("pylon api error %d", e.StatusCode)
}

// Unwrap allows errors.Is to match sentinel errors.
func (e *APIError) Unwrap() error { return e.sentinel }
