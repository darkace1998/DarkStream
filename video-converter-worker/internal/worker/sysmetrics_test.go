package worker

import (
	"testing"
)

func TestGetSystemMemoryUsage(t *testing.T) {
	usage := getSystemMemoryUsage()
	// Memory usage should be a valid percentage (0-100)
	if usage < 0 || usage > 100 {
		t.Errorf("getSystemMemoryUsage() = %v, want 0-100", usage)
	}
	// On any real system, memory usage should be > 0
	if usage == 0 {
		t.Log("getSystemMemoryUsage() returned 0.0 (may be expected on non-Linux)")
	}
}

func TestGetSystemCPUUsage(t *testing.T) {
	// First call initializes previous state, returns 0.0
	usage1 := getSystemCPUUsage()
	if usage1 < 0 || usage1 > 100 {
		t.Errorf("getSystemCPUUsage() first call = %v, want 0-100", usage1)
	}

	// Second call should return a delta-based value
	usage2 := getSystemCPUUsage()
	if usage2 < 0 || usage2 > 100 {
		t.Errorf("getSystemCPUUsage() second call = %v, want 0-100", usage2)
	}
}

func TestGetGoMemoryUsage(t *testing.T) {
	usage := getGoMemoryUsage()
	// Go memory usage should always return a valid percentage
	if usage < 0 || usage > 100 {
		t.Errorf("getGoMemoryUsage() = %v, want 0-100", usage)
	}
}

func TestParseMemInfoValue(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected uint64
	}{
		{
			name:     "valid MemTotal line",
			line:     "MemTotal:       16384000 kB",
			expected: 16384000,
		},
		{
			name:     "valid MemAvailable line",
			line:     "MemAvailable:    8192000 kB",
			expected: 8192000,
		},
		{
			name:     "empty line",
			line:     "",
			expected: 0,
		},
		{
			name:     "malformed line",
			line:     "MemTotal:",
			expected: 0,
		},
		{
			name:     "non-numeric value",
			line:     "MemTotal:       abc kB",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemInfoValue(tt.line)
			if result != tt.expected {
				t.Errorf("parseMemInfoValue(%q) = %d, want %d", tt.line, result, tt.expected)
			}
		})
	}
}

func TestReadCPUStats(t *testing.T) {
	stats := readCPUStats()
	// On Linux, total should be > 0; on other platforms, returns zero
	if stats.total > 0 && stats.idle > stats.total {
		t.Errorf("readCPUStats() idle (%d) > total (%d)", stats.idle, stats.total)
	}
}
