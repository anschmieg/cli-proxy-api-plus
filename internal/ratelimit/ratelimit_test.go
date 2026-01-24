package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	// Rate: 10 tokens per second, Capacity: 5
	tb := NewTokenBucket(10.0, 5.0)

	// Should allow 5 immediate bursts
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("expected request %d to be allowed", i+1)
		}
	}

	// 6th should be denied
	if tb.Allow() {
		t.Error("expected 6th request to be denied")
	}

	// Wait for 100ms (should gain 1 token)
	time.Sleep(110 * time.Millisecond)
	if !tb.Allow() {
		t.Error("expected request to be allowed after waiting")
	}
}

func TestTokenBucket_Wait(t *testing.T) {
	// Rate: 100 tokens per second, Capacity: 1
	tb := NewTokenBucket(100.0, 1.0)

	// Consume the only token
	tb.Allow()

	// Wait for next token
	start := time.Now()
	err := tb.Wait(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	
	elapsed := time.Since(start)
	if elapsed < 5*time.Millisecond {
		t.Errorf("Wait returned too early: %v", elapsed)
	}
}

func TestFixedWindowLimiter(t *testing.T) {
	// Limit: 2 requests per 100ms
	fw := NewFixedWindowLimiter(2, 100*time.Millisecond)

	if !fw.Allow() { t.Error("expected 1st to be allowed") }
	if !fw.Allow() { t.Error("expected 2nd to be allowed") }
	if fw.Allow() { t.Error("expected 3rd to be denied") }

	time.Sleep(110 * time.Millisecond)
	if !fw.Allow() { t.Error("expected request to be allowed after window reset") }
}
