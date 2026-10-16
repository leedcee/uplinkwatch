package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultOverallTimeout = 20 * time.Second
	defaultDNSTimeout     = 3 * time.Second
	defaultTCPTimeout     = 5 * time.Second
	defaultTLSTimeout     = 5 * time.Second
	defaultHTTPTimeout    = 10 * time.Second
	defaultMaxBodyBytes   = 1 << 20 // 1 MiB
)

// Resolver is the subset of net.Resolver used by Checker.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Options provides injectable dependencies and TLS settings.
type Options struct {
	Resolver  Resolver
	Dialer    *net.Dialer
	TLSConfig *tls.Config
	Now       func() time.Time
}

// Checker performs staged checks. It is safe for concurrent use.
type Checker struct {
	resolver  Resolver
	dialer    *net.Dialer
	tlsConfig *tls.Config
	now       func() time.Time
}

// New creates a checker with production-safe defaults for omitted options.
func New(options Options) *Checker {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &Checker{
		resolver:  resolver,
		dialer:    dialer,
		tlsConfig: cloneTLSConfig(options.TLSConfig),
		now:       now,
	}
}

// ValidateTarget checks static target settings without performing any network
// operations.
func ValidateTarget(target Target) error {
	_, _, _, err := validateTarget(target)
	return err
}

// Check runs DNS, TCP, optional TLS, and HTTP stages in order. The HTTP stage
// reuses the connection created by the TCP and TLS stages.
func (c *Checker) Check(ctx context.Context, target Target) (result Result) {
	startedAt := c.now()
	result = Result{
		TargetName: target.Name,
		URL:        target.URL,
		StartedAt:  startedAt,
		Stages:     make([]StageResult, 0, 5),
	}
	defer func() {
		result.Duration = c.now().Sub(startedAt)
	}()

	parsedURL, host, port, err := validateTarget(target)
	if err != nil {
		result.Failure = newFailure(StageHTTP, ReasonInvalidTarget, err)
		return result
	}

	timeouts := withDefaultTimeouts(target.Timeouts)
	overallCtx, cancelOverall := context.WithTimeout(ctx, timeouts.Overall)
	defer cancelOverall()

	ips, stage := c.resolve(overallCtx, host, timeouts.DNS)
	result.Stages = append(result.Stages, stage)
	if !stage.Success {
		result.Failure = failureFromStage(stage, classifyDNSFailure(stage.Error, overallCtx.Err()))
		return result
	}
	for _, ip := range ips {
		result.ResolvedIPs = append(result.ResolvedIPs, ip.String())
	}

	conn, remoteAddress, stage := c.connect(overallCtx, ips, port, timeouts.TCP)
	result.Stages = append(result.Stages, stage)
	if !stage.Success {
		result.Failure = failureFromStage(stage, classifyTCPFailure(stage.Error, overallCtx.Err()))
		return result
	}
	result.RemoteAddress = remoteAddress

	if parsedURL.Scheme == "https" {
		conn, stage = c.handshakeTLS(overallCtx, conn, host, timeouts.TLS)
		result.Stages = append(result.Stages, stage)
		if !stage.Success {
			result.Failure = failureFromStage(stage, classifyTLSFailure(stage.Error, overallCtx.Err()))
			return result
		}
	} else {
		result.Stages = append(result.Stages, StageResult{Stage: StageTLS, Skipped: true})
	}

	status, body, stage := c.request(overallCtx, conn, parsedURL, target, timeouts.HTTP)
	result.Stages = append(result.Stages, stage)
	result.HTTPStatusCode = status
	if !stage.Success {
		result.Failure = failureFromStage(stage, classifyHTTPFailure(stage.Error, overallCtx.Err()))
		return result
	}

	if !statusExpected(status, target.ExpectedStatuses) {
		message := fmt.Sprintf("received HTTP status %d; expected one of %v", status, target.ExpectedStatuses)
		stage = StageResult{Stage: StageHTTP, Error: message}
		result.Failure = failureFromStage(stage, ReasonUnexpectedStatus)
		return result
	}

	if target.ExpectedContent != "" {
		result.ContentCheckPerformed = true
		result.ContentMatch = strings.Contains(body, target.ExpectedContent)
		contentStage := StageResult{Stage: StageContent, Success: result.ContentMatch}
		if !result.ContentMatch {
			contentStage.Error = "expected content was not found in the response body"
			result.Stages = append(result.Stages, contentStage)
			result.Failure = failureFromStage(contentStage, ReasonContentMismatch)
			return result
		}
		result.Stages = append(result.Stages, contentStage)
	} else {
		result.Stages = append(result.Stages, StageResult{Stage: StageContent, Skipped: true})
	}

	result.Reachable = true
	return result
}

func validateTarget(target Target) (*url.URL, string, string, error) {
	parsedURL, err := url.Parse(target.URL)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, "", "", fmt.Errorf("unsupported URL scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Hostname() == "" {
		return nil, "", "", errors.New("URL must include a hostname")
	}
	if parsedURL.User != nil {
		return nil, "", "", errors.New("credentials in target URL are not supported")
	}

	port := parsedURL.Port()
	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return nil, "", "", fmt.Errorf("invalid port %q", port)
	}

	for _, status := range target.ExpectedStatuses {
		if status < 100 || status > 599 {
			return nil, "", "", fmt.Errorf("invalid expected HTTP status %d", status)
		}
	}
	if target.MaxResponseBytes < 0 {
		return nil, "", "", errors.New("max response bytes cannot be negative")
	}

	return parsedURL, parsedURL.Hostname(), port, nil
}

func (c *Checker) resolve(ctx context.Context, host string, timeout time.Duration) ([]net.IP, StageResult) {
	startedAt := c.now()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addresses, err := c.resolver.LookupIPAddr(stageCtx, host)
	stage := StageResult{Stage: StageDNS, Duration: c.now().Sub(startedAt)}
	if err != nil {
		stage.Error = err.Error()
		return nil, stage
	}
	if len(addresses) == 0 {
		stage.Error = "DNS returned no addresses"
		return nil, stage
	}

	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ips = append(ips, address.IP)
	}
	stage.Success = true
	return ips, stage
}

func (c *Checker) connect(ctx context.Context, ips []net.IP, port string, timeout time.Duration) (net.Conn, string, StageResult) {
	startedAt := c.now()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dialErrors []error
	for _, ip := range ips {
		address := net.JoinHostPort(ip.String(), port)
		conn, err := c.dialer.DialContext(stageCtx, "tcp", address)
		if err == nil {
			return conn, conn.RemoteAddr().String(), StageResult{
				Stage:    StageTCP,
				Duration: c.now().Sub(startedAt),
				Success:  true,
			}
		}
		dialErrors = append(dialErrors, fmt.Errorf("%s: %w", address, err))
		if stageCtx.Err() != nil {
			break
		}
	}

	err := errors.Join(dialErrors...)
	if err == nil {
		err = errors.New("no resolved IP addresses to dial")
	}
	return nil, "", StageResult{Stage: StageTCP, Duration: c.now().Sub(startedAt), Error: err.Error()}
}

func (c *Checker) handshakeTLS(ctx context.Context, conn net.Conn, host string, timeout time.Duration) (net.Conn, StageResult) {
	startedAt := c.now()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	config := cloneTLSConfig(c.tlsConfig)
	if config.ServerName == "" {
		config.ServerName = host
	}
	// The staged transport currently performs a single HTTP/1.1 request.
	config.NextProtos = []string{"http/1.1"}

	tlsConn := tls.Client(conn, config)
	if err := tlsConn.HandshakeContext(stageCtx); err != nil {
		_ = conn.Close()
		return nil, StageResult{Stage: StageTLS, Duration: c.now().Sub(startedAt), Error: err.Error()}
	}
	return tlsConn, StageResult{Stage: StageTLS, Duration: c.now().Sub(startedAt), Success: true}
}

func (c *Checker) request(ctx context.Context, conn net.Conn, parsedURL *url.URL, target Target, timeout time.Duration) (int, string, StageResult) {
	startedAt := c.now()
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(stageCtx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		_ = conn.Close()
		return 0, "", StageResult{Stage: StageHTTP, Duration: c.now().Sub(startedAt), Error: err.Error()}
	}
	request.Header.Set("User-Agent", "Network-Reachability-Monitor/0.1")
	for name, value := range target.Headers {
		request.Header.Set(name, value)
	}

	oneConn := &singleUseConn{conn: conn}
	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}
	if parsedURL.Scheme == "https" {
		transport.DialTLSContext = oneConn.dial
	} else {
		transport.DialContext = oneConn.dial
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		_ = conn.Close()
		return 0, "", StageResult{Stage: StageHTTP, Duration: c.now().Sub(startedAt), Error: err.Error()}
	}
	defer response.Body.Close()

	maxBytes := target.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxBodyBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return response.StatusCode, "", StageResult{Stage: StageHTTP, Duration: c.now().Sub(startedAt), Error: err.Error()}
	}

	return response.StatusCode, string(body), StageResult{
		Stage:    StageHTTP,
		Duration: c.now().Sub(startedAt),
		Success:  true,
	}
}

type singleUseConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (d *singleUseConn) dial(context.Context, string, string) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return nil, errors.New("the measured connection has already been used")
	}
	conn := d.conn
	d.conn = nil
	return conn, nil
}

func withDefaultTimeouts(timeouts Timeouts) Timeouts {
	if timeouts.Overall <= 0 {
		timeouts.Overall = defaultOverallTimeout
	}
	if timeouts.DNS <= 0 {
		timeouts.DNS = defaultDNSTimeout
	}
	if timeouts.TCP <= 0 {
		timeouts.TCP = defaultTCPTimeout
	}
	if timeouts.TLS <= 0 {
		timeouts.TLS = defaultTLSTimeout
	}
	if timeouts.HTTP <= 0 {
		timeouts.HTTP = defaultHTTPTimeout
	}
	return timeouts
}

func statusExpected(actual int, expected []int) bool {
	if len(expected) == 0 {
		return actual >= 200 && actual < 400
	}
	for _, status := range expected {
		if actual == status {
			return true
		}
	}
	return false
}

func cloneTLSConfig(config *tls.Config) *tls.Config {
	if config == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	cloned := config.Clone()
	if cloned.MinVersion == 0 {
		cloned.MinVersion = tls.VersionTLS12
	}
	return cloned
}

func newFailure(stage Stage, reason FailureReason, err error) *Failure {
	return &Failure{Stage: stage, Reason: reason, Message: err.Error()}
}

func failureFromStage(stage StageResult, reason FailureReason) *Failure {
	return &Failure{Stage: stage.Stage, Reason: reason, Message: stage.Error}
}

func classifyDNSFailure(message string, contextErr error) FailureReason {
	if isTimeout(message, contextErr) {
		return ReasonDNSTimeout
	}
	return ReasonDNSError
}

func classifyTCPFailure(message string, contextErr error) FailureReason {
	if isTimeout(message, contextErr) {
		return ReasonTCPTimeout
	}
	if strings.Contains(strings.ToLower(message), "connection refused") || strings.Contains(message, syscall.ECONNREFUSED.Error()) {
		return ReasonTCPRefused
	}
	return ReasonTCPError
}

func classifyTLSFailure(message string, contextErr error) FailureReason {
	if isTimeout(message, contextErr) {
		return ReasonTLSTimeout
	}
	if strings.Contains(message, "certificate") ||
		strings.Contains(message, "x509") {
		return ReasonTLSCertificateError
	}
	return ReasonTLSError
}

func classifyHTTPFailure(message string, contextErr error) FailureReason {
	if isTimeout(message, contextErr) {
		return ReasonHTTPTimeout
	}
	return ReasonHTTPError
}

func isTimeout(message string, contextErr error) bool {
	return errors.Is(contextErr, context.DeadlineExceeded) ||
		strings.Contains(strings.ToLower(message), "timeout") ||
		strings.Contains(strings.ToLower(message), "deadline exceeded")
}
