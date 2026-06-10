package services

import (
	"context"
	"sync"
	"time"
)

// window is the sliding-window span over which calls are counted against the
// RPM ceiling.
const window = 60 * time.Second

// ceilingBuffer is added to the computed sleep so that, when we wake, the oldest
// in-window call has definitively expired (clock granularity slack).
const ceilingBuffer = 100 * time.Millisecond

// slidingWindowLimiter is a thread-safe sliding-window rate limiter implementing
// services.RateLimiter (design §7). It records the timestamp of each granted
// call and, when the number of calls inside the trailing 60s window reaches the
// configured maximum, blocks the next caller until the oldest in-window call
// ages out.
//
// Unlike the design-doc sketch, Wait never sleeps while holding the mutex and is
// fully context-aware: a cancelled or expired context aborts the wait without
// consuming a slot.
type slidingWindowLimiter struct {
	mu     sync.Mutex
	maxRPM int
	calls  []time.Time

	// now is the clock source, swappable in tests for a fake clock. Defaults to
	// time.Now.
	now func() time.Time
}

// NewRateLimiter returns a sliding-window RateLimiter capped at maxRPM calls per
// rolling 60-second window. A maxRPM <= 0 is treated as effectively unlimited
// (it never blocks).
func NewRateLimiter(maxRPM int) RateLimiter {
	return &slidingWindowLimiter{
		maxRPM: maxRPM,
		now:    time.Now,
	}
}

// evict drops every recorded call that is no longer inside the trailing window
// relative to ref, mutating r.calls in place. The caller must hold r.mu.
func (r *slidingWindowLimiter) evict(ref time.Time) {
	cutoff := ref.Add(-window)
	valid := r.calls[:0]
	for _, t := range r.calls {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	r.calls = valid
}

// Wait blocks until a call slot is available within the rolling window, then
// records the granted call and returns nil. If ctx is cancelled or its deadline
// elapses while waiting for a slot, Wait returns ctx.Err() WITHOUT consuming a
// slot.
func (r *slidingWindowLimiter) Wait(ctx context.Context) error {
	if r.maxRPM <= 0 {
		// Unlimited: still respect an already-cancelled context.
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		r.calls = append(r.calls, r.now())
		r.mu.Unlock()
		return nil
	}

	for {
		r.mu.Lock()
		now := r.now()
		r.evict(now)

		if len(r.calls) < r.maxRPM {
			// Slot available: record it on the success path only.
			r.calls = append(r.calls, now)
			r.mu.Unlock()
			return nil
		}

		// At ceiling: compute how long until the oldest in-window call expires,
		// release the lock, then wait on a timer or context cancellation.
		sleep := r.calls[0].Add(window).Sub(now) + ceilingBuffer
		r.mu.Unlock()

		if sleep <= 0 {
			// Should not happen after eviction, but guard against a spin loop.
			sleep = ceilingBuffer
		}

		timer := time.NewTimer(sleep)
		select {
		case <-timer.C:
			// Loop and re-check; another goroutine may have taken the freed slot.
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// Snapshot returns the number of calls currently inside the trailing window and
// the configured maximum. It evicts stale entries as a side effect so the
// reported count reflects the live window.
func (r *slidingWindowLimiter) Snapshot() (current, max int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evict(r.now())
	return len(r.calls), r.maxRPM
}
