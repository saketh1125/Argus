package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock for deterministic rate-limiter tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestLimiter returns a limiter wired to a fake clock fixed at t0.
func newTestLimiter(maxRPM int) (*slidingWindowLimiter, *fakeClock) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	rl := &slidingWindowLimiter{maxRPM: maxRPM, now: clk.Now}
	return rl, clk
}

// TestWindowNeverExceedsMax fires maxRPM+5 calls against a frozen fake clock and
// asserts the in-window count never exceeds maxRPM, and that being at the
// ceiling forces a wait (proved via a short real context deadline) without
// consuming a slot.
func TestWindowNeverExceedsMax(t *testing.T) {
	const maxRPM = 5
	rl, _ := newTestLimiter(maxRPM)
	ctx := context.Background()

	// Fill the window with maxRPM granted calls (clock frozen, so all in-window).
	for i := 0; i < maxRPM; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("call %d unexpectedly blocked/failed: %v", i, err)
		}
		if cur, _ := rl.Snapshot(); cur > maxRPM {
			t.Fatalf("window exceeded max: got %d, max %d", cur, maxRPM)
		}
	}

	cur, max := rl.Snapshot()
	if cur != maxRPM || max != maxRPM {
		t.Fatalf("expected window full at %d/%d, got %d/%d", maxRPM, maxRPM, cur, max)
	}

	// At ceiling: the next 5 calls must block. With the fake clock frozen the
	// oldest call never ages out, so each Wait blocks on its timer until the real
	// ctx deadline fires. Assert it returns DeadlineExceeded and does NOT consume
	// a slot (count stays at maxRPM).
	for i := 0; i < 5; i++ {
		wctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		err := rl.Wait(wctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("call past ceiling %d: expected DeadlineExceeded, got %v", i, err)
		}
		if c, _ := rl.Snapshot(); c != maxRPM {
			t.Fatalf("ceiling-blocked call consumed a slot: count %d, want %d", c, maxRPM)
		}
	}
}

// TestEvictionFreesSlot verifies that advancing the fake clock past the window
// evicts old calls so a new call is granted immediately.
func TestEvictionFreesSlot(t *testing.T) {
	const maxRPM = 3
	rl, clk := newTestLimiter(maxRPM)
	ctx := context.Background()

	for i := 0; i < maxRPM; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("fill call %d failed: %v", i, err)
		}
	}
	if cur, _ := rl.Snapshot(); cur != maxRPM {
		t.Fatalf("window should be full: got %d", cur)
	}

	// Advance just past the 60s window: all prior calls expire.
	clk.Advance(61 * time.Second)
	if cur, _ := rl.Snapshot(); cur != 0 {
		t.Fatalf("window should be empty after eviction: got %d", cur)
	}

	// A fresh call is granted without blocking.
	wctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := rl.Wait(wctx); err != nil {
		t.Fatalf("post-eviction call should succeed immediately, got %v", err)
	}
	if cur, _ := rl.Snapshot(); cur != 1 {
		t.Fatalf("expected 1 call after eviction+grant, got %d", cur)
	}
}

// TestUnlimited verifies maxRPM<=0 never blocks.
func TestUnlimited(t *testing.T) {
	rl := NewRateLimiter(0)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("unlimited limiter blocked at %d: %v", i, err)
		}
	}
}
