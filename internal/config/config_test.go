package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndConvertsTargets(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
  "probe_id": "test-probe",
  "targets": [{
    "name": "example",
    "url": "https://example.com/",
    "expected_statuses": [200],
    "timeouts": {"http": "2s"}
  }]
}`)

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Interval.Duration != time.Minute {
		t.Fatalf("expected default interval, got %s", loaded.Interval.Duration)
	}
	if loaded.Concurrency != 4 {
		t.Fatalf("expected default concurrency, got %d", loaded.Concurrency)
	}
	if loaded.Listen != "127.0.0.1:9876" {
		t.Fatalf("expected default listen address, got %q", loaded.Listen)
	}
	targets := loaded.CheckerTargets()
	if len(targets) != 1 || targets[0].Timeouts.HTTP != 2*time.Second {
		t.Fatalf("unexpected checker targets: %+v", targets)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
  "unexpected": true,
  "targets": [{"name": "example", "url": "https://example.com"}]
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadRejectsDuplicateTargetNames(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
  "targets": [
    {"name": "example", "url": "https://example.com"},
    {"name": "example", "url": "https://example.org"}
  ]
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate name error, got %v", err)
	}
}

func TestLoadRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
  "targets": [{"name": "example", "url": "ftp://example.com"}]
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected invalid target error, got %v", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
