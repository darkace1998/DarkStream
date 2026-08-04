package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

const (
	// webhookQueueSize bounds how many pending notifications may be buffered
	// before new ones are dropped, providing backpressure instead of unbounded
	// goroutine/memory growth under a flood of events.
	webhookQueueSize = 256
	// webhookWorkerCount bounds how many deliveries run concurrently.
	webhookWorkerCount = 4
	// webhookTimeout bounds a single delivery attempt.
	webhookTimeout = 5 * time.Second
)

// WebhookNotifier handles sending webhooks for job events. Deliveries run on a
// fixed worker pool fed by a bounded queue, so callers never block and the
// number of in-flight requests and goroutines stays capped.
type WebhookNotifier struct {
	webhookURL string
	events     map[string]bool
	client     *http.Client

	queue  chan Payload
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.RWMutex
	closed bool
}

// Payload represents the JSON data sent in the webhook request
type Payload struct {
	Event     string      `json:"event"`
	Timestamp time.Time   `json:"timestamp"`
	Job       *models.Job `json:"job"`
}

// NewWebhookNotifier initializes a new WebhookNotifier and starts its worker
// pool. Returns nil when no webhook URL is configured.
func NewWebhookNotifier(webhookURL string, events []string) *WebhookNotifier {
	if webhookURL == "" {
		return nil
	}

	eventMap := make(map[string]bool)
	for _, e := range events {
		eventMap[e] = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	wn := &WebhookNotifier{
		webhookURL: webhookURL,
		events:     eventMap,
		client:     &http.Client{Timeout: webhookTimeout},
		queue:      make(chan Payload, webhookQueueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	wn.wg.Add(webhookWorkerCount)
	for range make([]struct{}, webhookWorkerCount) {
		go wn.worker()
	}

	return wn
}

// worker drains the queue until it is closed, delivering each payload.
func (wn *WebhookNotifier) worker() {
	defer wn.wg.Done()
	for payload := range wn.queue {
		wn.deliver(payload)
	}
}

// Notify enqueues a webhook for the given event and job, if the event is
// enabled. It never blocks the caller: if the queue is full the notification is
// dropped with a warning, and once the notifier is shut down it is a no-op.
func (wn *WebhookNotifier) Notify(event string, job *models.Job) {
	if wn == nil || wn.webhookURL == "" {
		return
	}

	// If the event type is not configured, do not send anything.
	if !wn.events[event] {
		return
	}

	payload := Payload{
		Event:     event,
		Timestamp: time.Now(),
		Job:       job,
	}

	// RLock pairs with Shutdown's Lock so a send can never race the close of the
	// queue channel (which would panic).
	wn.mu.RLock()
	defer wn.mu.RUnlock()
	if wn.closed {
		return
	}
	select {
	case wn.queue <- payload:
	default:
		slog.Warn("Webhook queue full, dropping notification", "event", event, "job_id", job.ID)
	}
}

// deliver performs a single webhook POST, honoring the notifier's context so an
// in-flight request is cancelled if the grace period expires during Shutdown.
func (wn *WebhookNotifier) deliver(payload Payload) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "error", err, "job_id", payload.Job.ID)
		return
	}

	req, err := http.NewRequestWithContext(wn.ctx, http.MethodPost, wn.webhookURL, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("Failed to create webhook request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := wn.client.Do(req)
	if err != nil {
		slog.Error("Failed to deliver webhook", "error", err, "url", wn.webhookURL, "job_id", payload.Job.ID)
		return
	}
	defer func() {
		// Drain before closing so the underlying connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("Webhook delivered but received non-success status", "status", resp.StatusCode, "url", wn.webhookURL, "job_id", payload.Job.ID)
		return
	}

	slog.Info("Successfully delivered webhook", "event", payload.Event, "job_id", payload.Job.ID)
}

// Shutdown stops accepting notifications and waits for queued deliveries to
// finish, or for the provided context to expire — at which point in-flight
// requests are cancelled. Safe to call more than once and on a nil notifier.
func (wn *WebhookNotifier) Shutdown(ctx context.Context) {
	if wn == nil {
		return
	}

	wn.mu.Lock()
	if wn.closed {
		wn.mu.Unlock()
		return
	}
	wn.closed = true
	close(wn.queue)
	wn.mu.Unlock()

	done := make(chan struct{})
	go func() {
		wn.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All queued deliveries finished within the grace period.
	case <-ctx.Done():
		// Grace period expired: cancel in-flight requests and let workers exit.
		wn.cancel()
		<-done
	}
	wn.cancel() // release context resources
}
