package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/metrics"
)

// A paused worker must get the same {jobs, count} envelope as every other
// response of the batch endpoint; workers decode it into that shape.
func TestGetNextJobsPausedWorkerEnvelope(t *testing.T) {
	srv := newTestServer(t)

	hb := &models.WorkerHeartbeat{WorkerID: "w1", Hostname: "h", Timestamp: time.Now(), Status: "online"}
	if err := srv.db.UpdateWorkerHeartbeat(hb); err != nil {
		t.Fatal(err)
	}
	if err := srv.db.SetWorkerStatus("w1", "paused"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/worker/next-jobs?worker_id=w1", nil)
	rec := httptest.NewRecorder()
	srv.GetNextJobs(rec, req)

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Count int           `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not a {jobs,count} object: %v (%s)", err, rec.Body.String())
	}
	if len(resp.Jobs) != 0 || resp.Count != 0 {
		t.Errorf("paused worker was handed jobs: %s", rec.Body.String())
	}
}

func TestParseIntRejectsTrailingGarbage(t *testing.T) {
	if _, err := parseInt("12abc"); err == nil {
		t.Error("expected an error for \"12abc\"")
	}
	if n, err := parseInt("12"); err != nil || n != 12 {
		t.Errorf("parseInt(\"12\") = %d, %v", n, err)
	}
}

// uploadTestJob creates a processing job owned by w1 and returns its output path.
func uploadTestJob(t *testing.T, srv *Server, id string) string {
	t.Helper()
	root := srv.masterCfg.Scanner.RootPath
	source := filepath.Join(root, id+".mkv")
	if err := os.WriteFile(source, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(srv.masterCfg.Scanner.OutputBase, id+".mp4")
	job := &models.Job{ID: id, SourcePath: source, OutputPath: output, Status: "pending", CreatedAt: time.Now(), MaxRetries: 3}
	if err := srv.db.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.ClaimNextPendingJob(t.Context(), "w1"); err != nil {
		t.Fatal(err)
	}
	return output
}

func multipartVideo(t *testing.T, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("note", "ignored"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("video", "output.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func TestUploadVideoStreamsAndCompletesJob(t *testing.T) {
	srv := newTestServer(t)
	output := uploadTestJob(t, srv, "upload-ok")

	content := []byte(strings.Repeat("video-bytes", 1000))
	body, contentType := multipartVideo(t, content)
	req := httptest.NewRequest(http.MethodPost, "/api/worker/upload-video?job_id=upload-ok", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.UploadVideo(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("output file mismatch (err=%v, %d bytes)", err, len(got))
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("output file is not readable by other users: %v", info.Mode().Perm())
	}
	job, err := srv.db.GetJobByID("upload-ok")
	if err != nil || job.Status != statusCompleted || job.OutputSize != int64(len(content)) {
		t.Errorf("job not completed correctly: %+v err=%v", job, err)
	}
}

func TestUploadVideoRejectsEmptyFile(t *testing.T) {
	srv := newTestServer(t)
	output := uploadTestJob(t, srv, "upload-empty")

	body, contentType := multipartVideo(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/worker/upload-video?job_id=upload-empty", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.UploadVideo(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty upload, got %d", rec.Code)
	}
	if _, err := os.Stat(output); err == nil {
		t.Error("empty upload created an output file")
	}
	if job, err := srv.db.GetJobByID("upload-empty"); err != nil || job.Status != statusProcessing {
		t.Errorf("job should still be processing: %+v err=%v", job, err)
	}
}

// A Range whose end lies beyond the file means "to the end", not an error.
func TestDownloadVideoClampsRangeEnd(t *testing.T) {
	srv := newTestServer(t)
	uploadTestJob(t, srv, "range")

	req := httptest.NewRequest(http.MethodGet, "/api/worker/download-video?job_id=range", nil)
	req.Header.Set("Range", "bytes=4-9999")
	rec := httptest.NewRecorder()
	srv.DownloadVideo(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "456789" {
		t.Errorf("unexpected range body %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 4-9/10" {
		t.Errorf("unexpected Content-Range %q", got)
	}
}

// Unknown paths must not each become their own metrics label.
func TestMetricsMiddlewareUsesRoutePattern(t *testing.T) {
	srv := newTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	handler := srv.metricsMiddleware(mux)

	for _, path := range []string{"/random-1", "/random-2", "/wp-login.php"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	metricsRec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	out := metricsRec.Body.String()
	if strings.Contains(out, "random-1") || strings.Contains(out, "wp-login") {
		t.Error("raw request paths leaked into metric labels")
	}
	if !strings.Contains(out, `endpoint="other"`) {
		t.Error("expected unmatched paths to be recorded under endpoint=\"other\"")
	}
}

// The upload is stored under the container extension the worker produced, the
// job follows it, and the reported source metadata is saved.
func TestUploadVideoAdjustsExtensionAndStoresMetadata(t *testing.T) {
	srv := newTestServer(t)
	original := uploadTestJob(t, srv, "upload-mkv")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("metadata", `{"source_width":1920,"source_height":1080,"source_video_codec":"hevc","source_duration":12.5}`); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("video", "output.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("matroska-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/worker/upload-video?job_id=upload-mkv", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.UploadVideo(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}

	wantPath := strings.TrimSuffix(original, ".mp4") + ".mkv"
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected output at %s: %v", wantPath, err)
	}
	if _, err := os.Stat(original); err == nil {
		t.Error("a Matroska file was stored under the .mp4 name")
	}

	job, err := srv.db.GetJobByID("upload-mkv")
	if err != nil {
		t.Fatal(err)
	}
	if job.OutputPath != wantPath {
		t.Errorf("job output path = %s, want %s", job.OutputPath, wantPath)
	}
	if job.SourceWidth != 1920 || job.SourceHeight != 1080 || job.SourceVideoCodec != "hevc" || job.SourceDuration != 12.5 {
		t.Errorf("metadata not stored: %+v", job)
	}
	if known, err := srv.db.HasKnownOutputChecksum(job.OutputChecksum); err != nil || !known {
		t.Errorf("output checksum not recorded as a known output (known=%v err=%v)", known, err)
	}
}

func TestOutputPathForUpload(t *testing.T) {
	cases := []struct{ current, upload, want string }{
		{"/o/v.mp4", "output.mkv", "/o/v.mkv"},
		{"/o/w.mp4", "output.mp4", "/o/w.mp4"},
		{"/o/x.MP4", "output.mp4", "/o/x.MP4"},
		{"/o/y.mp4", "output.exe", "/o/y.mp4"},
		{"/o/z.mp4", "", "/o/z.mp4"},
		{"/out/my.movie.mp4", "output.webm", "/out/my.movie.webm"},
	}
	for _, c := range cases {
		if got := outputPathForUpload(c.current, c.upload); got != c.want {
			t.Errorf("outputPathForUpload(%q, %q) = %q, want %q", c.current, c.upload, got, c.want)
		}
	}
}

// Progress from a worker that does not hold the job is refused, which is how
// that worker learns the job was cancelled or reassigned.
func TestJobProgressRejectsForeignOrFinishedJobs(t *testing.T) {
	srv := newTestServer(t)
	uploadTestJob(t, srv, "progress-job") // processing, owned by w1

	post := func(workerID string) int {
		payload := `{"job_id":"progress-job","worker_id":"` + workerID + `","progress":10,"stage":"convert"}`
		req := httptest.NewRequest(http.MethodPost, "/api/worker/job-progress", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		srv.JobProgress(rec, req)
		return rec.Code
	}

	if code := post("w1"); code != http.StatusOK {
		t.Fatalf("owner's progress rejected: %d", code)
	}
	if code := post("someone-else"); code != http.StatusConflict {
		t.Errorf("foreign worker's progress: got %d, want 409", code)
	}

	job, _ := srv.db.GetJobByID("progress-job")
	if updated, err := srv.db.MarkJobCancelled(job.ID, "cancelled", job.StartedAt); err != nil || !updated {
		t.Fatalf("cancel failed: %v", err)
	}
	if code := post("w1"); code != http.StatusConflict {
		t.Errorf("progress for a cancelled job: got %d, want 409", code)
	}
}

func TestWorkerCapacity(t *testing.T) {
	cases := map[string]int{"": 5, "max_jobs=8": 8, "max_jobs=0": 5, "max_jobs=abc": 5, "max_jobs=9999": 64}
	for query, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/worker/next-job?"+query, nil)
		if got := workerCapacity(req); got != want {
			t.Errorf("workerCapacity(%q) = %d, want %d", query, got, want)
		}
	}
}

const regressionAPIKey = "secret-key"

// With an API key configured the HTML pages must not expose job data to
// requests that do not present it.
func TestWebUIHidesDataWithoutAPIKey(t *testing.T) {
	srv := newTestServer(t)
	srv.apiKey = regressionAPIKey
	uploadTestJob(t, srv, "private-job")

	render := func(path string, withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if withCookie {
			req.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: regressionAPIKey})
		}
		rec := httptest.NewRecorder()
		if strings.HasPrefix(path, "/ui/job") {
			srv.handleJobDetailsUI(rec, req)
		} else {
			srv.ServeWebUI(rec, req)
		}
		return rec
	}

	if rec := render("/", false); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "private-job") {
		t.Errorf("dashboard leaked job data without a key (status %d)", rec.Code)
	}
	if rec := render("/", true); !strings.Contains(rec.Body.String(), "private-job") {
		t.Error("dashboard did not render job data for an authorised request")
	}
	if rec := render("/ui/job?id=private-job", false); rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "private-job.mkv") {
		t.Errorf("job page not protected (status %d)", rec.Code)
	}
	if rec := render("/ui/job?id=private-job", true); rec.Code != http.StatusOK {
		t.Errorf("job page rejected an authorised request (status %d)", rec.Code)
	}

	// The cookie must not unlock the API itself.
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.AddCookie(&http.Cookie{Name: uiAuthCookieName, Value: regressionAPIKey})
	rec := httptest.NewRecorder()
	srv.GetStats(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("API accepted the UI cookie as a credential (status %d)", rec.Code)
	}
}
