package utils

import (
	"testing"
	"time"
)

func TestMetricsCounter(t *testing.T) {
	m := NewMetrics()

	m.Inc("requests")
	if got := m.GetCounter("requests"); got != 1 {
		t.Errorf("GetCounter(requests) = %d, want 1", got)
	}

	m.Inc("requests")
	if got := m.GetCounter("requests"); got != 2 {
		t.Errorf("GetCounter(requests) = %d, want 2", got)
	}

	m.Add("requests", 5)
	if got := m.GetCounter("requests"); got != 7 {
		t.Errorf("GetCounter(requests) = %d, want 7", got)
	}

	// Non-existent counter should return 0
	if got := m.GetCounter("nonexistent"); got != 0 {
		t.Errorf("GetCounter(nonexistent) = %d, want 0", got)
	}
}

func TestMetricsGauge(t *testing.T) {
	m := NewMetrics()

	m.SetGauge("cpu", 45.5)
	if got := m.GetGauge("cpu"); got != 45.5 {
		t.Errorf("GetGauge(cpu) = %f, want 45.5", got)
	}

	m.SetGauge("cpu", 60.0)
	if got := m.GetGauge("cpu"); got != 60.0 {
		t.Errorf("GetGauge(cpu) = %f, want 60.0", got)
	}

	// Non-existent gauge should return 0
	if got := m.GetGauge("nonexistent"); got != 0 {
		t.Errorf("GetGauge(nonexistent) = %f, want 0", got)
	}
}

func TestMetricsTimer(t *testing.T) {
	m := NewMetrics()

	timer := m.Timer("operation")
	timer.Record(100 * time.Millisecond)
	timer.Record(200 * time.Millisecond)
	timer.Record(150 * time.Millisecond)

	if timer.Count() != 3 {
		t.Errorf("Timer.Count() = %d, want 3", timer.Count())
	}

	expectedTotal := 450 * time.Millisecond
	if timer.Total() != expectedTotal {
		t.Errorf("Timer.Total() = %v, want %v", timer.Total(), expectedTotal)
	}

	if timer.Min() != 100*time.Millisecond {
		t.Errorf("Timer.Min() = %v, want 100ms", timer.Min())
	}

	if timer.Max() != 200*time.Millisecond {
		t.Errorf("Timer.Max() = %v, want 200ms", timer.Max())
	}

	expectedAvg := 150 * time.Millisecond
	if timer.Avg() != expectedAvg {
		t.Errorf("Timer.Avg() = %v, want %v", timer.Avg(), expectedAvg)
	}
}

func TestMetricsTimeOperation(t *testing.T) {
	m := NewMetrics()

	m.TimeOperation("test", func() {
		time.Sleep(10 * time.Millisecond)
	})

	timer := m.Timer("test")
	if timer.Count() != 1 {
		t.Errorf("Timer.Count() = %d, want 1", timer.Count())
	}
	if timer.Total() < 10*time.Millisecond {
		t.Errorf("Timer.Total() = %v, expected >= 10ms", timer.Total())
	}
}

func TestMetricsTimeOperationWithError(t *testing.T) {
	m := NewMetrics()

	err := m.TimeOperationWithError("test", func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Errorf("TimeOperationWithError() error = %v", err)
	}

	timer := m.Timer("test")
	if timer.Count() != 1 {
		t.Errorf("Timer.Count() = %d, want 1", timer.Count())
	}
}

func TestMetricsSnapshot(t *testing.T) {
	m := NewMetrics()

	m.Inc("requests")
	m.SetGauge("cpu", 50.0)
	m.Timer("operation").Record(100 * time.Millisecond)

	snapshot := m.Snapshot()

	if snapshot.Counters["requests"] != 1 {
		t.Errorf("Snapshot.Counters[requests] = %d, want 1", snapshot.Counters["requests"])
	}

	if snapshot.Gauges["cpu"] != 50.0 {
		t.Errorf("Snapshot.Gauges[cpu] = %f, want 50.0", snapshot.Gauges["cpu"])
	}

	if snapshot.Timers["operation"].Count != 1 {
		t.Errorf("Snapshot.Timers[operation].Count = %d, want 1", snapshot.Timers["operation"].Count)
	}
}

func TestMetricsReset(t *testing.T) {
	m := NewMetrics()

	m.Inc("requests")
	m.SetGauge("cpu", 50.0)
	m.Timer("operation").Record(100 * time.Millisecond)

	m.Reset()

	if m.GetCounter("requests") != 0 {
		t.Errorf("GetCounter(requests) after reset = %d, want 0", m.GetCounter("requests"))
	}
	if m.GetGauge("cpu") != 0 {
		t.Errorf("GetGauge(cpu) after reset = %f, want 0", m.GetGauge("cpu"))
	}
	if m.Timer("operation").Count() != 0 {
		t.Errorf("Timer(operation).Count() after reset = %d, want 0", m.Timer("operation").Count())
	}
}

func TestStopwatch(t *testing.T) {
	sw := NewStopwatch()
	time.Sleep(20 * time.Millisecond)

	elapsed := sw.Elapsed()
	if elapsed < 20*time.Millisecond {
		t.Errorf("Stopwatch.Elapsed() = %v, expected >= 20ms", elapsed)
	}

	lap := sw.Lap()
	if lap < 20*time.Millisecond {
		t.Errorf("Stopwatch.Lap() = %v, expected >= 20ms", lap)
	}

	// After lap, elapsed should be small
	elapsed2 := sw.Elapsed()
	if elapsed2 > 10*time.Millisecond {
		t.Errorf("Stopwatch.Elapsed() after Lap = %v, expected < 10ms", elapsed2)
	}
}

func TestTimerStats(t *testing.T) {
	timer := &Timer{}
	timer.Record(100 * time.Millisecond)
	timer.Record(200 * time.Millisecond)

	stats := timer.Stats()

	if stats.Count != 2 {
		t.Errorf("TimerStats.Count = %d, want 2", stats.Count)
	}
	if stats.Total != 300*time.Millisecond {
		t.Errorf("TimerStats.Total = %v, want 300ms", stats.Total)
	}
	if stats.Min != 100*time.Millisecond {
		t.Errorf("TimerStats.Min = %v, want 100ms", stats.Min)
	}
	if stats.Max != 200*time.Millisecond {
		t.Errorf("TimerStats.Max = %v, want 200ms", stats.Max)
	}
	if stats.Avg != 150*time.Millisecond {
		t.Errorf("TimerStats.Avg = %v, want 150ms", stats.Avg)
	}
}
