// Package agent coordinates checks for all targets assigned to one probe.
package agent

import (
	"context"
	"sync"
	"time"

	"github.com/leedcee/uplinkwatch/internal/checker"
)

// TargetChecker is implemented by checker.Checker.
type TargetChecker interface {
	Check(ctx context.Context, target checker.Target) checker.Result
}

// Event wraps a result with the point from which it was observed.
type Event struct {
	ProbeID   string         `json:"probe_id"`
	EmittedAt time.Time      `json:"emitted_at"`
	Result    checker.Result `json:"result"`
}

// Runner checks a set of targets with bounded concurrency.
type Runner struct {
	ProbeID       string
	Concurrency   int
	Checker       TargetChecker
	Now           func() time.Time
	OnResult      func(Event) error
	OnOutputError func(error)
}

// CheckAll performs one check per target. It returns after all checks finish or
// the context is cancelled.
func (r Runner) CheckAll(ctx context.Context, targets []checker.Target) []checker.Result {
	concurrency := r.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(targets) {
		concurrency = len(targets)
	}
	if concurrency == 0 {
		return nil
	}

	results := make([]checker.Result, len(targets))
	jobs := make(chan int)
	var workers sync.WaitGroup

	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				result := r.Checker.Check(ctx, targets[index])
				results[index] = result
				r.emit(result)
			}
		}()
	}

	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return results
		}
	}
	close(jobs)
	workers.Wait()
	return results
}

func (r Runner) emit(result checker.Result) {
	if r.OnResult == nil {
		return
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	if err := r.OnResult(Event{ProbeID: r.ProbeID, EmittedAt: now(), Result: result}); err != nil && r.OnOutputError != nil {
		r.OnOutputError(err)
	}
}
