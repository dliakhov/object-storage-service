package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dliakhov/object-storage-service/internal/server"
)

func TestServer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	srv := server.New(0, logger)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "GET /health returns 200",
			method:         http.MethodGet,
			path:           "/health",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST /health returns 405",
			method:         http.MethodPost,
			path:           "/health",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "GET unknown path returns 404",
			method:         http.MethodGet,
			path:           "/unknown",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), tc.method, ts.URL+tc.path, http.NoBody)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}

			resp, err := ts.Client().Do(req) //nolint:gosec // G704 false positive: URL is from httptest.Server, not user input
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer func() {
				_, _ = io.Copy(io.Discard, resp.Body)
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("close response body: %v", closeErr)
				}
			}()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}
		})
	}
}
