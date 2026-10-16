package checker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckHTTPIsReachable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.UserAgent() != "Network-Reachability-Monitor/0.1" {
			t.Errorf("unexpected user agent %q", request.UserAgent())
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("service is healthy"))
	}))
	defer server.Close()

	result := New(Options{}).Check(context.Background(), Target{
		Name:             "local-test",
		URL:              server.URL,
		ExpectedStatuses: []int{http.StatusOK},
		ExpectedContent:  "healthy",
	})

	if !result.Reachable {
		t.Fatalf("expected reachable result, got failure: %+v", result.Failure)
	}
	if result.HTTPStatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", result.HTTPStatusCode)
	}
	if !result.ContentCheckPerformed || !result.ContentMatch {
		t.Fatalf("expected successful content check: %+v", result)
	}
	assertStage(t, result, StageDNS, true, false)
	assertStage(t, result, StageTCP, true, false)
	assertStage(t, result, StageTLS, false, true)
	assertStage(t, result, StageHTTP, true, false)
	assertStage(t, result, StageContent, true, false)
}

func TestCheckHTTPSIsReachableWithTrustedCertificate(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	tlsConfig := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	result := New(Options{TLSConfig: tlsConfig}).Check(context.Background(), Target{
		URL:              server.URL,
		ExpectedStatuses: []int{http.StatusNoContent},
	})

	if !result.Reachable {
		t.Fatalf("expected reachable result, got failure: %+v", result.Failure)
	}
	assertStage(t, result, StageTLS, true, false)
	assertStage(t, result, StageHTTP, true, false)
}

func TestCheckClassifiesUntrustedTLSCertificate(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	result := New(Options{}).Check(context.Background(), Target{URL: server.URL})

	if result.Reachable {
		t.Fatal("expected unreachable result")
	}
	assertFailure(t, result, StageTLS, ReasonTLSCertificateError)
}

func TestCheckReportsUnexpectedStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	result := New(Options{}).Check(context.Background(), Target{
		URL:              server.URL,
		ExpectedStatuses: []int{http.StatusOK},
	})

	if result.Reachable {
		t.Fatal("expected unreachable result")
	}
	if result.HTTPStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", result.HTTPStatusCode)
	}
	assertFailure(t, result, StageHTTP, ReasonUnexpectedStatus)
}

func TestCheckReportsContentMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("unexpected response"))
	}))
	defer server.Close()

	result := New(Options{}).Check(context.Background(), Target{
		URL:             server.URL,
		ExpectedContent: "healthy",
	})

	if result.Reachable {
		t.Fatal("expected unreachable result")
	}
	if !result.ContentCheckPerformed || result.ContentMatch {
		t.Fatalf("expected failed content check: %+v", result)
	}
	assertFailure(t, result, StageContent, ReasonContentMismatch)
}

func TestCheckRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	result := New(Options{}).Check(context.Background(), Target{URL: "ftp://example.com/file"})

	if result.Reachable {
		t.Fatal("expected unreachable result")
	}
	assertFailure(t, result, StageHTTP, ReasonInvalidTarget)
}

func TestCheckClassifiesDNSTimeout(t *testing.T) {
	t.Parallel()

	result := New(Options{Resolver: blockingResolver{}}).Check(context.Background(), Target{
		URL: "https://example.test",
		Timeouts: Timeouts{
			Overall: time.Second,
			DNS:     10 * time.Millisecond,
		},
	})

	if result.Reachable {
		t.Fatal("expected unreachable result")
	}
	assertFailure(t, result, StageDNS, ReasonDNSTimeout)
}

type blockingResolver struct{}

func (blockingResolver) LookupIPAddr(ctx context.Context, _ string) ([]net.IPAddr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func assertFailure(t *testing.T, result Result, stage Stage, reason FailureReason) {
	t.Helper()
	if result.Failure == nil {
		t.Fatal("expected failure details")
	}
	if result.Failure.Stage != stage || result.Failure.Reason != reason {
		t.Fatalf("expected %s/%s failure, got %+v", stage, reason, result.Failure)
	}
}

func assertStage(t *testing.T, result Result, expected Stage, success, skipped bool) {
	t.Helper()
	for _, stage := range result.Stages {
		if stage.Stage == expected {
			if stage.Success != success || stage.Skipped != skipped {
				t.Fatalf("unexpected %s stage: %+v", expected, stage)
			}
			return
		}
	}
	t.Fatalf("stage %s was not recorded", expected)
}
