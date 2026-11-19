package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skoveit/skovenet/static"
)

func TestGraphPageServesEmbeddedViewerWithoutFetchingTopology(t *testing.T) {
	t.Parallel()
	handler := graphHandler(func() (string, error) {
		t.Error("viewer request fetched topology")
		return "", nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?interval=5", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html" {
		t.Fatalf("viewer status/content type = %d, %q", response.Code, response.Header().Get("Content-Type"))
	}
	if len(static.GraphHTML) == 0 || !bytes.Equal(response.Body.Bytes(), static.GraphHTML) {
		t.Error("viewer did not return the embedded HTML")
	}
}

func TestGraphTopologyPassesThroughJSON(t *testing.T) {
	t.Parallel()
	const topology = `{"nodes":[{"id":"peer-alpha"},{"id":"peer-beta"}],"edges":[{"source":"peer-alpha","target":"peer-beta"}]}`
	calls := 0
	handler := graphHandler(func() (string, error) {
		calls++
		return topology, nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("topology status/content type = %d, %q", response.Code, response.Header().Get("Content-Type"))
	}
	if calls != 1 || response.Body.String() != topology || !json.Valid(response.Body.Bytes()) {
		t.Errorf("topology calls=%d, body=%q", calls, response.Body.String())
	}
}

func TestGraphTopologyReportsBackendFailure(t *testing.T) {
	t.Parallel()
	handler := graphHandler(func() (string, error) {
		return "", errors.New("fixture IPC disconnected")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("backend failure status = %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "fixture IPC disconnected") {
		t.Errorf("backend failure diagnostic = %q", response.Body.String())
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("backend failure presented as successful JSON: %q", response.Header().Get("Content-Type"))
	}
}
