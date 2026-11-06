package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type systemMetrics struct {
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryUsedBytes uint64    `json:"memory_used_bytes"`
	MemoryTotal     uint64    `json:"memory_total_bytes"`
	SampledAt       time.Time `json:"sampled_at"`
}

type systemMetricsSampler func(context.Context) (systemMetrics, error)

type cpuJiffies struct {
	total uint64
	idle  uint64
}

func (a *app) apiSystemMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.systemMetrics == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	sample, err := a.systemMetrics(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, sample)
}

func cpuUsagePercent(previous, current cpuJiffies) float64 {
	if current.total <= previous.total || current.idle < previous.idle {
		return 0
	}
	total := current.total - previous.total
	idle := current.idle - previous.idle
	if idle >= total {
		return 0
	}
	return roundPercent(float64(total-idle) * 100 / float64(total))
}

func roundPercent(value float64) float64 {
	value = math.Max(0, math.Min(100, value))
	return math.Round(value*10) / 10
}

func parseProcCPU(contents string) (cpuJiffies, error) {
	for line := range strings.Lines(contents) {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		if len(fields) < 5 {
			return cpuJiffies{}, fmt.Errorf("short aggregate CPU row")
		}
		var sample cpuJiffies
		// Guest time is already included in user and nice, so stop at steal.
		for i := 1; i < len(fields) && i <= 8; i++ {
			value, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return cpuJiffies{}, fmt.Errorf("CPU counter: %w", err)
			}
			sample.total += value
			if i == 4 || i == 5 {
				sample.idle += value
			}
		}
		return sample, nil
	}
	return cpuJiffies{}, fmt.Errorf("aggregate CPU row missing")
}

func parseProcMemory(contents string) (used, total uint64, err error) {
	values := map[string]uint64{}
	for line := range strings.Lines(contents) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		if len(fields) != 3 || fields[2] != "kB" {
			return 0, 0, fmt.Errorf("%s has an unexpected unit", key)
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("%s: %w", key, parseErr)
		}
		values[key] = value * 1024
	}
	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	if !hasTotal || !hasAvailable || total == 0 || available > total {
		return 0, 0, fmt.Errorf("invalid memory counters")
	}
	return total - available, total, nil
}

func parseDarwinCPU(output string) (float64, error) {
	var percent float64
	found := false
	for line := range strings.Lines(output) {
		if !strings.Contains(line, "CPU usage:") {
			continue
		}
		parts := strings.Split(line, ",")
		idle := strings.Fields(parts[len(parts)-1])
		if len(idle) != 2 || idle[1] != "idle" {
			return 0, fmt.Errorf("unexpected CPU sample")
		}
		value, err := strconv.ParseFloat(strings.TrimSuffix(idle[0], "%"), 64)
		if err != nil {
			return 0, fmt.Errorf("CPU sample: %w", err)
		}
		percent = roundPercent(100 - value)
		found = true
	}
	if !found {
		return 0, fmt.Errorf("CPU sample missing")
	}
	return percent, nil
}

func parseDarwinMemory(output string, total uint64) (uint64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 || total == 0 {
		return 0, fmt.Errorf("invalid memory counters")
	}
	var pageSize uint64
	header := strings.Fields(lines[0])
	for i := 0; i < len(header)-1; i++ {
		if header[i] == "of" {
			pageSize, _ = strconv.ParseUint(header[i+1], 10, 64)
			break
		}
	}
	if pageSize == 0 {
		return 0, fmt.Errorf("page size missing")
	}

	values := map[string]uint64{}
	required := []string{"Anonymous pages", "Pages purgeable", "Pages wired down", "Pages occupied by compressor"}
	for _, line := range lines[1:] {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := line[:colon]
		value, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(line[colon+1:]), "."), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		values[key] = value
	}
	for _, key := range required {
		if _, ok := values[key]; !ok {
			return 0, fmt.Errorf("%s missing", key)
		}
	}
	if values["Pages purgeable"] > values["Anonymous pages"] {
		return 0, fmt.Errorf("invalid purgeable pages")
	}
	// Activity Monitor's used memory is app memory plus wired and compressed pages.
	pages := values["Anonymous pages"] - values["Pages purgeable"] + values["Pages wired down"] + values["Pages occupied by compressor"]
	used := pages * pageSize
	if used > total {
		return 0, fmt.Errorf("used memory above total")
	}
	return used, nil
}
