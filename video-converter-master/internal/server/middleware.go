package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/utils"

	"encoding/json"
)

func newRateLimiter() *rateLimiter {
	rl := &rateLimiter{
		requestCounts: make(map[string]*bucketState),
		cleanupTicker: time.NewTicker(5 * time.Minute),
		stopCh:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

func (rl *rateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, state := range rl.requestCounts {
				// Remove entries not accessed in last 10 minutes
				if now.Sub(state.lastRefill) > 10*time.Minute {
					delete(rl.requestCounts, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *rateLimiter) allow(ip string, maxTokens int, refillRate time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	state, exists := rl.requestCounts[ip]

	if !exists {
		state = &bucketState{
			tokens:     maxTokens - 1,
			lastRefill: now,
		}
		rl.requestCounts[ip] = state
		return true
	}

	// Refill tokens based on time elapsed
	elapsed := now.Sub(state.lastRefill)
	tokensToAdd := int(elapsed / refillRate)

	if tokensToAdd > 0 {
		state.tokens += tokensToAdd
		if state.tokens > maxTokens {
			state.tokens = maxTokens
		}
		state.lastRefill = now
	}

	// Check if request allowed
	if state.tokens > 0 {
		state.tokens--
		return true
	}

	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

func (rl *rateLimiter) stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
		rl.cleanupTicker.Stop()
	})
}

// maxJSONBodyBytes caps every JSON request body so endpoints (which may be
// unauthenticated in the default no-api_key configuration) cannot be
// memory-exhausted by huge payloads.
const maxJSONBodyBytes int64 = 1 << 20 // 1 MiB

// limitBody caps a request body at maxJSONBodyBytes. Call before decoding r.Body.
func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
}

// rateLimitMiddleware applies rate limiting to endpoints
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		// Apply rate limit per IP. A single worker legitimately issues job polls,
		// heartbeats and per-job progress reports for every concurrent job, and
		// several workers may share one address behind NAT, so the budget has to
		// leave room for that; it only exists to stop runaway clients.
		if !s.rateLimiter.allow(ip, rateLimitBurst, time.Minute/rateLimitPerMinute) {
			slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
			w.Header().Set("Retry-After", "60")
			w.Header().Set("Content-Type", contentTypeJSON)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{fieldError: "rate limit exceeded"})
			return
		}

		next(w, r)
	}
}

// authMiddleware validates API key for worker endpoints
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.authenticateAPICall(r)
		if !ok {
			if r.Header.Get("Authorization") == "" && r.URL.Query().Get("api_key") == "" && r.URL.Query().Get("token") == "" {
				slog.Warn("Missing credentials", "path", r.URL.Path, "ip", r.RemoteAddr)
				http.Error(w, "Unauthorized: Missing credentials", http.StatusUnauthorized)
				return
			}
			slog.Warn("Invalid credentials", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid credentials", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey{}, true)
		ctx = context.WithValue(ctx, actorContextKey{}, actor)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) authenticatedRequest(r *http.Request) bool {
	v, ok := r.Context().Value(authContextKey{}).(bool)
	return ok && v
}

func (s *Server) authenticateRequest(r *http.Request) bool {
	if s.apiKey == "" {
		return true
	}

	var providedKey string
	authHeader := r.Header.Get("Authorization")
	const bearerPrefix = "Bearer "

	if authHeader != "" && strings.HasPrefix(authHeader, bearerPrefix) {
		providedKey = strings.TrimPrefix(authHeader, bearerPrefix)
	} else if isSSEPath(r.URL.Path) {
		// Query-parameter fallback only for EventSource/SSE connections,
		// which cannot set headers; never on regular routes.
		providedKey = r.URL.Query().Get("api_key")
	}

	if providedKey == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(providedKey), []byte(s.apiKey)) == 1
}

func (s *Server) requireAPIKeyAuth(w http.ResponseWriter, r *http.Request) bool {
	// Already authenticated by authMiddleware (master key, per-worker token,
	// or a short-lived stream token on SSE paths).
	if r.Context().Value(authContextKey{}) == true {
		return true
	}
	if s.authenticateRequest(r) {
		return true
	}

	slog.Warn("Unauthorized request", "path", r.URL.Path, "ip", r.RemoteAddr)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return false
}

// correlationMiddleware adds a correlation ID to each request for tracing
func (s *Server) correlationMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if correlation ID is already in the request header
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = utils.GenerateCorrelationID()
		}

		// Add correlation ID to response header
		w.Header().Set("X-Correlation-ID", correlationID)

		// Add correlation ID to request context
		ctx := utils.ContextWithCorrelationID(r.Context(), correlationID)
		r = r.WithContext(ctx)

		next(w, r)
	}
}

// updateQueueDepthMetric updates the queue depth metric
func (s *Server) updateQueueDepthMetric() {
	count, err := s.db.CountPendingJobs()
	if err == nil {
		s.metrics.SetQueueDepth(float64(count))
	}

	// Re-sync the in-progress gauge from the database. It is otherwise only
	// adjusted by start/finish events, so jobs that leave "processing" another
	// way (cancelled, requeued, reclaimed from an offline worker) would make it
	// drift upwards forever.
	if stats, statsErr := s.db.GetJobStats(); statsErr == nil {
		processing, _ := stats[statusProcessing].(int)
		s.metrics.SetJobsInProgress(processing)
	}
}

// RefreshQueueDepth updates the queue depth metric from the database.
func (s *Server) RefreshQueueDepth() {
	s.updateQueueDepthMetric()
}

// UpdateWorkerCounts updates the worker count metrics.
func (s *Server) UpdateWorkerCounts(total, active int) {
	s.metrics.SetWorkerCounts(total, active)
}

// RecordJobRetry increments the retry counter metric.
func (s *Server) RecordJobRetry(reason string) {
	s.metrics.RecordJobRetry(reason)
}

// RemoveWorkerMetrics drops a worker's per-worker metric series. Called when a
// worker goes offline so restarted/churned workers (which get fresh IDs) do not
// leak Prometheus cardinality for the process lifetime.
func (s *Server) RemoveWorkerMetrics(workerID string) {
	if s.metrics != nil {
		s.metrics.RemoveWorkerHeartbeat(workerID)
	}
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *metricsResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *metricsResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rw := &metricsResponseWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		// Label by the matched route, not the raw path: every distinct label
		// value is a new Prometheus series kept for the life of the process, so
		// raw paths would let any client (or scanner bot) grow memory unbounded.
		endpoint := r.Pattern
		if endpoint == "" || (endpoint == "/" && r.URL.Path != "/") {
			endpoint = "other"
		}
		s.metrics.RecordAPIRequest(endpoint, r.Method, fmt.Sprintf("%d", statusCode), time.Since(start).Seconds())
	})
}
