package shared

import (
	"net/http"
	"os"
	"strings"
)

var defaultAllowedCORSOrigins = []string{
	"http://localhost:3000",
	"http://127.0.0.1:3000",
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

func allowedCORSOrigins() map[string]struct{} {
	allowed := make(map[string]struct{}, len(defaultAllowedCORSOrigins))
	for _, origin := range defaultAllowedCORSOrigins {
		allowed[origin] = struct{}{}
	}

	raw := strings.TrimSpace(os.Getenv("RELAY_CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		return allowed
	}

	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return allowed
}

func isAllowedCORSOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	_, ok := allowedCORSOrigins()[origin]
	return ok
}

// CORSMiddleware applies CORS headers for local frontend development origins.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isAllowedCORSOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, MCP-Protocol-Version, Authorization")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
