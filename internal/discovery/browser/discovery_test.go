package browser

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadHistoryCopyAggregatesDomainsAndRemovesPaths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	historyPath := filepath.Join(t.TempDir(), "History")
	database, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE urls (url TEXT, visit_count INTEGER, last_visit_time INTEGER)`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	rows := []struct {
		url    string
		visits int
	}{
		{"https://www.example.com/private/path?q=secret", 5},
		{"https://example.com/another", 3},
		{"http://example.org/page", 2},
		{"file:///C:/secret.txt", 100},
		{"http://localhost:8080/admin", 100},
	}
	for _, row := range rows {
		if _, err := database.Exec(`INSERT INTO urls(url, visit_count, last_visit_time) VALUES(?, ?, ?)`, row.url, row.visits, chromeTime(now)); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	sites, err := readHistoryCopy(context.Background(), historyPath, chromeTime(now.AddDate(0, 0, -30)))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("expected two public domains, got %+v", sites)
	}
	byName := make(map[string]Site)
	for _, site := range sites {
		byName[site.Name] = site
	}
	if byName["example.com"].Visits != 8 || byName["example.com"].URL != "https://example.com/" {
		t.Fatalf("unexpected example.com aggregate: %+v", byName["example.com"])
	}
}

func TestReadFirefoxHistoryCopyUsesFirefoxTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	historyPath := filepath.Join(t.TempDir(), "places.sqlite")
	database, err := sql.Open("sqlite", historyPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE moz_places (url TEXT, visit_count INTEGER, last_visit_date INTEGER)`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO moz_places(url, visit_count, last_visit_date) VALUES(?, ?, ?)`,
		"https://www.mozilla.org/private?q=secret", 7, now.UnixMicro()); err != nil {
		t.Fatalf("insert row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	sites, err := readFirefoxHistoryCopy(context.Background(), historyPath, now.AddDate(0, 0, -30).UnixMicro())
	if err != nil {
		t.Fatalf("read Firefox history: %v", err)
	}
	if len(sites) != 1 || sites[0].Name != "mozilla.org" || sites[0].URL != "https://mozilla.org/" || sites[0].Visits != 7 {
		t.Fatalf("unexpected Firefox aggregate: %+v", sites)
	}
}

func TestSaveConfigReplacesPreviousFileWithoutBackup(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "sites.json")
	if err := os.WriteFile(path, []byte("old config"), 0o600); err != nil {
		t.Fatalf("write previous config: %v", err)
	}
	err := SaveConfig(path, []Site{{Name: "example.com", URL: "https://example.com/"}}, "home-windows-browsers")
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one generated config, got %d files: %v", len(files), err)
	}
}
