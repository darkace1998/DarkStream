package notifier

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// WebhookNotifier handles sending webhooks for job events
type WebhookNotifier struct {
	webhookURL string
	events     map[string]bool
	client     *http.Client
}

// Payload represents the JSON data sent in the webhook request
type Payload struct {
	Event     string      `json:"event"`
	Timestamp time.Time   `json:"timestamp"`
	Job       *models.Job `json:"job"`
}

// NewWebhookNotifier initializes a new WebhookNotifier
func NewWebhookNotifier(webhookURL string, events []string) *WebhookNotifier {
	if webhookURL == "" {
		return nil
	}

	eventMap := make(map[string]bool)
	for _, e := range events {
		eventMap[e] = true
	}

	return &WebhookNotifier{
		webhookURL: webhookURL,
		events:     eventMap,
		client: &http.Client{
			Timeout: 5 * time.Second, // Reasonable timeout for webhook delivery
		},
	}
}

// Notify sends a webhook for the given event and job, if the event is enabled.
func (wn *WebhookNotifier) Notify(event string, job *models.Job) {
	if wn == nil || wn.webhookURL == "" {
		return
	}

	// Check if this event type is configured to be sent
	if !wn.events[event] { // If events list is empty, it shouldn't send anything
		return
	}

	// Run webhook delivery asynchronously so it doesn't block the caller
	go func(evt string, j *models.Job) {
		payload := Payload{
			Event:     evt,
			Timestamp: time.Now(),
			Job:       j,
		}

		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("Failed to marshal webhook payload", "error", err, "job_id", j.ID)
			return
		}

		req, err := http.NewRequest(http.MethodPost, wn.webhookURL, bytes.NewBuffer(data))
		if err != nil {
			slog.Error("Failed to create webhook request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := wn.client.Do(req)
		if err != nil {
			slog.Error("Failed to deliver webhook", "error", err, "url", wn.webhookURL, "job_id", j.ID)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Error("Webhook delivered but received non-success status", "status", resp.StatusCode, "url", wn.webhookURL, "job_id", j.ID)
			return
		}

		slog.Info("Successfully delivered webhook", "event", evt, "job_id", j.ID)
	}(event, job)
}
