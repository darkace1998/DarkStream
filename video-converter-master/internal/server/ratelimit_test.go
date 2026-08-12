package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

// A single shared bucket let high-frequency telemetry starve the calls that keep
// the system running: progress updates exhausted it, then heartbeats and job
// dispatch were rejected and a busy worker was reported offline.
func TestRateLimitClassSeparatesTelemetryFromControl(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/worker/job-progress", rateClassTelemetry},
		{"/api/worker/job-log", rateClassTelemetry},
		{"/api/worker/upload-video", rateClassTransfer},
		{"/api/worker/download-video", rateClassTransfer},
		{"/api/worker/heartbeat", rateClassControl},
		{"/api/worker/next-job", rateClassControl},
		{"/api/worker/job-complete", rateClassControl},
		{"/api/stats", rateClassControl},
	}

	for _, tc := range tests {
		if got := rateLimitClass(tc.path); got != tc.want {
			t.Errorf("rateLimitClass(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// Workers must not share a budget just because they run on the same host or sit
// behind one NAT — otherwise the limit tightens as workers are added.
func TestRateLimitKeyPrefersWorkerIdentity(t *testing.T) {
	withHeader := httptest.NewRequest("POST", "/api/worker/heartbeat", nil)
	withHeader.Header.Set(workerIDHeader, "worker-a")
	withHeader.RemoteAddr = "10.0.0.5:1234"

	sameHostOtherWorker := httptest.NewRequest("POST", "/api/worker/heartbeat", nil)
	sameHostOtherWorker.Header.Set(workerIDHeader, "worker-b")
	sameHostOtherWorker.RemoteAddr = "10.0.0.5:5678"

	keyA := rateLimitKey(withHeader)
	keyB := rateLimitKey(sameHostOtherWorker)

	if keyA == keyB {
		t.Errorf("two workers on one host share the key %q", keyA)
	}
	if keyA != "worker:worker-a" {
		t.Errorf("rateLimitKey = %q, want the worker identity", keyA)
	}
}

// Older workers that only pass the id in the query string still get their own
// budget.
func TestRateLimitKeyFallsBackToQueryThenAddress(t *testing.T) {
	viaQuery := httptest.NewRequest("GET", "/api/worker/next-job?worker_id=worker-c", nil)
	viaQuery.RemoteAddr = "10.0.0.9:1111"
	if got := rateLimitKey(viaQuery); got != "worker:worker-c" {
		t.Errorf("rateLimitKey = %q, want the query worker id", got)
	}

	anonymous := httptest.NewRequest("GET", "/api/stats", nil)
	anonymous.RemoteAddr = "10.0.0.9:2222"
	if got := rateLimitKey(anonymous); got != "ip:10.0.0.9" {
		t.Errorf("rateLimitKey = %q, want the client address", got)
	}
}

// Exhausting one class must leave the others untouched, which is the whole point
// of splitting them.
func TestRateLimiterClassesAreIndependent(t *testing.T) {
	rl := newRateLimiter()
	defer rl.stop()

	const limit = 5
	key := "worker:worker-a"

	// Burn the telemetry budget.
	for i := 0; i < limit*3; i++ {
		rl.allow(key+"|"+rateClassTelemetry, limit, time.Hour)
	}

	if !rl.allow(key+"|"+rateClassControl, limit, time.Hour) {
		t.Error("control plane was rejected after telemetry exhausted its own budget")
	}
}

func TestRateLimiterRejectsOnlyAfterBudgetSpent(t *testing.T) {
	rl := newRateLimiter()
	defer rl.stop()

	const limit = 3
	key := "worker:worker-a|" + rateClassControl

	allowed := 0
	for i := 0; i < limit*2; i++ {
		if rl.allow(key, limit, time.Hour) {
			allowed++
		}
	}

	if allowed != limit {
		t.Errorf("allowed %d requests, want exactly the %d-request budget", allowed, limit)
	}
}
