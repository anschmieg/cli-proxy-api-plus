package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter defines the interface for a rate limiter.
type Limiter interface {
	// Allow checks if a request is allowed. If allowed, it consumes a token.
	Allow() bool
	// Wait blocks until a token is available or the context is canceled.
	Wait(ctx context.Context) error
}

// TokenBucket implements a thread-safe token bucket rate limiter.
type TokenBucket struct {
	rate       float64 // tokens per second
	capacity   float64 // max tokens
	tokens     float64 // current tokens
	lastUpdate time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a new TokenBucket limiter.
// rate: tokens per second (e.g., 5.0 for 5 req/sec). 0 means unlimited.
// capacity: maximum burst size.
func NewTokenBucket(rate float64, capacity float64) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     capacity, // Start full
		lastUpdate: time.Now(),
	}
}

// Allow checks if a request is allowed.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	if tb.rate <= 0 {
		return true // Unlimited
	}

	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastUpdate = now

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// Wait blocks until a token is available or the context is canceled.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	tb.mu.Lock()
	if tb.rate <= 0 {
		tb.mu.Unlock()
		return nil // Unlimited
	}

	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastUpdate = now

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		tb.mu.Unlock()
		return nil
	}

	// Calculate time to wait for 1 token
	missing := 1.0 - tb.tokens
	waitDuration := time.Duration((missing / tb.rate) * float64(time.Second))
	tb.mu.Unlock()

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		// Re-check (naive, but safe for now; strict ordering requires a queue)
		// In a highly contended scenario, this might loop, but for our API proxy use case
		// (one request per user mostly), it's acceptable.
		return tb.Wait(ctx)
	}
}

// FixedWindowLimiter implements a simple fixed window counter (e.g., 10 reqs / minute).
type FixedWindowLimiter struct {
	limit    int
	window   time.Duration
	count    int
	windowStart time.Time
	mu       sync.Mutex
}

// NewFixedWindowLimiter creates a new FixedWindowLimiter.
func NewFixedWindowLimiter(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:       limit,
		window:      window,
		windowStart: time.Now(),
	}
}

func (fw *FixedWindowLimiter) Allow() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now()
	if now.Sub(fw.windowStart) >= fw.window {
		fw.windowStart = now
		fw.count = 0
	}

	if fw.count < fw.limit {
		fw.count++
		return true
	}
	return false
}

func (fw *FixedWindowLimiter) Wait(ctx context.Context) error {
	for {
		if fw.Allow() {
			return nil
		}
		
		fw.mu.Lock()
		remaining := fw.window - time.Since(fw.windowStart)
		fw.mu.Unlock()

		if remaining <= 0 {
			continue
		}

		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Retry loop
		}
	}
}
