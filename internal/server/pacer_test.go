package server

import (
	"context"
	"testing"
	"time"
)

func TestPacerFirstCallImmediate(t *testing.T) {
	ticks := make(chan struct{}, 8)
	p := &pacer{}
	start := time.Now()
	if !p.wait(context.Background(), ticks) {
		t.Fatal("wait returned false")
	}
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("first wait took %s, want immediate", d)
	}
}

func TestPacerThrottlesFlood(t *testing.T) {
	// A firehose of ticks must not produce patches faster than the
	// minimum interval: the exact flood that froze clients on active
	// sessions.
	ticks := make(chan struct{}, 256)
	go func() {
		for {
			ticks <- struct{}{}
			time.Sleep(time.Millisecond)
		}
	}()
	p := &pacer{}
	p.wait(context.Background(), ticks) // first patch, starts the clock
	start := time.Now()
	n := 0
	for time.Since(start) < 250*time.Millisecond {
		if !p.wait(context.Background(), ticks) {
			t.Fatal("wait returned false")
		}
		n++
	}
	// 250ms window at 1s minimum between patches: exactly the one
	// immediate patch plus margin for scheduling slack, never a patch
	// per millisecond tick.
	if n > 2 {
		t.Fatalf("%d waits in 250ms with a 1s minimum interval", n)
	}
}

func TestPacerCtxCancel(t *testing.T) {
	ticks := make(chan struct{})
	p := &pacer{}
	p.wait(context.Background(), ticks)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p.wait(ctx, ticks) {
		t.Fatal("cancelled ctx should stop the wait")
	}
}
