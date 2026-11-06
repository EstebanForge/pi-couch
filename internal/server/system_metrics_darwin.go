//go:build darwin

package server

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

func newSystemMetricsSampler() systemMetricsSampler {
	var mu sync.Mutex
	var cached systemMetrics
	var validUntil time.Time
	return func(ctx context.Context) (systemMetrics, error) {
		mu.Lock()
		defer mu.Unlock()
		if time.Now().Before(validUntil) {
			return cached, nil
		}
		cpuOutput, err := darwinCommand(ctx, "top", "-l", "2", "-n", "0", "-s", "1", "-stats", "cpu")
		if err != nil {
			return systemMetrics{}, err
		}
		percent, err := parseDarwinCPU(cpuOutput)
		if err != nil {
			return systemMetrics{}, err
		}
		totalOutput, err := darwinCommand(ctx, "sysctl", "-n", "hw.memsize")
		if err != nil {
			return systemMetrics{}, err
		}
		total, err := strconv.ParseUint(strings.TrimSpace(totalOutput), 10, 64)
		if err != nil || total == 0 {
			return systemMetrics{}, fmt.Errorf("invalid total memory")
		}
		vmOutput, err := darwinCommand(ctx, "vm_stat")
		if err != nil {
			return systemMetrics{}, err
		}
		used, err := parseDarwinMemory(vmOutput, total)
		if err != nil {
			return systemMetrics{}, err
		}
		cached = systemMetrics{
			CPUPercent:      percent,
			MemoryUsedBytes: used,
			MemoryTotal:     total,
			SampledAt:       time.Now(),
		}
		validUntil = cached.SampledAt.Add(time.Second)
		return cached, nil
	}
}

func darwinCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(output), nil
}
