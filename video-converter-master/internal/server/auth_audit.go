package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// actorContextKey carries the authenticated actor ("master-key",
// "worker:<id>", "stream-token") for audit logging.
type actorContextKey struct{}

// streamTokenTTL is how long a signed SSE stream token stays valid.
const streamTokenTTL = 5 * time.Minute

// Actor identifiers recorded in the audit log.
const (
	actorMasterKey = "master-key"
	actorUnknown   = "unknown"
)

// issueStreamToken creates a short-lived HMAC-signed token for SSE endpoints,
// so the long-lived API key never appears in URLs/access logs.
func (s *Server) issueStreamToken() (string, time.Time) {
	exp := time.Now().Add(streamTokenTTL)
	mac := hmac.New(sha256.New, []byte(s.apiKey))
	_, _ = fmt.Fprintf(mac, "stream:%d", exp.Unix())
	return fmt.Sprintf("%x.%d", mac.Sum(nil), exp.Unix()), exp
}

// validateStreamToken checks a token from issueStreamToken.
func (s *Server) validateStreamToken(tok string) (string, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(s.apiKey))
	_, _ = fmt.Fprintf(mac, "stream:%d", exp)
	expected := fmt.Sprintf("%x.%d", mac.Sum(nil), exp)
	if subtle.ConstantTimeCompare([]byte(tok), []byte(expected)) == 1 {
		return "stream-token", true
	}
	return "", false
}

// isSSEPath reports whether the path is an SSE stream endpoint (these accept
// short-lived stream tokens in addition to the API key).
func isSSEPath(path string) bool {
	return path == "/api/stats/stream" || path == "/api/job/progress/stream"
}

// authenticateAPICall authenticates an API request and returns the actor.
// Accepted credentials: master API key (any route), per-worker token
// (/api/worker/* only), short-lived stream token (SSE endpoints only).
func (s *Server) authenticateAPICall(r *http.Request) (string, bool) {
	if s.apiKey == "" {
		return "anonymous", true
	}

	provided := r.URL.Query().Get("api_key")
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		provided = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if provided != "" {
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.apiKey)) == 1 {
			return actorMasterKey, true
		}
		if strings.HasPrefix(r.URL.Path, "/api/worker/") {
			for workerID, token := range s.workerTokens {
				if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1 {
					return "worker:" + workerID, true
				}
			}
		}
		return "", false
	}

	if isSSEPath(r.URL.Path) {
		if tok := r.URL.Query().Get("token"); tok != "" {
			if actor, ok := s.validateStreamToken(tok); ok {
				return actor, true
			}
		}
	}
	return "", false
}

// sanitizeAuditQuery strips credential-bearing query parameters (api_key,
// token) so the long-lived master key never lands in the audit table.
func sanitizeAuditQuery(rawQuery string) string {
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	vals.Del("api_key")
	vals.Del("token")
	return vals.Encode()
}

// auditRequest records an audit entry for a state-changing request with a
// sanitized "METHOD path?query" detail string.
func (s *Server) auditRequest(r *http.Request, action string) {
	s.audit(r, action, r.Method+" "+r.URL.Path+" "+sanitizeAuditQuery(r.URL.RawQuery))
}

// checkWorkerBinding rejects requests that present a per-worker token for a
// different worker_id than the token was issued for, so one worker cannot
// impersonate another on /api/worker/* routes. Master-key requests are
// unaffected.
func (s *Server) checkWorkerBinding(w http.ResponseWriter, r *http.Request, workerID string) bool {
	actor, _ := r.Context().Value(actorContextKey{}).(string)
	if strings.HasPrefix(actor, "worker:") && workerID != "" && actor != "worker:"+workerID {
		slog.Warn("Worker token presented for mismatched worker_id", "actor", actor, fieldWorkerID, workerID, "path", r.URL.Path, "ip", r.RemoteAddr)
		http.Error(w, "Forbidden: token does not belong to this worker_id", http.StatusForbidden)
		return false
	}
	return true
}

// audit records an audit-log entry for a state-changing request.
func (s *Server) audit(r *http.Request, action, detail string) {
	actor, _ := r.Context().Value(actorContextKey{}).(string)
	if actor == "" {
		actor = actorUnknown
	}
	if err := s.db.RecordAudit(actor, action, detail); err != nil {
		slog.Warn("Failed to record audit entry", "action", action, "error", err)
	}
}

// HandleStreamToken issues a short-lived signed token for SSE endpoints.
// POST /api/stream-token (requires the API key when one is configured).
func (s *Server) HandleStreamToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.apiKey == "" {
		http.Error(w, "No API key configured; stream tokens are not needed", http.StatusBadRequest)
		return
	}
	token, exp := s.issueStreamToken()
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"token":      token,
		"expires_at": exp.UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("Failed to encode stream token", "error", err)
	}
}

// HandleAuditLog returns recent audit entries. GET /api/audit-log?limit=N
func (s *Server) HandleAuditLog(w http.ResponseWriter, r *http.Request) {
	serveJSONList(w, r, "entries", func(limit int) (any, error) {
		return s.db.GetAuditLog(limit)
	})
}

// actorFromContext extracts the actor for logging/audit purposes.
func actorFromContext(ctx context.Context) string {
	if actor, ok := ctx.Value(actorContextKey{}).(string); ok && actor != "" {
		return actor
	}
	return actorUnknown
}
