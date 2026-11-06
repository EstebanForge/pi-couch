//go:build linux

package server

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

const staleCPUDelta = 30 * time.Second

func newSystemMetricsSampler() systemMetricsSampler {
	return newLinuxSystemMetricsSampler(readLinuxCPU, readLinuxMemory, time.Now, 150*time.Millisecond)
}

func newLinuxSystemMetricsSampler(
	readCPU func() (cpuJiffies, error),
	readMemory func() (uint64, uint64, error),
	now func() time.Time,
	prime time.Duration,
) systemMetricsSampler {
	var mu sync.Mutex
	var previous cpuJiffies
	var previousAt time.Time
	var cached systemMetrics
	var validUntil time.Time
	return func(ctx context.Context) (systemMetrics, error) {
		mu.Lock()
		defer mu.Unlock()
		if now().Before(validUntil) {
			return cached, nil
		}
		current, err := readCPU()
		if err != nil {
			return systemMetrics{}, err
		}
		if previous.total == 0 || now().Sub(previousAt) > staleCPUDelta {
			previous = current
			timer := time.NewTimer(prime)
			select {
			case <-ctx.Done():
				timer.Stop()
				return systemMetrics{}, ctx.Err()
			case <-timer.C:
			}
			current, err = readCPU()
			if err != nil {
				return systemMetrics{}, err
			}
		}
		percent := cpuUsagePercent(previous, current)
		previous = current
		previousAt = now()
		used, total, err := readMemory()
		if err != nil {
			return systemMetrics{}, err
		}
		cached = systemMetrics{
			CPUPercent:      percent,
			MemoryUsedBytes: used,
			MemoryTotal:     total,
			SampledAt:       now(),
		}
		validUntil = cached.SampledAt.Add(time.Second)
		return cached, nil
	}
}

func readLinuxCPU() (cpuJiffies, error) {
	contents, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuJiffies{}, fmt.Errorf("read CPU counters: %w", err)
	}
	return parseProcCPU(string(contents))
}

func readLinuxMemory() (used, total uint64, err error) {
	contents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read memory counters: %w", err)
	}
	return parseProcMemory(string(contents))
}
