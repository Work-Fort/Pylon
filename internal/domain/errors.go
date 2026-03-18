// SPDX-License-Identifier: GPL-3.0-or-later
package domain

import "errors"

var (
	// ErrUnreachable is returned when a service cannot be reached.
	ErrUnreachable = errors.New("service unreachable")
)
