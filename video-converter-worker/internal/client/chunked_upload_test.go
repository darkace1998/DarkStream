package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeChunkMaster emulates POST /api/worker/upload-chunk: it writes chunks at
// their offsets into a buffer and reports completion once [0,total) is covered.
type fakeChunkMaster struct {
	mu       sync.Mutex
	buf      []byte
	received map[int64]int64
	total    int64
	status   int // status code to always return (e.g. 404 for fallback tests)
}

func newFakeChunkMaster(total int64) *fakeChunkMaster {
	return &fakeChunkMaster{
		buf:      make([]byte, total),
		received: map[int64]int64{},
		total:    total,
	}
}

func (f *fakeChunkMaster) handler(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	total, _ := strconv.ParseInt(r.URL.Query().Get("total_size"), 10, 64)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	copy(f.buf[offset:], body)
	if _, dup := f.received[offset]; !dup {
		f.received[offset] = int64(len(body))
	}
	var got int64
	covered := int64(0)
	for off, ln := range f.received {
		if off != covered {
			// gap: not complete
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"received": sum(f.received), "complete": false}); err != nil {
				t.Errorf("encode: %v", err)
			}
			return
		}
		covered += ln
	}
	got = covered
	complete := got == total
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"received": got, "complete": complete}); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func sum(m map[int64]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}

func TestUploadConvertedVideoChunkedAssemblesFile(t *testing.T) {
	// Deterministic pseudo-random content, larger than several chunk sizes.
	content := make([]byte, 250_000)
	for i := range content {
		content[i] = byte(i * 31 % 251)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	fake := newFakeChunkMaster(int64(len(content)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fake.handler(t, w, r) }))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	mc.SetChunkSize(64_000) // 4 chunks: 64k, 64k, 64k, 58k

	var progressCalls int
	var lastTransferred, lastTotal int64
	err := mc.UploadConvertedVideoChunked("job-1", path, "", func(transferred, total int64) {
		progressCalls++
		lastTransferred, lastTotal = transferred, total
	})
	if err != nil {
		t.Fatalf("chunked upload failed: %v", err)
	}
	if !bytes.Equal(fake.buf, content) {
		t.Fatalf("assembled file differs from original")
	}
	if progressCalls == 0 {
		t.Fatalf("progress callback never invoked")
	}
	if lastTotal != int64(len(content)) {
		t.Fatalf("progress total = %d, want %d", lastTotal, len(content))
	}
	if lastTransferred != int64(len(content)) {
		t.Fatalf("final transferred = %d, want %d", lastTransferred, len(content))
	}
}

func TestUploadConvertedVideoChunkedUnsupportedFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(path, []byte("converted"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // old master without the endpoint
	}))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	mc.SetChunkSize(4)

	err := mc.UploadConvertedVideoChunked("job-2", path, "", nil)
	if !errors.Is(err, ErrChunkedUploadUnsupported) {
		t.Fatalf("expected ErrChunkedUploadUnsupported, got %v", err)
	}
}

func TestUploadConvertedVideoChunkedEmptyFileRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mp4")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	err := mc.UploadConvertedVideoChunked("job-3", path, "", nil)
	if err == nil || errors.Is(err, ErrChunkedUploadUnsupported) {
		t.Fatalf("expected a refusal error for empty file, got %v", err)
	}
}

func TestUploadConvertedVideoChunkedRetriesTransientError(t *testing.T) {
	content := []byte("converted-video-bytes")
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	// Fail the first chunk request with a 500, then behave like a real master.
	fake := newFakeChunkMaster(int64(len(content)))
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		shouldFail := calls == 1
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fake.handler(t, w, r)
	}))
	defer srv.Close()

	mc := New(srv.URL, "worker-1", false)
	mc.SetChunkSize(int64(len(content))) // single chunk

	err := mc.UploadConvertedVideoChunked("job-4", path, "", nil)
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if !bytes.Equal(fake.buf, content) {
		t.Fatalf("assembled file differs after retry")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected at least one retry, got %d calls", calls)
	}
}

// TestThrottledReaderEnforcesBandwidth verifies the token bucket actually
// slows reads down: 1.5 MiB at 1 MiB/s must take noticeably longer than an
// unlimited read of the same data.
func TestThrottledReaderEnforcesBandwidth(t *testing.T) {
	data := make([]byte, 1536*1024) // 1.5 MiB

	// Unlimited control: should be effectively instant.
	start := time.Now()
	unlimited := NewThrottledReader(bytes.NewReader(data), 1<<62)
	if _, err := io.Copy(io.Discard, unlimited); err != nil {
		t.Fatal(err)
	}
	unlimitedElapsed := time.Since(start)

	// 1 MiB/s: the first 1 MiB is free (initial tokens), the remaining 512 KiB
	// requires refills. Each starved Read sleeps up to ThrottleMaxWaitDuration.
	start = time.Now()
	throttled := NewThrottledReader(bytes.NewReader(data), 1<<20)
	n, err := io.Copy(io.Discard, throttled)
	if err != nil {
		t.Fatal(err)
	}
	throttledElapsed := time.Since(start)

	if n != int64(len(data)) {
		t.Fatalf("read %d bytes, want %d", n, len(data))
	}
	// 512 KiB at 1 MiB/s = 0.5 s theoretical minimum.
	if throttledElapsed < 300*time.Millisecond {
		t.Fatalf("throttled read too fast: %v (unlimited was %v)", throttledElapsed, unlimitedElapsed)
	}
	if unlimitedElapsed > throttledElapsed {
		t.Fatalf("unlimited read (%v) slower than throttled (%v)?", unlimitedElapsed, throttledElapsed)
	}
	t.Logf("unlimited=%v throttled=%v", unlimitedElapsed, throttledElapsed)
}

// TestThrottledReaderDeliversAllBytes checks correctness under a tight limit.
func TestThrottledReaderDeliversAllBytes(t *testing.T) {
	data := []byte("hello darkstream throttle")
	tr := NewThrottledReader(bytes.NewReader(data), 1<<40)
	out, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("got %q, want %q", out, data)
	}
}
