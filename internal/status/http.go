package status

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leedcee/uplinkwatch/internal/checker"
)

// Handler returns the local dashboard, JSON API, health check, and metrics
// endpoints backed by store.
func Handler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		setSecurityHeaders(writer)
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTemplate.Execute(writer, dashboardView(store.Snapshot())); err != nil {
			http.Error(writer, "render dashboard", http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("GET /api/v1/results", func(writer http.ResponseWriter, _ *http.Request) {
		setSecurityHeaders(writer)
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(store.Snapshot())
	})
	mux.HandleFunc("GET /api/v1/diagnosis", func(writer http.ResponseWriter, _ *http.Request) {
		setSecurityHeaders(writer)
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(diagnose(store.Snapshot()))
	})
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		setSecurityHeaders(writer)
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(writer, "ok\n")
	})
	mux.HandleFunc("GET /metrics", func(writer http.ResponseWriter, _ *http.Request) {
		setSecurityHeaders(writer)
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writeMetrics(writer, store.Snapshot())
	})
	return mux
}

type pageData struct {
	ProbeID   string
	UpdatedAt string
	HasData   bool
	Online    int
	Total     int
	Rows      []pageRow
	Diagnosis Diagnosis
}

type pageRow struct {
	Name          string
	URL           string
	State         string
	StateClass    string
	Status        string
	Duration      string
	DNSDuration   string
	TCPDuration   string
	TLSDuration   string
	HTTPDuration  string
	Failure       string
	Checks        uint64
	LastChange    string
	LastCheckedAt string
}

func dashboardView(snapshot Snapshot) pageData {
	data := pageData{ProbeID: snapshot.ProbeID, HasData: len(snapshot.Results) > 0, Total: len(snapshot.Results), Diagnosis: diagnose(snapshot)}
	if !snapshot.UpdatedAt.IsZero() {
		data.UpdatedAt = snapshot.UpdatedAt.Format(time.RFC3339)
	}
	for _, result := range snapshot.Results {
		stats := snapshot.Statistics[result.TargetName]
		row := pageRow{
			Name:          result.TargetName,
			URL:           result.URL,
			State:         "Недоступен",
			StateClass:    "down",
			Status:        "—",
			Duration:      result.Duration.Round(time.Millisecond).String(),
			DNSDuration:   stageDuration(result, checker.StageDNS),
			TCPDuration:   stageDuration(result, checker.StageTCP),
			TLSDuration:   stageDuration(result, checker.StageTLS),
			HTTPDuration:  stageDuration(result, checker.StageHTTP),
			Checks:        stats.Checks,
			LastChange:    formatClock(stats.LastStateChange),
			LastCheckedAt: result.StartedAt.Format("15:04:05"),
		}
		if result.Reachable {
			data.Online++
			row.State = "Доступен"
			row.StateClass = "up"
		}
		if result.HTTPStatusCode != 0 {
			row.Status = strconv.Itoa(result.HTTPStatusCode)
		}
		if result.Failure != nil {
			row.Failure = fmt.Sprintf("%s: %s", result.Failure.Stage, result.Failure.Reason)
		}
		data.Rows = append(data.Rows, row)
	}
	return data
}

func formatClock(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Format("15:04:05")
}

func stageDuration(result checker.Result, wanted checker.Stage) string {
	for _, stage := range result.Stages {
		if stage.Stage != wanted {
			continue
		}
		if stage.Skipped {
			return "—"
		}
		return stage.Duration.Round(time.Millisecond).String()
	}
	return "—"
}

func writeMetrics(writer io.Writer, snapshot Snapshot) {
	writeMetricHelp(writer, "site_reachable", "Whether the latest site check met all configured criteria.", "gauge")
	writeMetricHelp(writer, "site_dns_duration_seconds", "Duration of the latest DNS stage in seconds.", "gauge")
	writeMetricHelp(writer, "site_tcp_duration_seconds", "Duration of the latest TCP stage in seconds.", "gauge")
	writeMetricHelp(writer, "site_tls_duration_seconds", "Duration of the latest TLS stage in seconds.", "gauge")
	writeMetricHelp(writer, "site_http_duration_seconds", "Duration of the latest HTTP stage in seconds.", "gauge")
	writeMetricHelp(writer, "site_http_status_code", "HTTP status code from the latest check, or zero if none was received.", "gauge")
	writeMetricHelp(writer, "site_content_match", "Whether the latest configured content check matched.", "gauge")
	writeMetricHelp(writer, "site_last_check_timestamp_seconds", "Unix timestamp of the latest site check.", "gauge")
	writeMetricHelp(writer, "site_checks_total", "Number of site checks since this agent process started.", "counter")
	writeMetricHelp(writer, "site_successful_checks_total", "Number of successful site checks since this agent process started.", "counter")
	writeMetricHelp(writer, "site_availability_ratio", "Ratio of successful checks since this agent process started.", "gauge")

	for _, result := range snapshot.Results {
		labels := metricLabels(snapshot.ProbeID, result.TargetName)
		writeGauge(writer, "site_reachable", labels, boolFloat(result.Reachable))
		writeGauge(writer, "site_dns_duration_seconds", labels, stageSeconds(result, checker.StageDNS))
		writeGauge(writer, "site_tcp_duration_seconds", labels, stageSeconds(result, checker.StageTCP))
		writeGauge(writer, "site_tls_duration_seconds", labels, stageSeconds(result, checker.StageTLS))
		writeGauge(writer, "site_http_duration_seconds", labels, stageSeconds(result, checker.StageHTTP))
		writeGauge(writer, "site_http_status_code", labels, float64(result.HTTPStatusCode))
		if result.ContentCheckPerformed {
			writeGauge(writer, "site_content_match", labels, boolFloat(result.ContentMatch))
		}
		writeGauge(writer, "site_last_check_timestamp_seconds", labels, float64(result.StartedAt.Unix()))
		stats := snapshot.Statistics[result.TargetName]
		writeGauge(writer, "site_checks_total", labels, float64(stats.Checks))
		writeGauge(writer, "site_successful_checks_total", labels, float64(stats.Successful))
		if stats.Checks > 0 {
			writeGauge(writer, "site_availability_ratio", labels, float64(stats.Successful)/float64(stats.Checks))
		}
	}

	writeMetricHelp(writer, "check_failures_total", "Number of failed checks since this agent process started.", "counter")
	series := make([]FailureSeries, 0, len(snapshot.FailureCount))
	for item := range snapshot.FailureCount {
		series = append(series, item)
	}
	sort.Slice(series, func(i, j int) bool {
		left := series[i]
		right := series[j]
		return left.Probe+left.Target+string(left.Stage)+string(left.Reason) < right.Probe+right.Target+string(right.Stage)+string(right.Reason)
	})
	for _, item := range series {
		labels := fmt.Sprintf("probe=%q,site=%q,stage=%q,reason=%q",
			escapeLabel(item.Probe), escapeLabel(item.Target), escapeLabel(string(item.Stage)), escapeLabel(string(item.Reason)))
		fmt.Fprintf(writer, "check_failures_total{%s} %d\n", labels, snapshot.FailureCount[item])
	}
}

func writeMetricHelp(writer io.Writer, name, help, metricType string) {
	fmt.Fprintf(writer, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func writeGauge(writer io.Writer, name, labels string, value float64) {
	fmt.Fprintf(writer, "%s{%s} %s\n", name, labels, strconv.FormatFloat(value, 'g', -1, 64))
}

func metricLabels(probe, target string) string {
	return fmt.Sprintf("probe=%q,site=%q", escapeLabel(probe), escapeLabel(target))
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
	return replacer.Replace(value)
}

func stageSeconds(result checker.Result, wanted checker.Stage) float64 {
	for _, stage := range result.Stages {
		if stage.Stage == wanted && !stage.Skipped {
			return stage.Duration.Seconds()
		}
	}
	return 0
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func setSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="refresh" content="10">
  <title>UplinkWatch</title>
  <style>
    :root {
      color-scheme: dark;
      font-family: "Cascadia Mono", "Cascadia Code", Consolas, "Courier New", monospace;
      background: #000;
      color: #fff;
    }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: #000; color: #fff; }
    main {
      position: relative;
      width: calc(100% - clamp(32px, 8vw, 120px));
      min-height: calc(100vh - clamp(32px, 8vw, 120px));
      margin: clamp(16px, 4vw, 60px) auto;
      padding: clamp(24px, 4vw, 58px);
      border: 2px solid #00e676;
      background: #020402;
      box-shadow: 0 0 0 1px #003d1c, inset 0 0 30px rgba(0, 230, 118, .045), 0 0 24px rgba(0, 230, 118, .10);
    }
    main::before {
      content: "UPLINKWATCH // LOCAL PROBE";
      position: absolute;
      top: -10px;
      left: 24px;
      padding: 0 10px;
      background: #000;
      color: #00e676;
      font-size: 12px;
      letter-spacing: .12em;
    }
    header {
      display: flex;
      justify-content: space-between;
      align-items: end;
      gap: 28px;
      margin-bottom: 28px;
      padding-bottom: 20px;
      border-bottom: 1px solid #08783d;
    }
    h1 {
      margin: 0 0 10px;
      color: #fff;
      font-size: clamp(22px, 3vw, 34px);
      font-weight: 600;
      letter-spacing: -.04em;
    }
    h1::before { content: "> "; color: #00e676; }
    p { margin: 0; color: #b8c2bc; font-size: 13px; line-height: 1.6; }
    .panel { overflow-x: auto; background: #000; }
    .diagnosis {
      margin: 0 0 24px;
      padding: 15px 18px;
      border: 1px solid #31503d;
      background: #050b07;
    }
    .diagnosis strong { display: block; margin-bottom: 6px; color: #fff; font-size: 14px; }
    .diagnosis strong::before { content: "[diagnosis] "; color: #00e676; }
    .diagnosis p { color: #c5cec8; }
    .diagnosis.warning { border-color: #d59b24; background: #171104; }
    .diagnosis.warning strong::before { color: #ffc857; }
    .diagnosis.critical { border-color: #ff405c; background: #1c0509; }
    .diagnosis.critical strong, .diagnosis.critical strong::before { color: #ff6b7f; }
    table { width: 100%; border-collapse: collapse; min-width: 1320px; }
    th, td {
      padding: 13px 12px;
      text-align: left;
      border-bottom: 1px solid #12301f;
      color: #fff;
      font-size: 13px;
      white-space: nowrap;
    }
    th {
      padding-top: 8px;
      padding-bottom: 14px;
      color: #9da7a1;
      font-size: 11px;
      font-weight: 500;
      text-transform: uppercase;
      letter-spacing: .09em;
    }
    tbody tr { transition: background-color .12s ease; }
    tbody tr:hover { background: #041a0d; }
    tbody tr.down td,
    tbody tr.down td strong,
    tbody tr.down a { color: #ff5f6d; }
    tbody tr.down:hover { background: #210307; }
    tbody tr.down a { text-decoration-color: #8f2734; }
    tbody tr.down a:hover { color: #ff9aa5; text-decoration-color: #ff5f6d; }
    tr:last-child td { border-bottom: 0; }
    td:first-child { min-width: 250px; }
    td strong { color: #fff; font-weight: 600; }
    a { color: #fff; text-decoration-color: #256a43; text-underline-offset: 3px; }
    a:hover { color: #9dffc6; text-decoration-color: #00e676; }
    .state { display: inline-flex; align-items: center; gap: 9px; color: #fff; font-weight: 600; }
    .state::before { content: ''; width: 8px; height: 8px; border-radius: 50%; }
    .state.up::before { background: #00e676; box-shadow: 0 0 10px rgba(0, 230, 118, .8); }
    .state.down::before { background: #ff405c; box-shadow: 0 0 10px rgba(255, 64, 92, .65); }
    .failure { color: #fff; }
    .empty { padding: 70px 24px; color: #fff; text-align: center; }
    .empty::before { content: "[waiting] "; color: #00e676; }
    code { color: #fff; font-family: inherit; }
    @media (max-width: 760px) {
      main { width: calc(100% - 24px); min-height: calc(100vh - 24px); margin: 12px auto; padding: 28px 18px; }
      main::before { left: 12px; }
      header { align-items: start; flex-direction: column; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div><h1>UplinkWatch</h1><p>Точка проверки: <code>{{.ProbeID}}</code></p></div>
      <p>{{if .UpdatedAt}}ONLINE {{.Online}}/{{.Total}} &nbsp;//&nbsp; Обновлено: {{.UpdatedAt}}{{else}}Ожидание первой проверки{{end}}</p>
    </header>
    <section class="diagnosis {{.Diagnosis.Level}}">
      <strong>{{.Diagnosis.Title}}</strong>
      <p>{{.Diagnosis.Details}}</p>
    </section>
    <section class="panel">
      {{if .HasData}}
      <table>
        <thead><tr><th>Сайт</th><th>Состояние</th><th>HTTP</th><th>Всего</th><th>DNS</th><th>TCP</th><th>TLS</th><th>HTTP</th><th>Проверок</th><th>Смена</th><th>Причина</th><th>Проверен</th></tr></thead>
        <tbody>
          {{range .Rows}}<tr class="{{.StateClass}}">
            <td><strong>{{.Name}}</strong><br><a href="{{.URL}}" rel="noreferrer">{{.URL}}</a></td>
            <td><span class="state {{.StateClass}}">{{.State}}</span></td>
            <td>{{.Status}}</td><td>{{.Duration}}</td><td>{{.DNSDuration}}</td><td>{{.TCPDuration}}</td><td>{{.TLSDuration}}</td><td>{{.HTTPDuration}}</td>
            <td>{{.Checks}}</td><td>{{.LastChange}}</td><td class="failure">{{if .Failure}}{{.Failure}}{{else}}—{{end}}</td><td>{{.LastCheckedAt}}</td>
          </tr>{{end}}
        </tbody>
      </table>
      {{else}}<div class="empty">Агент запущен. Ожидается первая проверка сайтов.</div>{{end}}
    </section>
  </main>
</body>
</html>`))
