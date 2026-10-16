package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/leedcee/uplinkwatch/internal/checker"
)

func TestRunnerPreservesResultOrderAndLimitsConcurrency(t *testing.T) {
	t.Parallel()

	fake := &countingChecker{delay: 20 * time.Millisecond}
	targets := []checker.Target{
		{Name: "one"},
		{Name: "two"},
		{Name: "three"},
		{Name: "four"},
	}
	var eventMu sync.Mutex
	var events []Event
	runner := Runner{
		ProbeID:     "test-probe",
		Concurrency: 2,
		Checker:     fake,
		OnResult: func(event Event) error {
			eventMu.Lock()
			defer eventMu.Unlock()
			events = append(events, event)
			return nil
		},
	}

	results := runner.CheckAll(context.Background(), targets)

	if len(results) != len(targets) {
		t.Fatalf("expected %d results, got %d", len(targets), len(results))
	}
	for index := range targets {
		if results[index].TargetName != targets[index].Name {
			t.Fatalf("result order changed at index %d: %+v", index, results)
		}
	}
	if fake.maximumActive() > 2 {
		t.Fatalf("concurrency limit exceeded: %d", fake.maximumActive())
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) != len(targets) {
		t.Fatalf("expected %d events, got %d", len(targets), len(events))
	}
	for _, event := range events {
		if event.ProbeID != "test-probe" {
			t.Fatalf("unexpected probe ID %q", event.ProbeID)
		}
	}
}

type countingChecker struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (c *countingChecker) Check(_ context.Context, target checker.Target) checker.Result {
	c.mu.Lock()
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()

	time.Sleep(c.delay)

	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return checker.Result{TargetName: target.Name, Reachable: true}
}

func (c *countingChecker) maximumActive() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxActive
}
