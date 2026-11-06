package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSystemMetricsHandler(t *testing.T) {
	sampledAt := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	t.Run("sample", func(t *testing.T) {
		a := &app{systemMetrics: func(context.Context) (systemMetrics, error) {
			return systemMetrics{
				CPUPercent:      22.4,
				MemoryUsedBytes: 786_432,
				MemoryTotal:     1_048_576,
				SampledAt:       sampledAt,
			}, nil
		}}
		rec := httptest.NewRecorder()
		a.apiSystemMetrics(rec, httptest.NewRequest(http.MethodGet, "/api/system/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q", got)
		}
		var got systemMetrics
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.CPUPercent != 22.4 || got.MemoryUsedBytes != 786_432 || got.MemoryTotal != 1_048_576 {
			t.Fatalf("sample = %+v", got)
		}
		if !got.SampledAt.Equal(sampledAt) {
			t.Fatalf("SampledAt = %s", got.SampledAt)
		}
	})

	t.Run("unavailable does not expose collector errors", func(t *testing.T) {
		a := &app{systemMetrics: func(context.Context) (systemMetrics, error) {
			return systemMetrics{}, errors.New("secret collector detail")
		}}
		rec := httptest.NewRecorder()
		a.apiSystemMetrics(rec, httptest.NewRequest(http.MethodGet, "/api/system/metrics", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", rec.Code)
		}
		var got map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["error"] != "unavailable" {
			t.Fatalf("body = %v", got)
		}
	})
}

func TestCPUUsagePercent(t *testing.T) {
	tests := []struct {
		name              string
		previous, current cpuJiffies
		want              float64
	}{
		{name: "busy delta", previous: cpuJiffies{total: 1000, idle: 600}, current: cpuJiffies{total: 2000, idle: 700}, want: 90},
		{name: "all idle", previous: cpuJiffies{total: 1000, idle: 600}, current: cpuJiffies{total: 2000, idle: 1600}, want: 0},
		{name: "counter reset", previous: cpuJiffies{total: 2000, idle: 1000}, current: cpuJiffies{total: 1000, idle: 500}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cpuUsagePercent(tt.previous, tt.current); got != tt.want {
				t.Fatalf("cpuUsagePercent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseProcCPU(t *testing.T) {
	got, err := parseProcCPU("cpu 100 20 30 400 50 5 5 5 100 50\ncpu0 1 2 3 4 5 6 7 8\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.total != 615 || got.idle != 450 {
		t.Fatalf("sample = %+v, want total 615 and idle 450", got)
	}
	if _, err := parseProcCPU("cpu0 1 2 3 4\n"); err == nil {
		t.Fatal("expected a missing aggregate CPU row to fail")
	}
}

func TestParseProcMemory(t *testing.T) {
	used, total, err := parseProcMemory("MemTotal: 1024 kB\nMemFree: 100 kB\nMemAvailable: 256 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1_048_576 || used != 786_432 {
		t.Fatalf("used,total = %d,%d", used, total)
	}
	if _, _, err := parseProcMemory("MemTotal: 1024 kB\n"); err == nil {
		t.Fatal("expected missing MemAvailable to fail")
	}
	if _, _, err := parseProcMemory("MemTotal: 128 kB\nMemAvailable: 256 kB\n"); err == nil {
		t.Fatal("expected available memory above total to fail")
	}
}

func TestParseDarwinCPU(t *testing.T) {
	output := "Processes: 500 total\nCPU usage: 8.0% user, 17.0% sys, 75.0% idle\nCPU usage: 20.5% user, 42.5% sys, 37.0% idle\n"
	got, err := parseDarwinCPU(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != 63 {
		t.Fatalf("CPU = %v, want 63", got)
	}
	if _, err := parseDarwinCPU("Processes: 500 total\n"); err == nil {
		t.Fatal("expected missing CPU samples to fail")
	}
}

func TestParseDarwinMemory(t *testing.T) {
	output := `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                                500.
Anonymous pages:                          1000.
Pages purgeable:                           100.
Pages wired down:                          200.
Pages occupied by compressor:               50.
`
	used, err := parseDarwinMemory(output, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if used != 4_710_400 {
		t.Fatalf("used = %d, want 4710400", used)
	}
	if _, err := parseDarwinMemory("Pages free: 1.\n", 10_000_000); err == nil {
		t.Fatal("expected incomplete vm_stat output to fail")
	}
}
