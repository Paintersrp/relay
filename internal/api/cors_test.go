package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSBehavior(t *testing.T) {
	cases := []struct {
		name           string
		method         string
		origin         func() string
		setupEnv       func()
		expectedStatus int
		expectAllow    bool
		expectedOrigin string
	}{
		{
			name:           "GET from http://localhost:5173 is allowed",
			method:         "GET",
			origin:         func() string { return "http://localhost:5173" },
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://localhost:5173",
		},
		{
			name:           "GET from http://127.0.0.1:5173 is allowed",
			method:         "GET",
			origin:         func() string { return "http://127.0.0.1:5173" },
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://127.0.0.1:5173",
		},
		{
			name:           "GET from http://localhost:3000 (legacy) is allowed",
			method:         "GET",
			origin:         func() string { return "http://localhost:3000" },
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://localhost:3000",
		},
		{
			name:           "GET from disallowed origin http://evil.example is blocked",
			method:         "GET",
			origin:         func() string { return "http://evil.example" },
			expectedStatus: http.StatusOK,
			expectAllow:    false,
		},
		{
			name:           "OPTIONS preflight from allowed origin",
			method:         "OPTIONS",
			origin:         func() string { return "http://localhost:5173" },
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://localhost:5173",
		},
		{
			name:           "OPTIONS preflight from disallowed origin",
			method:         "OPTIONS",
			origin:         func() string { return "http://evil.example" },
			expectedStatus: http.StatusOK,
			expectAllow:    false,
		},
		{
			name:   "RELAY_CORS_ALLOWED_ORIGINS appends custom origin",
			method: "GET",
			origin: func() string { return "http://custom-app.local" },
			setupEnv: func() {
				t.Setenv("RELAY_CORS_ALLOWED_ORIGINS", "http://custom-app.local,http://another-app.local")
			},
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://custom-app.local",
		},
		{
			name:   "RELAY_CORS_ALLOWED_ORIGINS does not drop defaults",
			method: "GET",
			origin: func() string { return "http://localhost:5173" },
			setupEnv: func() {
				t.Setenv("RELAY_CORS_ALLOWED_ORIGINS", "http://custom-app.local")
			},
			expectedStatus: http.StatusOK,
			expectAllow:    true,
			expectedOrigin: "http://localhost:5173",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setupEnv != nil {
				tc.setupEnv()
			}

			handler := CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, "/api/runs", nil)
			if tc.origin != nil {
				req.Header.Set("Origin", tc.origin())
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
			if tc.expectAllow {
				if allowOrigin != tc.expectedOrigin {
					t.Errorf("expected Access-Control-Allow-Origin %q, got %q", tc.expectedOrigin, allowOrigin)
				}
				vary := resp.Header.Get("Vary")
				if vary != "Origin" {
					t.Errorf("expected Vary: Origin, got %q", vary)
				}
				methods := resp.Header.Get("Access-Control-Allow-Methods")
				if methods != "GET, POST, OPTIONS" {
					t.Errorf("expected allowed methods 'GET, POST, OPTIONS', got %q", methods)
				}
				headers := resp.Header.Get("Access-Control-Allow-Headers")
				expectedHeaders := "Content-Type, Accept, MCP-Protocol-Version, Authorization"
				if headers != expectedHeaders {
					t.Errorf("expected allowed headers %q, got %q", expectedHeaders, headers)
				}
			} else {
				if allowOrigin != "" {
					t.Errorf("expected no Access-Control-Allow-Origin header, got %q", allowOrigin)
				}
			}
		})
	}
}
