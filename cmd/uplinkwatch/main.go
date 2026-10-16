package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/leedcee/uplinkwatch/internal/agent"
	"github.com/leedcee/uplinkwatch/internal/checker"
	"github.com/leedcee/uplinkwatch/internal/config"
	browserdiscovery "github.com/leedcee/uplinkwatch/internal/discovery/browser"
	monitorstatus "github.com/leedcee/uplinkwatch/internal/status"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uplinkwatch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version information and exit")
	configPath := flags.String("config", "data/sites.json", "path to the JSON configuration file")
	listenAddress := flags.String("listen-address", "", "override the HTTP listen address from the configuration")
	once := flags.Bool("once", false, "check all targets once and exit")
	openDashboard := flags.Bool("open-browser", false, "open the local dashboard in the default browser")
	discoverBrowsers := flags.Bool("discover-browsers", false, "build an editable config from all supported browser histories and exit")
	discoveryOutput := flags.String("discovery-output", "data/sites.json", "path for the generated browser config")
	discoveryLimit := flags.Int("discovery-limit", 30, "maximum number of frequent browser sites to include")
	discoveryDays := flags.Int("discovery-days", 180, "how many recent days of browser history to consider")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "UplinkWatch %s (commit %s, built %s)\n", version, commit, buildDate)
		return 0
	}
	if *discoverBrowsers {
		return runBrowserDiscovery(*discoveryOutput, *discoveryLimit, *discoveryDays, stdout, stderr)
	}

	settings, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	if *listenAddress != "" {
		if _, err := net.ResolveTCPAddr("tcp", *listenAddress); err != nil {
			fmt.Fprintf(stderr, "invalid listen address %q: %v\n", *listenAddress, err)
			return 2
		}
		settings.Listen = *listenAddress
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	var outputMu sync.Mutex
	statusStore := monitorstatus.NewStore(settings.ProbeID)
	runner := agent.Runner{
		ProbeID:     settings.ProbeID,
		Concurrency: settings.Concurrency,
		Checker:     checker.New(checker.Options{}),
		OnResult: func(event agent.Event) error {
			statusStore.Record(event)
			outputMu.Lock()
			defer outputMu.Unlock()
			return encoder.Encode(event)
		},
		OnOutputError: func(err error) {
			fmt.Fprintf(stderr, "write result: %v\n", err)
		},
	}
	targets := settings.CheckerTargets()

	if *once {
		results := runner.CheckAll(ctx, targets)
		for _, result := range results {
			if !result.Reachable {
				return 1
			}
		}
		return 0
	}

	listener, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		fmt.Fprintf(stderr, "start local HTTP server on %s: %v\n", settings.Listen, err)
		return 2
	}
	server := &http.Server{
		Handler:           monitorstatus.Handler(statusStore),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	dashboardURL := "http://" + settings.Listen
	fmt.Fprintf(stderr, "probe %q started: %d target(s), interval %s\n", settings.ProbeID, len(targets), settings.Interval.Duration)
	fmt.Fprintf(stderr, "local dashboard: %s\n", dashboardURL)
	if *openDashboard {
		if err := openBrowser(dashboardURL); err != nil {
			fmt.Fprintf(stderr, "open dashboard in browser: %v\n", err)
		}
	}
	runner.CheckAll(ctx, targets)
	ticker := time.NewTicker(settings.Interval.Duration)
	defer ticker.Stop()
	defer shutdownServer(server, stderr)

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(stderr, "agent stopped")
			return 0
		case err, ok := <-serverErrors:
			if ok {
				fmt.Fprintf(stderr, "local HTTP server stopped: %v\n", err)
				return 1
			}
			serverErrors = nil
		case <-ticker.C:
			runner.CheckAll(ctx, targets)
		}
	}
}

func runBrowserDiscovery(outputPath string, limit, days int, stdout, stderr io.Writer) int {
	if limit < 1 || limit > 100 {
		fmt.Fprintln(stderr, "discovery limit must be between 1 and 100")
		return 2
	}
	if days < 1 || days > 3650 {
		fmt.Fprintln(stderr, "discovery days must be between 1 and 3650")
		return 2
	}
	options := browserdiscovery.Options{Limit: limit, Days: days}
	sites, err := browserdiscovery.Discover(context.Background(), options)
	if err != nil {
		fmt.Fprintf(stderr, "browser discovery error: %v\n", err)
		return 1
	}
	if err := browserdiscovery.SaveConfig(outputPath, sites, "home-windows-browsers"); err != nil {
		fmt.Fprintf(stderr, "save browser config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Updated %s with %d frequent browser site(s).\n", outputPath, len(sites))
	fmt.Fprintln(stdout, "Only domain names and aggregate visit counts were used; page paths, titles, and search queries were not saved.")
	return 0
}

func shutdownServer(server *http.Server, stderr io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		fmt.Fprintf(stderr, "stop local HTTP server: %v\n", err)
	}
}
