package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/leedcee/uplinkwatch/internal/agent"
)

func TestRunOnceChecksConfiguredTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("healthy"))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	contents := fmt.Sprintf(`{
  "probe_id": "integration-test",
  "targets": [{
    "name": "local-server",
    "url": %q,
    "expected_statuses": [200],
    "expected_content": "healthy"
  }]
}`, server.URL)
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"-config", configPath, "-once"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected successful exit, got %d: %s", exitCode, stderr.String())
	}

	var event agent.Event
	if err := json.NewDecoder(&stdout).Decode(&event); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if event.ProbeID != "integration-test" || !event.Result.Reachable {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestRunHelpExitsSuccessfully(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"-help"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("expected successful help exit, got %d", exitCode)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("-config")) {
		t.Fatalf("expected flag help, got %q", stderr.String())
	}
}

func TestRunVersionExitsSuccessfully(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run([]string{"-version"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("expected successful version exit, got %d: %s", exitCode, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("UplinkWatch")) {
		t.Fatalf("expected product name in version output, got %q", stdout.String())
	}
}

func TestLocalDashboardURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"127.0.0.1:9876": "http://127.0.0.1:9876",
		"0.0.0.0:9876":   "http://127.0.0.1:9876",
		"[::]:9876":      "http://127.0.0.1:9876",
		"[::1]:9876":     "http://[::1]:9876",
		"localhost:9876": "http://localhost:9876",
	}

	for listenAddress, expected := range tests {
		listenAddress := listenAddress
		expected := expected
		t.Run(listenAddress, func(t *testing.T) {
			t.Parallel()
			if actual := localDashboardURL(listenAddress); actual != expected {
				t.Fatalf("expected %q, got %q", expected, actual)
			}
		})
	}
}
