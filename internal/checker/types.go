// Package checker performs staged network reachability checks.
package checker

import "time"

// Stage identifies a step in a reachability check.
type Stage string

const (
	StageDNS     Stage = "dns"
	StageTCP     Stage = "tcp"
	StageTLS     Stage = "tls"
	StageHTTP    Stage = "http"
	StageContent Stage = "content"
)

// FailureReason is a stable, machine-readable failure classification.
type FailureReason string

const (
	ReasonInvalidTarget       FailureReason = "invalid_target"
	ReasonDNSTimeout          FailureReason = "dns_timeout"
	ReasonDNSError            FailureReason = "dns_error"
	ReasonTCPTimeout          FailureReason = "tcp_timeout"
	ReasonTCPRefused          FailureReason = "tcp_refused"
	ReasonTCPError            FailureReason = "tcp_error"
	ReasonTLSTimeout          FailureReason = "tls_timeout"
	ReasonTLSCertificateError FailureReason = "tls_certificate_error"
	ReasonTLSError            FailureReason = "tls_error"
	ReasonHTTPTimeout         FailureReason = "http_timeout"
	ReasonHTTPError           FailureReason = "http_error"
	ReasonUnexpectedStatus    FailureReason = "unexpected_status"
	ReasonContentMismatch     FailureReason = "content_mismatch"
)

// Timeouts controls the maximum duration of each check stage and the whole
// check. Zero values are replaced with safe defaults.
type Timeouts struct {
	Overall time.Duration `json:"overall"`
	DNS     time.Duration `json:"dns"`
	TCP     time.Duration `json:"tcp"`
	TLS     time.Duration `json:"tls"`
	HTTP    time.Duration `json:"http"`
}

// Target describes one HTTP or HTTPS endpoint and its success criteria.
type Target struct {
	Name             string            `json:"name"`
	URL              string            `json:"url"`
	ExpectedStatuses []int             `json:"expected_statuses,omitempty"`
	ExpectedContent  string            `json:"expected_content,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	MaxResponseBytes int64             `json:"max_response_bytes,omitempty"`
	Timeouts         Timeouts          `json:"timeouts"`
}

// StageResult records the outcome and latency of one attempted stage.
type StageResult struct {
	Stage    Stage         `json:"stage"`
	Duration time.Duration `json:"duration"`
	Success  bool          `json:"success"`
	Skipped  bool          `json:"skipped,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// Failure describes the first stage that prevented the target from meeting
// its configured success criteria.
type Failure struct {
	Stage   Stage         `json:"stage"`
	Reason  FailureReason `json:"reason"`
	Message string        `json:"message"`
}

// Result is the complete outcome of a single target check.
type Result struct {
	TargetName            string        `json:"target_name"`
	URL                   string        `json:"url"`
	StartedAt             time.Time     `json:"started_at"`
	Duration              time.Duration `json:"duration"`
	Reachable             bool          `json:"reachable"`
	ResolvedIPs           []string      `json:"resolved_ips,omitempty"`
	RemoteAddress         string        `json:"remote_address,omitempty"`
	HTTPStatusCode        int           `json:"http_status_code,omitempty"`
	ContentCheckPerformed bool          `json:"content_check_performed"`
	ContentMatch          bool          `json:"content_match"`
	Stages                []StageResult `json:"stages"`
	Failure               *Failure      `json:"failure,omitempty"`
}
