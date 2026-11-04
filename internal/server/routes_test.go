package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-engine/internal/fleet"
	"ai-engine/internal/providers"
	"ai-engine/internal/telemetry"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		fleet:       fleet.NewManager(),
		broadcaster: telemetry.NewBroadcaster(),
	}
	providers.Register(providers.NewMock(s.fleet))
	return s
}

func TestHealthHandler(t *testing.T) {
	s := newTestServer(t)
	server := httptest.NewServer(http.HandlerFunc(s.HealthHandler))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("error making request to server. Err: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK; got %v", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading response body. Err: %v", err)
	}
	if expected := `{"status":"ok"}`; expected != strings.TrimSpace(string(body)) {
		t.Errorf("expected response body to be %v; got %v", expected, string(body))
	}
}

func TestDrainNodeHandler(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/nodes/{id}/drain", s.DrainNodeHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/nodes/node-2/drain", "", nil)
	if err != nil {
		t.Fatalf("drain request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200; got %v", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Draining") {
		t.Errorf("expected card to show Draining status")
	}

	resp, err = http.Post(server.URL+"/api/nodes/nope/drain", "application/json", nil)
	if err != nil {
		t.Fatalf("drain request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown node; got %v", resp.Status)
	}
}
