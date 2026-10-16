// Package browser discovers frequently visited sites from local browser
// profiles. It retains domain-level aggregates only: paths, page titles, and
// search queries are never returned or written to the generated config.
package browser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const chromeEpochOffsetSeconds int64 = 11_644_473_600

// Options controls local history discovery.
type Options struct {
	Limit int
	Days  int
	Now   time.Time
}

// Site is a domain-level aggregate suitable for monitoring.
type Site struct {
	Name      string
	URL       string
	Visits    int64
	LastVisit time.Time
}

type historySource struct {
	browser string
	path    string
	firefox bool
}

// Discover returns frequent domains aggregated across Arc, Chrome, Edge,
// Brave, and Firefox profiles belonging to the current Windows user.
func Discover(ctx context.Context, options Options) ([]Site, error) {
	options = defaultOptions(options)
	sources, err := findAllHistorySources()
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("no supported browser history was found")
	}
	return discoverSources(ctx, options, sources)
}

func defaultOptions(options Options) Options {
	if options.Limit <= 0 {
		options.Limit = 20
	}
	if options.Days <= 0 {
		options.Days = 180
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	return options
}

func discoverSources(ctx context.Context, options Options, sources []historySource) ([]Site, error) {
	threshold := chromeTime(options.Now.AddDate(0, 0, -options.Days))
	firefoxThreshold := options.Now.AddDate(0, 0, -options.Days).UnixMicro()
	aggregates := make(map[string]Site)
	var profileErrors []error
	for _, source := range sources {
		var sites []Site
		var err error
		if source.firefox {
			sites, err = readFirefoxHistoryCopy(ctx, source.path, firefoxThreshold)
		} else {
			sites, err = readHistoryCopy(ctx, source.path, threshold)
		}
		if err != nil {
			profileErrors = append(profileErrors, fmt.Errorf("%s/%s: %w", source.browser, filepath.Base(filepath.Dir(source.path)), err))
			continue
		}
		for _, site := range sites {
			current := aggregates[site.Name]
			current.Name = site.Name
			current.Visits += site.Visits
			if current.URL == "" || strings.HasPrefix(site.URL, "https://") {
				current.URL = site.URL
			}
			if site.LastVisit.After(current.LastVisit) {
				current.LastVisit = site.LastVisit
			}
			aggregates[site.Name] = current
		}
	}
	if len(aggregates) == 0 {
		if len(profileErrors) > 0 {
			return nil, fmt.Errorf("could not read browser history; close the browsers and try again: %w", errors.Join(profileErrors...))
		}
		return nil, fmt.Errorf("browser history contains no public websites visited in the last %d days", options.Days)
	}

	sites := make([]Site, 0, len(aggregates))
	for _, site := range aggregates {
		sites = append(sites, site)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].Visits == sites[j].Visits {
			return sites[i].LastVisit.After(sites[j].LastVisit)
		}
		return sites[i].Visits > sites[j].Visits
	})
	if len(sites) > options.Limit {
		sites = sites[:options.Limit]
	}
	return sites, nil
}

func findAllHistorySources() ([]historySource, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	appData := os.Getenv("APPDATA")
	if localAppData == "" || appData == "" {
		return nil, errors.New("LOCALAPPDATA or APPDATA is not set")
	}

	chromiumRoots := []struct {
		browser string
		path    string
	}{
		{"Arc", filepath.Join(localAppData, "Packages", "TheBrowserCompany.Arc_ttt1ap7aakyb4", "LocalCache", "Local", "Arc", "User Data")},
		{"Chrome", filepath.Join(localAppData, "Google", "Chrome", "User Data")},
		{"Edge", filepath.Join(localAppData, "Microsoft", "Edge", "User Data")},
		{"Brave", filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data")},
	}

	var sources []historySource
	for _, root := range chromiumRoots {
		profiles, err := chromiumHistorySources(root.browser, root.path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, profiles...)
	}

	firefoxRoot := filepath.Join(appData, "Mozilla", "Firefox", "Profiles")
	entries, err := os.ReadDir(firefoxRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read Firefox profiles: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		places := filepath.Join(firefoxRoot, entry.Name(), "places.sqlite")
		if info, err := os.Stat(places); err == nil && !info.IsDir() {
			sources = append(sources, historySource{browser: "Firefox", path: places, firefox: true})
		}
	}
	return sources, nil
}

func chromiumHistorySources(browser, userData string) ([]historySource, error) {
	entries, err := os.ReadDir(userData)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s profiles: %w", browser, err)
	}
	var sources []historySource
	for _, entry := range entries {
		if !entry.IsDir() || (entry.Name() != "Default" && !strings.HasPrefix(entry.Name(), "Profile ")) {
			continue
		}
		history := filepath.Join(userData, entry.Name(), "History")
		if info, err := os.Stat(history); err == nil && !info.IsDir() {
			sources = append(sources, historySource{browser: browser, path: history})
		}
	}
	return sources, nil
}

func readHistoryCopy(ctx context.Context, historyFile string, threshold int64) ([]Site, error) {
	temporaryDirectory, err := os.MkdirTemp("", "uplinkwatch-browser-history-")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)

	temporaryHistory := filepath.Join(temporaryDirectory, "History")
	if err := copyFile(historyFile, temporaryHistory); err != nil {
		return nil, fmt.Errorf("copy history database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(historyFile + suffix); err == nil {
			if err := copyFile(historyFile+suffix, temporaryHistory+suffix); err != nil {
				return nil, fmt.Errorf("copy history%s: %w", suffix, err)
			}
		}
	}

	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(temporaryHistory)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open copied history: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	rows, err := database.QueryContext(ctx, `
SELECT url, visit_count, last_visit_time
FROM urls
WHERE visit_count > 0 AND last_visit_time >= ?`, threshold)
	if err != nil {
		return nil, fmt.Errorf("query Chromium browser history: %w", err)
	}
	defer rows.Close()

	aggregates := make(map[string]Site)
	for rows.Next() {
		var rawURL string
		var visits int64
		var lastVisit int64
		if err := rows.Scan(&rawURL, &visits, &lastVisit); err != nil {
			return nil, fmt.Errorf("read Chromium browser history row: %w", err)
		}
		name, baseURL, ok := domainTarget(rawURL)
		if !ok {
			continue
		}
		current := aggregates[name]
		current.Name = name
		current.Visits += visits
		if current.URL == "" || strings.HasPrefix(baseURL, "https://") {
			current.URL = baseURL
		}
		visitedAt := fromChromeTime(lastVisit)
		if visitedAt.After(current.LastVisit) {
			current.LastVisit = visitedAt
		}
		aggregates[name] = current
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Chromium browser history: %w", err)
	}

	sites := make([]Site, 0, len(aggregates))
	for _, site := range aggregates {
		sites = append(sites, site)
	}
	return sites, nil
}

func readFirefoxHistoryCopy(ctx context.Context, historyFile string, threshold int64) ([]Site, error) {
	temporaryDirectory, err := os.MkdirTemp("", "network-monitor-firefox-history-")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)

	temporaryHistory := filepath.Join(temporaryDirectory, "places.sqlite")
	if err := copyFile(historyFile, temporaryHistory); err != nil {
		return nil, fmt.Errorf("copy history database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(historyFile + suffix); err == nil {
			if err := copyFile(historyFile+suffix, temporaryHistory+suffix); err != nil {
				return nil, fmt.Errorf("copy history%s: %w", suffix, err)
			}
		}
	}

	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(temporaryHistory)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open copied history: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	rows, err := database.QueryContext(ctx, `
SELECT url, visit_count, last_visit_date
FROM moz_places
WHERE visit_count > 0 AND last_visit_date IS NOT NULL AND last_visit_date >= ?`, threshold)
	if err != nil {
		return nil, fmt.Errorf("query Firefox history: %w", err)
	}
	defer rows.Close()

	aggregates := make(map[string]Site)
	for rows.Next() {
		var rawURL string
		var visits int64
		var lastVisit int64
		if err := rows.Scan(&rawURL, &visits, &lastVisit); err != nil {
			return nil, fmt.Errorf("read Firefox history row: %w", err)
		}
		name, baseURL, ok := domainTarget(rawURL)
		if !ok {
			continue
		}
		current := aggregates[name]
		current.Name = name
		current.Visits += visits
		if current.URL == "" || strings.HasPrefix(baseURL, "https://") {
			current.URL = baseURL
		}
		visitedAt := time.UnixMicro(lastVisit)
		if visitedAt.After(current.LastVisit) {
			current.LastVisit = visitedAt
		}
		aggregates[name] = current
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Firefox history: %w", err)
	}

	sites := make([]Site, 0, len(aggregates))
	for _, site := range aggregates {
		sites = append(sites, site)
	}
	return sites, nil
}

func domainTarget(rawURL string) (name, baseURL string, ok bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", "", false
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	hostname = strings.TrimPrefix(hostname, "www.")
	if hostname == "" || hostname == "localhost" || strings.HasSuffix(hostname, ".local") {
		return "", "", false
	}
	if address := net.ParseIP(hostname); address != nil &&
		(address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsUnspecified()) {
		return "", "", false
	}

	host := hostname
	if parsed.Port() != "" {
		host = net.JoinHostPort(hostname, parsed.Port())
	}
	return hostname, parsed.Scheme + "://" + host + "/", true
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func chromeTime(value time.Time) int64 {
	return (value.Unix() + chromeEpochOffsetSeconds) * 1_000_000
}

func fromChromeTime(value int64) time.Time {
	seconds := value/1_000_000 - chromeEpochOffsetSeconds
	nanoseconds := value % 1_000_000 * 1_000
	return time.Unix(seconds, nanoseconds)
}

// SaveConfig atomically replaces the single generated browser configuration.
func SaveConfig(path string, sites []Site, probeID string) error {
	if len(sites) == 0 {
		return errors.New("cannot save an empty site list")
	}

	type target struct {
		Name             string `json:"name"`
		URL              string `json:"url"`
		ExpectedStatuses []int  `json:"expected_statuses"`
	}
	type draft struct {
		ProbeID     string   `json:"probe_id"`
		Interval    string   `json:"interval"`
		Concurrency int      `json:"concurrency"`
		Listen      string   `json:"listen_address"`
		Targets     []target `json:"targets"`
	}

	configuration := draft{
		ProbeID:     probeID,
		Interval:    "1m",
		Concurrency: 4,
		Listen:      "127.0.0.1:9876",
		Targets:     make([]target, 0, len(sites)),
	}
	for _, site := range sites {
		configuration.Targets = append(configuration.Targets, target{
			Name:             site.Name,
			URL:              site.URL,
			ExpectedStatuses: []int{200, 204, 301, 302, 303, 307, 308, 401, 403},
		})
	}

	contents, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return fmt.Errorf("encode browser config: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".sites-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary config: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install browser config: %w", err)
	}
	return nil
}
