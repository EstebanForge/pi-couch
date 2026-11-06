//go:build linux

package server

import (
	"context"
	"testing"
	"time"
)

func TestLinuxSamplerResetsStaleCPUDelta(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	reads := 0
	idle := []uint64{500, 1000, 1500, 4000}
	readCPU := func() (cpuJiffies, error) {
		reads++
		return cpuJiffies{total: uint64(reads * 1000), idle: idle[reads-1]}, nil
	}
	readMemory := func() (uint64, uint64, error) {
		return 512, 1024, nil
	}
	sample := newLinuxSystemMetricsSampler(readCPU, readMemory, func() time.Time { return now }, 0)
	if _, err := sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("initial sample used %d CPU reads, want priming pair", reads)
	}

	now = now.Add(time.Hour)
	got, err := sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reads != 4 {
		t.Fatalf("stale sample used %d total CPU reads, want a fresh priming pair", reads)
	}
	if got.CPUPercent != 0 {
		t.Fatalf("CPU = %v, want only the fresh priming interval", got.CPUPercent)
	}
}
