package api

import (
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
