// Package config loads and validates the local agent configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/leedcee/uplinkwatch/internal/checker"
)

const (
	defaultInterval    = time.Minute
	defaultConcurrency = 4
	defaultListen      = "127.0.0.1:9876"
)

// Duration accepts human-readable JSON strings such as "500ms" or "2m".
type Duration struct {
	time.Duration
}

// UnmarshalJSON parses a duration string. An empty string means that the
// checker default should be used.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string such as \"5s\"")
	}
	if value == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value, err)
	}
	if parsed <= 0 {
		return fmt.Errorf("duration %q must be positive", value)
	}
	d.Duration = parsed
	return nil
}

// File is the on-disk representation of the agent configuration.
type File struct {
	ProbeID     string   `json:"probe_id"`
	Interval    Duration `json:"interval"`
	Concurrency int      `json:"concurrency"`
	Listen      string   `json:"listen_address"`
	Targets     []Target `json:"targets"`
}

// Target is the JSON representation of a website check.
type Target struct {
	Name             string            `json:"name"`
	URL              string            `json:"url"`
	ExpectedStatuses []int             `json:"expected_statuses,omitempty"`
	ExpectedContent  string            `json:"expected_content,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	MaxResponseBytes int64             `json:"max_response_bytes,omitempty"`
	Timeouts         Timeouts          `json:"timeouts,omitempty"`
}

// Timeouts is the human-readable JSON representation of checker timeouts.
type Timeouts struct {
	Overall Duration `json:"overall,omitempty"`
	DNS     Duration `json:"dns,omitempty"`
	TCP     Duration `json:"tcp,omitempty"`
	TLS     Duration `json:"tls,omitempty"`
	HTTP    Duration `json:"http,omitempty"`
}

// Load reads a JSON configuration file, rejects unknown fields, applies safe
// defaults, and validates target identity.
func Load(path string) (File, error) {
	file, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var config File
	if err := decoder.Decode(&config); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return File{}, errors.New("decode config: multiple JSON documents are not allowed")
		}
		return File{}, fmt.Errorf("decode config: trailing data: %w", err)
	}

	if err := applyDefaultsAndValidate(&config); err != nil {
		return File{}, err
	}
	return config, nil
}

func applyDefaultsAndValidate(config *File) error {
	config.ProbeID = strings.TrimSpace(config.ProbeID)
	if config.ProbeID == "" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			config.ProbeID = "local"
		} else {
			config.ProbeID = hostname
		}
	}
	if config.Interval.Duration == 0 {
		config.Interval.Duration = defaultInterval
	}
	if config.Concurrency == 0 {
		config.Concurrency = defaultConcurrency
	}
	config.Listen = strings.TrimSpace(config.Listen)
	if config.Listen == "" {
		config.Listen = defaultListen
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return fmt.Errorf("listen_address must have host:port format: %w", err)
	}
	if config.Concurrency < 1 || config.Concurrency > 100 {
		return errors.New("concurrency must be between 1 and 100")
	}
	if len(config.Targets) == 0 {
		return errors.New("config must contain at least one target")
	}

	names := make(map[string]struct{}, len(config.Targets))
	for index := range config.Targets {
		target := &config.Targets[index]
		target.Name = strings.TrimSpace(target.Name)
		target.URL = strings.TrimSpace(target.URL)
		if target.Name == "" {
			return fmt.Errorf("target %d: name is required", index+1)
		}
		if target.URL == "" {
			return fmt.Errorf("target %q: URL is required", target.Name)
		}
		if _, exists := names[target.Name]; exists {
			return fmt.Errorf("target name %q is duplicated", target.Name)
		}
		names[target.Name] = struct{}{}
		if err := checker.ValidateTarget(target.checkerTarget()); err != nil {
			return fmt.Errorf("target %q: %w", target.Name, err)
		}
	}
	return nil
}

// CheckerTargets converts configuration targets to checker inputs.
func (f File) CheckerTargets() []checker.Target {
	targets := make([]checker.Target, 0, len(f.Targets))
	for _, target := range f.Targets {
		targets = append(targets, target.checkerTarget())
	}
	return targets
}

func (target Target) checkerTarget() checker.Target {
	return checker.Target{
		Name:             target.Name,
		URL:              target.URL,
		ExpectedStatuses: append([]int(nil), target.ExpectedStatuses...),
		ExpectedContent:  target.ExpectedContent,
		Headers:          cloneMap(target.Headers),
		MaxResponseBytes: target.MaxResponseBytes,
		Timeouts: checker.Timeouts{
			Overall: target.Timeouts.Overall.Duration,
			DNS:     target.Timeouts.DNS.Duration,
			TCP:     target.Timeouts.TCP.Duration,
			TLS:     target.Timeouts.TLS.Duration,
			HTTP:    target.Timeouts.HTTP.Duration,
		},
	}
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
