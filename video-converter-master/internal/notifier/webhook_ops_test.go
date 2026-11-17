package notifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
)

type recordedDelivery struct {
	event      string
	jobID      string
	attempts   int
	statusCode int
	err        error
}

// deliverSync runs one delivery synchronously and returns the recorded outcome.
func deliverSync(t *testing.T, wn *WebhookNotifier, payload Payload) recordedDelivery {
	t.Helper()
	recCh := make(chan recordedDelivery, 1)
	wn.recorder = func(event, jobID string, attempts, statusCode int, err error) {
		recCh <- recordedDelivery{event, jobID, attempts, statusCode, err}
	}
	wn.deliver(payload)
	select {
	case rec := <-recCh:
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("recorder was not called")
		return recordedDelivery{}
	}
}

func TestWebhookRetryThenSuccess(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		n := requestCount
		mu.Unlock()
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError) // fail once, then succeed
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wn := NewWebhookNotifier(ts.URL, []string{constants.JobStatusCompleted}, "", nil)
	defer wn.Shutdown(t.Context())

	payload := Payload{Event: constants.JobStatusCompleted, Timestamp: time.Now(), Job: &models.Job{ID: "job-retry"}}
	rec := deliverSync(t, wn, payload)

	if rec.attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", rec.attempts)
	}
	if rec.statusCode != http.StatusOK || rec.err != nil {
		t.Errorf("Expected final success (200, nil), got status=%d err=%v", rec.statusCode, rec.err)
	}
	if rec.event != constants.JobStatusCompleted || rec.jobID != "job-retry" {
		t.Errorf("Recorder got wrong identity: %+v", rec)
	}
	mu.Lock()
	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests, got %d", requestCount)
	}
	mu.Unlock()
}

func TestWebhookAllAttemptsFailRecorded(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()

	wn := NewWebhookNotifier(ts.URL, []string{constants.JobStatusFailed}, "", nil)
	defer wn.Shutdown(t.Context())

	payload := Payload{Event: constants.JobStatusFailed, Timestamp: time.Now(), Job: &models.Job{ID: "job-fail"}}
	rec := deliverSync(t, wn, payload)

	if rec.attempts != webhookMaxAttempts {
		t.Errorf("Expected %d attempts, got %d", webhookMaxAttempts, rec.attempts)
	}
	if rec.statusCode != http.StatusBadGateway {
		t.Errorf("Expected final status 502, got %d", rec.statusCode)
	}
	if rec.err == nil {
		t.Error("Expected a non-nil error for the failed delivery")
	}
	mu.Lock()
	if requestCount != webhookMaxAttempts {
		t.Errorf("Expected %d HTTP requests, got %d", webhookMaxAttempts, requestCount)
	}
	mu.Unlock()
}

func TestWebhookSignatureAndHeaders(t *testing.T) {
	const secret = "test-secret-123"
	var mu sync.Mutex
	var gotBody []byte
	var gotSig, gotEvent, gotDelivery, gotTS string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get(constants.HeaderWebhookSignature)
		gotEvent = r.Header.Get(constants.HeaderWebhookEvent)
		gotDelivery = r.Header.Get(constants.HeaderWebhookDelivery)
		gotTS = r.Header.Get(constants.HeaderWebhookTimestamp)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wn := NewWebhookNotifier(ts.URL, []string{constants.JobStatusCompleted}, secret, nil)
	defer wn.Shutdown(t.Context())

	job := &models.Job{ID: "job-sig", Status: constants.JobStatusCompleted}
	payload := Payload{Event: constants.JobStatusCompleted, Timestamp: time.Now(), Job: job}
	wn.deliver(payload)

	mu.Lock()
	defer mu.Unlock()

	if gotEvent != constants.JobStatusCompleted {
		t.Errorf("X-DarkStream-Event = %q", gotEvent)
	}
	if gotDelivery == "" {
		t.Error("X-DarkStream-Delivery missing")
	}
	if gotTS == "" {
		t.Error("X-DarkStream-Timestamp missing")
	}
	if gotSig == "" {
		t.Fatal("X-DarkStream-Signature missing")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("Signature mismatch:\n got %s\nwant %s", gotSig, want)
	}
	// The body must still be the documented JSON payload.
	var decoded Payload
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body is not a JSON payload: %v", err)
	}
	if decoded.Job == nil || decoded.Job.ID != "job-sig" {
		t.Errorf("Payload job mismatch: %+v", decoded)
	}
}

func TestWebhookNoSignatureWithoutSecret(t *testing.T) {
	var mu sync.Mutex
	var gotSig string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSig = r.Header.Get(constants.HeaderWebhookSignature)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	wn := NewWebhookNotifier(ts.URL, []string{constants.JobStatusCompleted}, "", nil)
	defer wn.Shutdown(t.Context())
	wn.deliver(Payload{Event: constants.JobStatusCompleted, Timestamp: time.Now(), Job: &models.Job{ID: "job-nosig"}})

	mu.Lock()
	defer mu.Unlock()
	if gotSig != "" {
		t.Errorf("Signature header must be absent without a secret, got %q", gotSig)
	}
}

func TestWebhookTransportErrorRecorded(t *testing.T) {
	// Nothing listens on this port: every attempt fails at the transport layer.
	wn := NewWebhookNotifier("http://127.0.0.1:1/hook", []string{constants.JobStatusFailed}, "", nil)
	defer wn.Shutdown(t.Context())

	rec := deliverSync(t, wn, Payload{Event: constants.JobStatusFailed, Timestamp: time.Now(), Job: &models.Job{ID: "job-down"}})
	if rec.attempts != webhookMaxAttempts || rec.statusCode != 0 {
		t.Errorf("Expected attempts=%d status=0, got attempts=%d status=%d", webhookMaxAttempts, rec.attempts, rec.statusCode)
	}
	if rec.err == nil {
		t.Error("Expected transport error to be recorded")
	} else {
		t.Logf("transport error recorded: %v", rec.err)
	}
}
