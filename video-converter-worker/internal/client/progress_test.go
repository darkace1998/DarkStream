package client

import (
	"bytes"
	"errors"
	"io"
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
		
		if transferred > total {
			t.Errorf("Transferred bytes (%d) exceeds total (%d)", transferred, total)
		}
	}
	
	// Create progress reader with smaller report interval for testing
	reader := NewProgressReader(bytes.NewReader(testData), totalSize, progressCallback)
	reader.reportInterval = 10 * time.Millisecond

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
		// Sleep a bit to allow progress reporting
		time.Sleep(15 * time.Millisecond)
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
	reader.reportInterval = 1 * time.Millisecond

	// Read all data
	_, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	// Progress should still be reported even with zero total
	time.Sleep(10 * time.Millisecond)
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
