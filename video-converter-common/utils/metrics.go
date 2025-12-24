package utils

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics provides performance tracking capabilities.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*int64
	gauges   map[string]*float64
	timers   map[string]*Timer
}

// Timer tracks timing information for operations.
type Timer struct {
	mu    sync.RWMutex
	count int64
	total time.Duration
	min   time.Duration
	max   time.Duration
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]*int64),
		gauges:   make(map[string]*float64),
		timers:   make(map[string]*Timer),
	}
}

// Counter returns a counter by name, creating it if necessary.
func (m *Metrics) Counter(name string) *int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.counters[name]; ok {
		return c
	}

	c := new(int64)
	m.counters[name] = c
	return c
}

// Inc increments a counter by 1.
func (m *Metrics) Inc(name string) {
	atomic.AddInt64(m.Counter(name), 1)
}

// Add adds a value to a counter.
func (m *Metrics) Add(name string, delta int64) {
	atomic.AddInt64(m.Counter(name), delta)
}

// GetCounter returns the current value of a counter.
func (m *Metrics) GetCounter(name string) int64 {
	m.mu.RLock()
	c, ok := m.counters[name]
	m.mu.RUnlock()

	if !ok {
		return 0
	}
	return atomic.LoadInt64(c)
}

// SetGauge sets a gauge value.
func (m *Metrics) SetGauge(name string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.gauges[name]; !ok {
		m.gauges[name] = new(float64)
	}
	*m.gauges[name] = value
}

// GetGauge returns the current value of a gauge.
func (m *Metrics) GetGauge(name string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if g, ok := m.gauges[name]; ok {
		return *g
	}
	return 0
}

// Timer returns a timer by name, creating it if necessary.
func (m *Metrics) Timer(name string) *Timer {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t, ok := m.timers[name]; ok {
		return t
	}

	t := &Timer{}
	m.timers[name] = t
	return t
}

// Record records a duration for a timer.
func (t *Timer) Record(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.count++
	t.total += d

	if t.count == 1 || d < t.min {
		t.min = d
	}
	if d > t.max {
		t.max = d
	}
}

// Count returns the number of recorded durations.
func (t *Timer) Count() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
}

// Total returns the total of all recorded durations.
func (t *Timer) Total() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.total
}

// Min returns the minimum recorded duration.
func (t *Timer) Min() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.min
}

// Max returns the maximum recorded duration.
func (t *Timer) Max() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.max
}

// Avg returns the average of all recorded durations.
func (t *Timer) Avg() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.count == 0 {
		return 0
	}
	return t.total / time.Duration(t.count)
}

// TimerStats contains statistics for a timer.
type TimerStats struct {
	Count int64         `json:"count"`
	Total time.Duration `json:"total"`
	Min   time.Duration `json:"min"`
	Max   time.Duration `json:"max"`
	Avg   time.Duration `json:"avg"`
}

// Stats returns the current statistics for a timer.
func (t *Timer) Stats() TimerStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var avg time.Duration
	if t.count > 0 {
		avg = t.total / time.Duration(t.count)
	}

	return TimerStats{
		Count: t.count,
		Total: t.total,
		Min:   t.min,
		Max:   t.max,
		Avg:   avg,
	}
}

// TimeOperation times an operation and records the duration.
func (m *Metrics) TimeOperation(name string, fn func()) {
	start := time.Now()
	fn()
	m.Timer(name).Record(time.Since(start))
}

// TimeOperationWithError times an operation that returns an error.
func (m *Metrics) TimeOperationWithError(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	m.Timer(name).Record(time.Since(start))
	return err
}

// Snapshot contains a snapshot of all metrics.
type Snapshot struct {
	Counters map[string]int64      `json:"counters"`
	Gauges   map[string]float64    `json:"gauges"`
	Timers   map[string]TimerStats `json:"timers"`
}

// Snapshot returns a snapshot of all metrics.
func (m *Metrics) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := Snapshot{
		Counters: make(map[string]int64, len(m.counters)),
		Gauges:   make(map[string]float64, len(m.gauges)),
		Timers:   make(map[string]TimerStats, len(m.timers)),
	}

	for name, c := range m.counters {
		s.Counters[name] = atomic.LoadInt64(c)
	}
	for name, g := range m.gauges {
		s.Gauges[name] = *g
	}
	for name, t := range m.timers {
		s.Timers[name] = t.Stats()
	}

	return s
}

// Reset resets all metrics to their initial values.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.counters {
		atomic.StoreInt64(c, 0)
	}
	for name := range m.gauges {
		*m.gauges[name] = 0
	}
	for name := range m.timers {
		m.timers[name] = &Timer{}
	}
}

// Stopwatch provides a simple way to time operations.
type Stopwatch struct {
	start time.Time
}

// NewStopwatch creates and starts a new Stopwatch.
func NewStopwatch() *Stopwatch {
	return &Stopwatch{start: time.Now()}
}

// Elapsed returns the elapsed time since the stopwatch was started.
func (s *Stopwatch) Elapsed() time.Duration {
	return time.Since(s.start)
}

// Reset restarts the stopwatch.
func (s *Stopwatch) Reset() {
	s.start = time.Now()
}

// Lap returns the elapsed time and resets the stopwatch.
func (s *Stopwatch) Lap() time.Duration {
	elapsed := s.Elapsed()
	s.Reset()
	return elapsed
}
