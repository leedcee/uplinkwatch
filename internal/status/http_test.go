package status

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leedcee/uplinkwatch/internal/agent"
	"github.com/leedcee/uplinkwatch/internal/checker"
)

func TestHandlerExposesDashboardAPIAndMetrics(t *testing.T) {
	t.Parallel()

	store := NewStore("home-test")
	store.Record(agent.Event{
		ProbeID:   "home-test",
		EmittedAt: time.Unix(1_700_000_001, 0),
		Result: checker.Result{
			TargetName:            "example",
			URL:                   "https://example.com/",
			StartedAt:             time.Unix(1_700_000_000, 0),
			Duration:              100 * time.Millisecond,
			Reachable:             true,
			HTTPStatusCode:        http.StatusOK,
			ContentCheckPerformed: true,
			ContentMatch:          true,
			Stages: []checker.StageResult{
				{Stage: checker.StageDNS, Duration: 10 * time.Millisecond, Success: true},
				{Stage: checker.StageTCP, Duration: 20 * time.Millisecond, Success: true},
				{Stage: checker.StageTLS, Duration: 30 * time.Millisecond, Success: true},
				{Stage: checker.StageHTTP, Duration: 40 * time.Millisecond, Success: true},
			},
		},
	})

	server := httptest.NewServer(Handler(store))
	defer server.Close()

	dashboard := getBody(t, server.URL+"/")
	if !strings.Contains(dashboard, "example") || !strings.Contains(dashboard, "Доступен") {
		t.Fatalf("dashboard does not contain result: %s", dashboard)
	}

	api := getBody(t, server.URL+"/api/v1/results")
	if !strings.Contains(api, `"probe_id":"home-test"`) || !strings.Contains(api, `"reachable":true`) {
		t.Fatalf("unexpected API response: %s", api)
	}
	diagnosis := getBody(t, server.URL+"/api/v1/diagnosis")
	if !strings.Contains(diagnosis, `"code":"healthy"`) {
		t.Fatalf("unexpected diagnosis response: %s", diagnosis)
	}

	metrics := getBody(t, server.URL+"/metrics")
	for _, expected := range []string{
		`site_reachable{probe="home-test",site="example"} 1`,
		`site_dns_duration_seconds{probe="home-test",site="example"} 0.01`,
		`site_http_status_code{probe="home-test",site="example"} 200`,
		`site_content_match{probe="home-test",site="example"} 1`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics do not contain %q:\n%s", expected, metrics)
		}
	}
}

func TestDiagnosisDistinguishesLocalFailurePatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []checker.Result
		code    string
	}{
		{name: "waiting", code: "waiting"},
		{name: "healthy", results: []checker.Result{{Reachable: true}, {Reachable: true}}, code: "healthy"},
		{name: "partial", results: []checker.Result{{Reachable: true}, failedAt(checker.StageHTTP)}, code: "partial_failure"},
		{name: "dns", results: []checker.Result{failedAt(checker.StageDNS), failedAt(checker.StageDNS)}, code: "dns_failure"},
		{name: "tcp", results: []checker.Result{failedAt(checker.StageTCP), failedAt(checker.StageTCP)}, code: "connection_failure"},
		{name: "mixed", results: []checker.Result{failedAt(checker.StageDNS), failedAt(checker.StageTCP)}, code: "multiple_failures"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := diagnose(Snapshot{Results: test.results})
			if got.Code != test.code {
				t.Fatalf("diagnose() code = %q, want %q", got.Code, test.code)
			}
		})
	}
}

func failedAt(stage checker.Stage) checker.Result {
	return checker.Result{Failure: &checker.Failure{Stage: stage, Reason: checker.ReasonHTTPError}}
}

func TestStoreCountsFailures(t *testing.T) {
	t.Parallel()

	store := NewStore("home-test")
	event := agent.Event{
		ProbeID: "home-test",
		Result: checker.Result{
			TargetName: "failed-site",
			Failure: &checker.Failure{
				Stage:  checker.StageTCP,
				Reason: checker.ReasonTCPTimeout,
			},
		},
	}
	store.Record(event)
	store.Record(event)
	successAt := time.Unix(1_700_000_100, 0)
	store.Record(agent.Event{
		ProbeID:   "home-test",
		EmittedAt: successAt,
		Result: checker.Result{
			TargetName: "failed-site",
			Reachable:  true,
		},
	})

	stats := store.Snapshot().Statistics["failed-site"]
	if stats.Checks != 3 || stats.Successful != 1 || !stats.LastStateChange.Equal(successAt) {
		t.Fatalf("unexpected runtime statistics: %+v", stats)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	Handler(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected response code %d", recorder.Code)
	}
	expected := `check_failures_total{probe="home-test",site="failed-site",stage="tcp",reason="tcp_timeout"} 2`
	if !strings.Contains(recorder.Body.String(), expected) {
		t.Fatalf("missing failure counter:\n%s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `site_availability_ratio{probe="home-test",site="failed-site"} 0.3333333333333333`) {
		t.Fatalf("missing availability ratio:\n%s", recorder.Body.String())
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	Handler(NewStore("test")).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok\n" {
		t.Fatalf("unexpected health response: %d %q", recorder.Code, recorder.Body.String())
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}
