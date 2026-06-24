package api

import (
	"net/http"

	"relay/internal/api/shared"
)

// CORSMiddleware preserves the legacy api.CORSMiddleware entrypoint while
// shared owns the transport middleware implementation.
func CORSMiddleware(next http.Handler) http.Handler {
	return shared.CORSMiddleware(next)
}
