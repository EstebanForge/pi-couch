package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHerdrRetryStopsBackoffOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	start := time.Now()
	_, err := herdrRetry(ctx, func() (struct{}, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return struct{}{}, err
		}
		cancel()
		return struct{}{}, errors.New("herdr: dial: timeout")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context cancellation", err)
	}
	if calls != 1 {
		t.Fatalf("calls %d, want no attempt after cancellation", calls)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation waited %s for backoff", elapsed)
	}
}

func TestHerdrRetryDoesNotSleepAfterLastAttempt(t *testing.T) {
	previous := herdrRetryBackoff
	herdrRetryBackoff = 200 * time.Millisecond
	defer func() { herdrRetryBackoff = previous }()
	calls := 0
	lastAttempt := time.Now()
	_, err := herdrRetry(context.Background(), func() (struct{}, error) {
		calls++
		lastAttempt = time.Now()
		return struct{}{}, errors.New("herdr: dial: timeout")
	})
	if err == nil {
		t.Fatal("expected the final dial error")
	}
	if calls != 8 {
		t.Fatalf("calls %d, want 8", calls)
	}
	if elapsed := time.Since(lastAttempt); elapsed > 100*time.Millisecond {
		t.Fatalf("returned %s after the final attempt", elapsed)
	}
}
