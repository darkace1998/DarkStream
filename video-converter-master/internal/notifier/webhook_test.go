package notifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

func TestWebhookNotifier_Notify(t *testing.T) {
	var mu sync.Mutex
	var receivedPayload Payload
	var requestCount int

	// Create a test server to receive the webhook
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++

		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			t.Errorf("Failed to decode payload: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Test 1: Event enabled, should send
	notifier := NewWebhookNotifier(ts.URL, []string{"completed", "failed"})
	job := &models.Job{ID: "job123", Status: "completed"}

	notifier.Notify("completed", job)

	// Wait for async request to finish
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}
	if receivedPayload.Event != "completed" {
		t.Errorf("Expected event 'completed', got %s", receivedPayload.Event)
	}
	if receivedPayload.Job == nil || receivedPayload.Job.ID != "job123" {
		t.Errorf("Expected job with ID 'job123', got %v", receivedPayload.Job)
	}
	mu.Unlock()

	// Test 2: Event not enabled, should not send
	notifier.Notify("pending", job)

	// Wait for potential async request
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if requestCount != 1 {
		t.Errorf("Expected request count to remain 1, got %d", requestCount)
	}
	mu.Unlock()

	// Test 3: Nil notifier, should not panic
	var nilNotifier *WebhookNotifier
	nilNotifier.Notify("completed", job) // Should simply return
}
