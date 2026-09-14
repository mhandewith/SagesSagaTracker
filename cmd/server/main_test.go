package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestEndpoints(t *testing.T) {
	storage := testStore(t)
	for _, tc := range []struct{ path, key, want string }{
		{"/", "service", "SagesSagaTracker"},
		{"/healthz", "status", "ok"},
		{"/api/v1/ping", "message", "pong"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			routes(storage).ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body[tc.key] != tc.want {
				t.Fatalf("got %v, want %s=%s", body, tc.key, tc.want)
			}
			if tc.path == "/api/v1/ping" {
				if _, err := time.Parse(time.RFC3339, body["time"]); err != nil {
					t.Fatal(err)
				}
				if body["version"] != version || body["revision"] != revision {
					t.Fatalf("missing build identity: %v", body)
				}
			}
		})
	}
}

func TestRoutingErrors(t *testing.T) {
	storage := testStore(t)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/missing", http.StatusNotFound},
		{http.MethodPost, "/api/v1/ping", http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		routes(storage).ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d", tc.method, tc.path, response.Code, tc.status)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	server := httptest.NewServer(routes(testStore(t)))
	defer server.Close()
	if err := checkHealth(server.URL + "/healthz"); err != nil {
		t.Fatal(err)
	}
	if err := checkHealth(server.URL + "/missing"); err == nil {
		t.Fatal("healthcheck must fail on non-200 status")
	}
	server.Close()
	if err := checkHealth(server.URL + "/healthz"); err == nil {
		t.Fatal("healthcheck must fail when server is unavailable")
	}
}

func testStore(t *testing.T) *store {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "sagetracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}
