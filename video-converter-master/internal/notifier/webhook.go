package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"

	"github.com/darkace1998/video-converter-common/constants"
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
	// webhookMaxAttempts is how many times a delivery is tried before it is
	// recorded as failed (1 = fire-and-forget, the old behavior).
	webhookMaxAttempts = 3
)

// DeliveryRecorder is called once per delivery with its final outcome.
// statusCode is 0 when the request never got a response. May be nil.
type DeliveryRecorder func(event, jobID string, attempts, statusCode int, err error)

// WebhookNotifier handles sending webhooks for job events. Deliveries run on a
// fixed worker pool fed by a bounded queue, so callers never block and the
// number of in-flight requests and goroutines stays capped.
type WebhookNotifier struct {
	webhookURL string
	events     map[string]bool
	secret     string
	recorder   DeliveryRecorder
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
// pool. Returns nil when no webhook URL is configured. secret optionally signs
// every delivery (X-Darkstream-Signature header); recorder optionally receives
// the final outcome of each delivery.
func NewWebhookNotifier(webhookURL string, events []string, secret string, recorder DeliveryRecorder) *WebhookNotifier {
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
		secret:     secret,
		recorder:   recorder,
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

// deliver POSTs the payload to the webhook URL, retrying with exponential
// backoff (up to webhookMaxAttempts) on transport errors or non-2xx responses.
// The final outcome is reported to the recorder. Honors the notifier's context
// so an in-flight request is cancelled if the grace period expires during
// Shutdown.
func (wn *WebhookNotifier) deliver(payload Payload) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "error", err, "job_id", payload.Job.ID)
		wn.record(payload, 0, 0, err)
		return
	}

	deliveryID := fmt.Sprintf("%d-%s", time.Now().UnixNano(), payload.Job.ID)
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= webhookMaxAttempts; attempt++ {
		if attempt > 1 {
			// Backoff: 1s, 4s, ... capped, abortable on shutdown.
			backoff := min(time.Duration(1<<(attempt-2))*time.Second, 30*time.Second)
			timer := time.NewTimer(backoff)
			select {
			case <-wn.ctx.Done():
				timer.Stop()
				wn.record(payload, attempt-1, lastStatus, wn.ctx.Err())
				return
			case <-timer.C:
			}
			slog.Info("Retrying webhook delivery", "event", payload.Event, "job_id", payload.Job.ID, "attempt", attempt, "url", wn.webhookURL)
		}

		req, err := http.NewRequestWithContext(wn.ctx, http.MethodPost, wn.webhookURL, bytes.NewReader(data))
		if err != nil {
			slog.Error("Failed to create webhook request", "error", err)
			wn.record(payload, attempt, 0, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(constants.HeaderWebhookEvent, payload.Event)
		req.Header.Set(constants.HeaderWebhookDelivery, deliveryID)
		req.Header.Set(constants.HeaderWebhookTimestamp, payload.Timestamp.UTC().Format(time.RFC3339Nano))
		if wn.secret != "" {
			mac := hmac.New(sha256.New, []byte(wn.secret))
			mac.Write(data)
			req.Header.Set(constants.HeaderWebhookSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}

		resp, err := wn.client.Do(req)
		if err != nil {
			lastErr, lastStatus = err, 0
			slog.Warn("Webhook delivery attempt failed", "attempt", attempt, "error", err, "url", wn.webhookURL, "job_id", payload.Job.ID)
			continue
		}
		// Drain before closing so the underlying connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			slog.Info("Successfully delivered webhook", "event", payload.Event, "job_id", payload.Job.ID, "attempts", attempt, "status", resp.StatusCode)
			wn.record(payload, attempt, resp.StatusCode, nil)
			return
		}
		lastErr = fmt.Errorf("non-success status %d", resp.StatusCode)
		slog.Warn("Webhook delivered but received non-success status", "attempt", attempt, "status", resp.StatusCode, "url", wn.webhookURL, "job_id", payload.Job.ID)
	}

	slog.Error("Webhook delivery failed after all attempts", "event", payload.Event, "job_id", payload.Job.ID, "attempts", webhookMaxAttempts, "url", wn.webhookURL, "last_status", lastStatus, "error", lastErr)
	wn.record(payload, webhookMaxAttempts, lastStatus, lastErr)
}

// record reports the final delivery outcome to the recorder, if any.
func (wn *WebhookNotifier) record(payload Payload, attempts, statusCode int, err error) {
	if wn.recorder == nil {
		return
	}
	jobID := ""
	if payload.Job != nil {
		jobID = payload.Job.ID
	}
	wn.recorder(payload.Event, jobID, attempts, statusCode, err)
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
