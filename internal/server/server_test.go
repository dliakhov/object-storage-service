package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dliakhov/object-storage-service/internal/server"
	"github.com/dliakhov/object-storage-service/internal/storage"
)

func TestServer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	srv := server.New(0, logger, storage.NewMemoryStore())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	tests := []struct {
		name           string
		method         string
		path           string
		requestBody    string
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
		{
			name:           "PUT /objects/{bucket}/{objectID} returns 201",
			method:         http.MethodPut,
			path:           "/objects/b/o",
			requestBody:    "hello",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "GET /objects/{bucket}/{objectID} missing returns 404",
			method:         http.MethodGet,
			path:           "/objects/b/missing",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "DELETE /objects/{bucket}/{objectID} missing returns 404",
			method:         http.MethodDelete,
			path:           "/objects/b/missing",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "POST /objects/{bucket}/{objectID} returns 405",
			method:         http.MethodPost,
			path:           "/objects/b/o",
			expectedStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body io.Reader = http.NoBody
			if tc.requestBody != "" {
				body = strings.NewReader(tc.requestBody)
			}

			req, err := http.NewRequestWithContext(t.Context(), tc.method, ts.URL+tc.path, body)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}

			resp, err := ts.Client().Do(req) //nolint:gosec // G107 false positive: URL is from httptest.Server, not user input
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

// doRequest executes an HTTP request against ts and returns the response status
// and body. The response body is always closed before returning.
func doRequest(t *testing.T, ts *httptest.Server, method, path, body string) (int, string) {
	t.Helper()

	var reqBody io.Reader = http.NoBody
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+path, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := ts.Client().Do(req) //nolint:gosec // G107 false positive: URL is from httptest.Server, not user input
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()

	b, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read body: %v", readErr)
	}

	return resp.StatusCode, string(b)
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	ts := httptest.NewServer(server.New(0, logger, storage.NewMemoryStore()).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestServerInvalidInputReturns400(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fileStore, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	logger := slog.New(slog.DiscardHandler)
	ts := httptest.NewServer(server.New(0, logger, fileStore).Handler())
	t.Cleanup(ts.Close)

	// "~" is a valid URL character but rejected by validateName, so r.PathValue
	// delivers it to the handler as-is and the store returns InvalidInputError.
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"PUT invalid objectID", http.MethodPut, "/objects/b/bad~name"},
		{"GET invalid objectID", http.MethodGet, "/objects/b/bad~name"},
		{"DELETE invalid objectID", http.MethodDelete, "/objects/b/bad~name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, _ := doRequest(t, ts, tc.method, tc.path, "")
			if status != http.StatusBadRequest {
				t.Errorf("got %d, want 400", status)
			}
		})
	}
}

func TestObjectEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("PUT returns 201 with JSON id", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		status, body := doRequest(t, ts, http.MethodPut, "/objects/bucket/myobj", "hello world")

		if status != http.StatusCreated {
			t.Errorf("expected 201, got %d", status)
		}
		if want := `{"id":"myobj"}`; strings.TrimSpace(body) != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("GET after PUT returns 200 with stored content", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket/obj1", "stored content")

		status, body := doRequest(t, ts, http.MethodGet, "/objects/bucket/obj1", "")

		if status != http.StatusOK {
			t.Errorf("expected 200, got %d", status)
		}
		if body != "stored content" {
			t.Errorf("body = %q, want %q", body, "stored content")
		}
	})

	t.Run("DELETE after PUT returns 200", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket/todelete", "data")

		status, _ := doRequest(t, ts, http.MethodDelete, "/objects/bucket/todelete", "")

		if status != http.StatusOK {
			t.Errorf("expected 200, got %d", status)
		}
	})

	t.Run("GET after DELETE returns 404", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket/gone", "data")
		doRequest(t, ts, http.MethodDelete, "/objects/bucket/gone", "")

		status, _ := doRequest(t, ts, http.MethodGet, "/objects/bucket/gone", "")

		if status != http.StatusNotFound {
			t.Errorf("expected 404, got %d", status)
		}
	})

	t.Run("deduplication: delete one of two shared-content objects, other still accessible", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket/dup-a", "shared body")
		doRequest(t, ts, http.MethodPut, "/objects/bucket/dup-b", "shared body")
		doRequest(t, ts, http.MethodDelete, "/objects/bucket/dup-a", "")

		status, body := doRequest(t, ts, http.MethodGet, "/objects/bucket/dup-b", "")

		if status != http.StatusOK {
			t.Errorf("expected 200, got %d", status)
		}
		if body != "shared body" {
			t.Errorf("body = %q, want %q", body, "shared body")
		}
	})

	t.Run("cross-bucket: same objectID in different buckets are independent", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket-a/obj", "in bucket-a")

		status, _ := doRequest(t, ts, http.MethodGet, "/objects/bucket-b/obj", "")

		if status != http.StatusNotFound {
			t.Errorf("expected 404 from different bucket, got %d", status)
		}
	})

	t.Run("overwrite: re-PUT replaces content", func(t *testing.T) {
		t.Parallel()
		ts := newTestServer(t)

		doRequest(t, ts, http.MethodPut, "/objects/bucket/obj", "v1")
		doRequest(t, ts, http.MethodPut, "/objects/bucket/obj", "v2")

		status, body := doRequest(t, ts, http.MethodGet, "/objects/bucket/obj", "")

		if status != http.StatusOK {
			t.Errorf("expected 200, got %d", status)
		}
		if body != "v2" {
			t.Errorf("body = %q, want %q", body, "v2")
		}
	})
}
