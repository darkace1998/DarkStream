package worker

import (
	"bufio"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// cpuStats holds CPU time counters read from /proc/stat
type cpuStats struct {
	total uint64
	idle  uint64
}

var (
	prevCPU     cpuStats
	prevCPUInit bool
	cpuMu       sync.Mutex
)

// getSystemMemoryUsage returns system memory usage as a percentage (0.0-100.0).
// On Linux, it reads /proc/meminfo for system-wide memory usage.
// Falls back to Go runtime memory stats on other platforms.
func getSystemMemoryUsage() float64 {
	// Try Linux procfs first
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return getGoMemoryUsage()
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Debug("Failed to close /proc/meminfo", "error", cerr)
		}
	}()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemInfoValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseMemInfoValue(line)
		}
		if memTotal > 0 && memAvailable > 0 {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Debug("Failed to read /proc/meminfo", "error", err)
	}

	if memTotal == 0 {
		return getGoMemoryUsage()
	}

	if memAvailable > memTotal {
		slog.Debug("MemAvailable > MemTotal, clamping to zero",
			"total", memTotal, "available", memAvailable)
		return 0.0
	}

	used := memTotal - memAvailable
	return float64(used) / float64(memTotal) * 100.0
}

// parseMemInfoValue extracts the numeric value from a /proc/meminfo line (in kB)
func parseMemInfoValue(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// getGoMemoryUsage returns Go process memory usage as a percentage of memory obtained from the OS by the Go runtime.
// This is not system-wide memory usage; it is used as a fallback on non-Linux platforms.
func getGoMemoryUsage() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.Sys == 0 {
		return 0.0
	}
	return float64(m.Alloc) / float64(m.Sys) * 100.0
}

// getSystemCPUUsage returns system CPU usage as a percentage (0.0-100.0).
// On Linux, it reads /proc/stat and computes the delta from the previous call.
// Returns 0.0 on non-Linux platforms or on the first call.
func getSystemCPUUsage() float64 {
	current := readCPUStats()
	if current.total == 0 {
		return 0.0
	}

	cpuMu.Lock()
	defer cpuMu.Unlock()

	if !prevCPUInit {
		prevCPU = current
		prevCPUInit = true
		return 0.0
	}

	prev := prevCPU
	prevCPU = current

	totalDelta := current.total - prev.total
	idleDelta := current.idle - prev.idle

	if totalDelta == 0 {
		return 0.0
	}

	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
}

// readCPUStats reads aggregate CPU counters from /proc/stat.
// Returns zero-value cpuStats if /proc/stat is unavailable (non-Linux).
func readCPUStats() cpuStats {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStats{}
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Debug("Failed to close /proc/stat", "error", cerr)
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuStats{}
		}

		var total uint64
		for i := 1; i < len(fields); i++ {
			val, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				continue
			}
			total += val
		}

		// idle is the 4th value after "cpu" (fields[4])
		idle, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			return cpuStats{}
		}

		return cpuStats{total: total, idle: idle}
	}

	if err := scanner.Err(); err != nil {
		slog.Debug("Failed to read /proc/stat", "error", err)
	}

	return cpuStats{}
}
