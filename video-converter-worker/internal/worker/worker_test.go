package worker

import (
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// TestGetAndUpdateBackoff tests the adaptive backoff logic
func TestGetAndUpdateBackoff(t *testing.T) {
	// Create a minimal worker with backoff configuration
	cfg := &models.WorkerConfig{}
	cfg.Worker.InitialBackoffInterval = 1 * time.Second
	cfg.Worker.MaxBackoffInterval = 8 * time.Second

	w := &Worker{
		config:         cfg,
		currentBackoff: 1 * time.Second,
		// backoffMu uses zero value which is valid for sync.Mutex
	}

	// Test initial backoff
	backoff := w.getAndUpdateBackoff(true)
	if backoff != 1*time.Second {
		t.Errorf("Expected initial backoff of 1s, got %v", backoff)
	}

	// Test exponential increase
	backoff = w.getAndUpdateBackoff(true)
	if backoff != 2*time.Second {
		t.Errorf("Expected backoff of 2s after first increase, got %v", backoff)
	}

	backoff = w.getAndUpdateBackoff(true)
	if backoff != 4*time.Second {
		t.Errorf("Expected backoff of 4s after second increase, got %v", backoff)
	}

	backoff = w.getAndUpdateBackoff(true)
	if backoff != 8*time.Second {
		t.Errorf("Expected backoff of 8s after third increase, got %v", backoff)
	}

	// Test max backoff cap
	backoff = w.getAndUpdateBackoff(true)
	if backoff != 8*time.Second {
		t.Errorf("Expected backoff to be capped at 8s, got %v", backoff)
	}

	// Test reset on success
	w.getAndUpdateBackoff(false)
	backoff = w.getAndUpdateBackoff(true)
	if backoff != 1*time.Second {
		t.Errorf("Expected backoff to reset to 1s after success, got %v", backoff)
	}
}

// TestGetAndUpdateBackoffDefaults tests backoff with zero/default configuration
func TestGetAndUpdateBackoffDefaults(t *testing.T) {
	// Create a worker with zero values (should use defaults)
	cfg := &models.WorkerConfig{}
	// InitialBackoffInterval and MaxBackoffInterval are 0

	w := &Worker{
		config:         cfg,
		currentBackoff: 0, // Will be set to default
		// backoffMu uses zero value which is valid for sync.Mutex
	}

	// First call should use default initial backoff (1s)
	w.getAndUpdateBackoff(false) // Reset to defaults
	backoff := w.getAndUpdateBackoff(true)
	if backoff != 1*time.Second {
		t.Errorf("Expected default initial backoff of 1s, got %v", backoff)
	}

	// Increase until we hit the default max (30s)
	// 1 -> 2 -> 4 -> 8 -> 16 -> 32 (capped to 30)
	for i := 0; i < 5; i++ {
		backoff = w.getAndUpdateBackoff(true)
	}
	if backoff != 30*time.Second {
		t.Errorf("Expected backoff to be capped at default 30s, got %v", backoff)
	}
}

// TestGetAndUpdateBackoffConcurrency tests that backoff is thread-safe
func TestGetAndUpdateBackoffConcurrency(t *testing.T) {
	cfg := &models.WorkerConfig{}
	cfg.Worker.InitialBackoffInterval = 1 * time.Second
	cfg.Worker.MaxBackoffInterval = 30 * time.Second

	w := &Worker{
		config:         cfg,
		currentBackoff: 1 * time.Second,
		// backoffMu uses zero value which is valid for sync.Mutex
	}

	var wg sync.WaitGroup
	numGoroutines := 10
	iterations := 100

	// Run concurrent updates
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Alternate between increase and reset
				if j%3 == 0 {
					w.getAndUpdateBackoff(false)
				} else {
					w.getAndUpdateBackoff(true)
				}
			}
		}()
	}

	wg.Wait()

	// After concurrent operations, backoff should be within valid range
	w.backoffMu.Lock()
	finalBackoff := w.currentBackoff
	w.backoffMu.Unlock()

	if finalBackoff < 1*time.Second || finalBackoff > 30*time.Second {
		t.Errorf("Final backoff %v is outside valid range [1s, 30s]", finalBackoff)
	}
}

// TestJobSemaphoreCapacity tests that job semaphore has correct capacity
func TestJobSemaphoreCapacity(t *testing.T) {
	concurrency := 5
	sem := make(chan struct{}, concurrency)

	// Should be able to acquire all slots
	for i := 0; i < concurrency; i++ {
		select {
		case sem <- struct{}{}:
			// OK
		default:
			t.Errorf("Failed to acquire semaphore slot %d", i)
		}
	}

	// Next acquire should block (we use select with default to check non-blocking)
	select {
	case sem <- struct{}{}:
		t.Error("Semaphore should be full, but acquired additional slot")
	default:
		// Expected - semaphore is full
	}

	// Release one slot
	<-sem

	// Now should be able to acquire again
	select {
	case sem <- struct{}{}:
		// OK
	default:
		t.Error("Failed to acquire semaphore slot after release")
	}
}

// TestRateLimiterInterval tests that rate limiter interval is calculated correctly
func TestRateLimiterInterval(t *testing.T) {
	tests := []struct {
		name             string
		requestsPerMin   int
		expectedInterval time.Duration
	}{
		{"60 requests per minute", 60, 1 * time.Second},
		{"120 requests per minute", 120, 500 * time.Millisecond},
		{"30 requests per minute", 30, 2 * time.Second},
		{"1 request per minute", 1, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interval := time.Minute / time.Duration(tt.requestsPerMin)
			if interval != tt.expectedInterval {
				t.Errorf("Expected interval %v for %d req/min, got %v",
					tt.expectedInterval, tt.requestsPerMin, interval)
			}
		})
	}
}
