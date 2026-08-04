package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgressReader(t *testing.T) {
	// Create test data
	testData := bytes.Repeat([]byte("test data chunk"), 100)
	totalSize := int64(len(testData))

	// Track progress
	var lastReported int64
	var callCount int32

	progressCallback := func(transferred, _ int64) {
		atomic.AddInt32(&callCount, 1)
		atomic.StoreInt64(&lastReported, transferred)

		if transferred > totalSize {
			t.Errorf("Transferred bytes (%d) exceeds total (%d)", transferred, totalSize)
		}
	}

	// Create progress reader with smaller report interval for testing
	reader := NewProgressReader(bytes.NewReader(testData), totalSize, progressCallback)
	reader.reportInterval = 0

	// Read data
	buf := make([]byte, 256)
	totalRead := int64(0)
	for {
		n, err := reader.Read(buf)
		totalRead += int64(n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error during read: %v", err)
		}
	}

	// Verify all data was read
	if totalRead != totalSize {
		t.Errorf("Expected to read %d bytes, but read %d", totalSize, totalRead)
	}

	// Verify progress was reported
	finalReported := atomic.LoadInt64(&lastReported)
	if finalReported != totalSize {
		t.Errorf("Expected final progress to be %d, got %d", totalSize, finalReported)
	}

	// Verify callback was called multiple times (at least once)
	calls := atomic.LoadInt32(&callCount)
	if calls < 1 {
		t.Error("Progress callback should have been called at least once")
	}
}

func TestProgressReaderWithZeroTotal(t *testing.T) {
	testData := []byte("test")

	// Progress callback that tracks calls
	var callCount int32
	progressCallback := func(_, _ int64) {
		atomic.AddInt32(&callCount, 1)
	}

	reader := NewProgressReader(bytes.NewReader(testData), 0, progressCallback)
	reader.reportInterval = 0

	// Read all data
	_, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	// Progress should still be reported even with zero total
	if atomic.LoadInt32(&callCount) < 1 {
		t.Error("Expected progress callback to be called at least once")
	}
}

func TestProgressReaderNilCallback(t *testing.T) {
	testData := []byte("test data")

	// Create progress reader with nil callback (should not crash)
	reader := NewProgressReader(bytes.NewReader(testData), int64(len(testData)), nil)

	// Read all data - should work without callback
	_, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read with nil callback: %v", err)
	}
}

func TestThrottledReader(t *testing.T) {
	testData := bytes.Repeat([]byte("x"), 10000)
	bytesPerSec := int64(5000) // 5KB/s

	reader := NewThrottledReader(bytes.NewReader(testData), bytesPerSec)

	start := time.Now()
	buf, err := io.ReadAll(reader)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(buf) != len(testData) {
		t.Errorf("Expected to read %d bytes, got %d", len(testData), len(buf))
	}

	// Should take roughly 2 seconds to read 10KB at 5KB/s
	// Allow generous tolerance for scheduling delays and system load
	minExpected := 1000 * time.Millisecond
	maxExpected := 4000 * time.Millisecond

	if elapsed < minExpected {
		t.Errorf("Read completed too quickly: %v (expected at least %v)", elapsed, minExpected)
	}

	if elapsed > maxExpected {
		t.Logf("Warning: Read took longer than expected: %v (expected max %v)", elapsed, maxExpected)
	}
}

// TestSleepWithContext verifies retry backoff aborts promptly when the context
// is cancelled and otherwise waits the full delay.
func TestSleepWithContext(t *testing.T) {
	// Cancelled context returns its error well before the delay elapses.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepWithContext(ctx, 5*time.Second); err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("expected prompt return on cancellation, waited %v", elapsed)
	}

	// A live context waits the full (short) delay and returns nil.
	start = time.Now()
	if err := sleepWithContext(context.Background(), 20*time.Millisecond); err != nil {
		t.Errorf("expected nil after full delay, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("returned too early: %v", elapsed)
	}

	// A nil context falls back to a plain sleep and returns nil. Passing nil is
	// exactly the fallback behavior under test here.
	//nolint:staticcheck // SA1012: deliberately exercising the nil-context fallback path
	if err := sleepWithContext(nil, time.Millisecond); err != nil {
		t.Errorf("expected nil for nil context, got %v", err)
	}
}

// TestDownloadWithoutContentLength verifies a chunked response with no
// Content-Length is accepted and written fully (previously rejected outright).
func TestDownloadWithoutContentLength(t *testing.T) {
	payload := bytes.Repeat([]byte("videodata"), 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server ResponseWriter is not a Flusher")
			return
		}
		// Flushing mid-write forces a chunked response with no Content-Length.
		w.WriteHeader(http.StatusOK)
		half := len(payload) / 2
		_, _ = w.Write(payload[:half])
		fl.Flush()
		_, _ = w.Write(payload[half:])
	}))
	defer srv.Close()

	mc := New(srv.URL, "worker-test", false)
	out := filepath.Join(t.TempDir(), "src.mp4")
	if err := mc.DownloadSourceVideo("job1", out); err != nil {
		t.Fatalf("download failed: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("downloaded %d bytes, want %d", len(got), len(payload))
	}
}
