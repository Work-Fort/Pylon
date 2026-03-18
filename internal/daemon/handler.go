// SPDX-License-Identifier: GPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"

	auth "github.com/Work-Fort/Passport/go/service-auth"
)

// HandleServices returns the service listing (authenticated) or
// the passport URL (unauthenticated).
func HandleServices(reg *Registry, passportURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check if the request has an authenticated identity.
		_, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			// Unauthenticated — return passport URL for discovery.
			json.NewEncoder(w).Encode(map[string]string{
				"passport_url": passportURL,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"services": reg.Services(),
		})
	}
}
