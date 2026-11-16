package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// deadMasterURL refuses connections; used for unreachable-server tests.
const deadMasterURL = "http://127.0.0.1:1"

// jobIDOne is the sample job id used by the cancel/priority tests.
const jobIDOne = "job-1"

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// mockMaster returns a server that asserts method+path and replies with body.
func mockMaster(t *testing.T, wantMethod, wantPath, wantQuery string, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod || r.URL.Path != wantPath {
			t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, wantMethod, wantPath)
		}
		if wantQuery != "" && r.URL.RawQuery != wantQuery {
			t.Errorf("got query %q, want %q", r.URL.RawQuery, wantQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
}

func TestRunCancelHitsCancelEndpoint(t *testing.T) {
	srv := mockMaster(t, http.MethodPost, "/api/job/cancel", "job_id=job-1", `{"status":"cancelled"}`)
	defer srv.Close()

	err := runCancel([]string{flagMasterURL, srv.URL, flagJobID, jobIDOne, flagFormat, formatJSON})
	if err != nil {
		t.Fatalf("runCancel: %v", err)
	}
}

func TestRunCancelRequiresJobID(t *testing.T) {
	if err := runCancel([]string{flagMasterURL, deadMasterURL}); err == nil {
		t.Fatal("expected error when --job-id is missing")
	}
}

func TestRunRetryHitsRetryEndpoint(t *testing.T) {
	srv := mockMaster(t, http.MethodPost, "/api/retry", "limit=5", `{"retried": 5}`)
	defer srv.Close()

	err := runRetry([]string{flagMasterURL, srv.URL, flagLimit, "5", flagFormat, formatJSON})
	if err != nil {
		t.Fatalf("runRetry: %v", err)
	}
}

func TestRunRequeueHitsRequeueEndpoint(t *testing.T) {
	srv := mockMaster(t, http.MethodPost, "/api/job/requeue", "job_id=job-9", `{"status":"pending"}`)
	defer srv.Close()

	err := runRequeue([]string{flagMasterURL, srv.URL, flagJobID, "job-9", flagFormat, formatJSON})
	if err != nil {
		t.Fatalf("runRequeue: %v", err)
	}
}

func TestRunRequeueRequiresJobID(t *testing.T) {
	if err := runRequeue([]string{flagMasterURL, deadMasterURL}); err == nil {
		t.Fatal("expected error when --job-id is missing")
	}
}

func TestRunPriorityValidation(t *testing.T) {
	srv := mockMaster(t, http.MethodPost, "/api/job/priority", "job_id=job-1&priority=9", `{"status":"ok"}`)
	defer srv.Close()

	if err := runPriority([]string{flagMasterURL, srv.URL, flagJobID, jobIDOne, flagPriority, "9"}); err != nil {
		t.Fatalf("runPriority: %v", err)
	}
	if err := runPriority([]string{flagMasterURL, srv.URL, flagJobID, jobIDOne, flagPriority, "11"}); err == nil {
		t.Fatal("expected error for priority > 10")
	}
	if err := runPriority([]string{flagMasterURL, srv.URL, flagPriority, "3"}); err == nil {
		t.Fatal("expected error when --job-id is missing")
	}
}

func TestRunSubmitPostsJob(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/job" {
			t.Errorf("got %s %s, want POST /api/job", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"job-new","status":"pending"}`))
	}))
	defer srv.Close()

	err := runSubmit([]string{flagMasterURL, srv.URL, "--source-path", "/videos/in.mp4", "--output-path", "/videos/out.mp4", flagPriority, "7", flagFormat, formatJSON})
	if err != nil {
		t.Fatalf("runSubmit: %v", err)
	}
	if !strings.Contains(gotBody, `"source_path":"/videos/in.mp4"`) || !strings.Contains(gotBody, `"priority":7`) {
		t.Fatalf("unexpected request body: %s", gotBody)
	}

	if err := runSubmit([]string{flagMasterURL, srv.URL}); err == nil {
		t.Fatal("expected error when --source-path is missing")
	}
}

func TestRunDisplayStatsFormats(t *testing.T) {
	srv := mockMaster(t, http.MethodGet, "/api/stats", "", `{"total_jobs":10,"completed_jobs":4,"failed_jobs":1,"pending_jobs":5,"active_workers":2,"total_workers":3}`)
	defer srv.Close()

	for _, format := range []string{formatJSON, "csv", "table"} {
		out := captureStdout(t, func() {
			if err := runDisplayStats(srv.URL, format, false); err != nil {
				t.Fatalf("runDisplayStats(%s): %v", format, err)
			}
		})
		if format == formatJSON && !strings.Contains(out, `"total_jobs"`) {
			t.Fatalf("json output missing stats: %s", out)
		}
	}
}

func TestRunDisplayJobsJSON(t *testing.T) {
	srv := mockMaster(t, http.MethodGet, "/api/jobs", "limit=50&status=failed", `{"jobs":[{"id":"job-1","status":"failed","source_path":"/a.mp4","output_path":"/b.mp4","priority":5}],"total":1}`)
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := runDisplayJobs(srv.URL, "failed", 50, formatJSON); err != nil {
			t.Fatalf("runDisplayJobs: %v", err)
		}
	})
	if !strings.Contains(out, jobIDOne) {
		t.Fatalf("json output missing job: %s", out)
	}
}

func TestRunDisplayStatus(t *testing.T) {
	srv := mockMaster(t, http.MethodGet, "/api/status", "", `{"status":"ok","workers":2,"queue_depth":3}`)
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := runDisplayStatus(srv.URL, formatJSON); err != nil {
			t.Fatalf("runDisplayStatus: %v", err)
		}
	})
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("json output missing status: %s", out)
	}
}

func TestRunDisplayWorkers(t *testing.T) {
	srv := mockMaster(t, http.MethodGet, "/api/workers", "", `{"workers":[{"id":"w1","status":"online","jobs_completed":3}],"total":1}`)
	defer srv.Close()

	out := captureStdout(t, func() {
		if err := runDisplayWorkers(srv.URL, false, formatJSON); err != nil {
			t.Fatalf("runDisplayWorkers: %v", err)
		}
	})
	if !strings.Contains(out, "w1") {
		t.Fatalf("json output missing worker: %s", out)
	}
}

func TestRunCommandsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	if err := runDisplayStats(srv.URL, formatJSON, false); err == nil {
		t.Fatal("expected error on 500 from /api/stats")
	}
	if err := runDisplayJobs(srv.URL, "", 10, formatJSON); err == nil {
		t.Fatal("expected error on 500 from /api/jobs")
	}
	if err := runDisplayStatus(srv.URL, formatJSON); err == nil {
		t.Fatal("expected error on 500 from /api/status")
	}
	if err := runDisplayWorkers(srv.URL, false, formatJSON); err == nil {
		t.Fatal("expected error on 500 from /api/workers")
	}
	if err := runCancel([]string{flagMasterURL, srv.URL, flagJobID, "j"}); err == nil {
		t.Fatal("expected error on 500 from /api/job/cancel")
	}
}

func TestRunCommandsUnreachableServer(t *testing.T) {
	// Port 1 is reserved and refuses connections.
	dead := deadMasterURL
	if err := runDisplayStats(dead, formatJSON, false); err == nil {
		t.Fatal("expected connection error for stats")
	}
	if err := runDisplayJobs(dead, "", 10, formatJSON); err == nil {
		t.Fatal("expected connection error for jobs")
	}
	if err := runSubmit([]string{flagMasterURL, dead, "--source-path", "/a.mp4"}); err == nil {
		t.Fatal("expected connection error for submit")
	}
}
