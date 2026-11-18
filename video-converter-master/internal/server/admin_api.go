package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"gopkg.in/yaml.v3"
)

// ValidateConfig handles validating a configuration file
func (s *Server) ValidateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configType := r.URL.Query().Get("type")
	if configType != "master" && configType != "worker" {
		http.Error(w, "Invalid type parameter. Must be 'master' or 'worker'", http.StatusBadRequest)
		return
	}

	// Read the config from request body, capped so a client cannot force an
	// arbitrarily large allocation. Config files are only a few KB in practice.
	const maxConfigBodySize = 1 << 20 // 1 MiB
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	errors := validateConfigContent(configType, body)

	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		"valid":     len(errors) == 0,
		fieldErrors: errors,
		"type":      configType,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode validation response", "error", err)
		return
	}
}

// parseInt safely parses an integer from string
func parseInt(s string) (int, error) {
	// strconv rejects trailing garbage ("12abc"), which fmt.Sscanf silently accepted.
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("failed to parse integer: %w", err)
	}
	return n, nil
}

// validateConfigContent validates config YAML content
func validateConfigContent(configType string, content []byte) []string {
	var errors []string

	// Basic YAML structure check
	if len(content) == 0 {
		return []string{"Empty configuration"}
	}

	// Type-specific validation
	switch configType {
	case "master":
		var cfg models.MasterConfig
		dec := yaml.NewDecoder(bytes.NewReader(content))
		dec.KnownFields(true)
		err := dec.Decode(&cfg)
		if err != nil {
			errors = append(errors, fmt.Sprintf("YAML parsing error: %v", err))
			return errors
		}

		// Validate required fields
		if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
			errors = append(errors, "Invalid server port (must be 1-65535)")
		}
		if cfg.Scanner.RootPath == "" {
			errors = append(errors, "Scanner root_path is required")
		}
		if len(cfg.Scanner.VideoExtensions) == 0 {
			errors = append(errors, "At least one video extension is required")
		}
		if cfg.Scanner.OutputBase == "" {
			errors = append(errors, "Scanner output_base is required")
		}
		if cfg.Database.Path == "" {
			errors = append(errors, "Database path is required")
		}

	case "worker":
		var cfg models.WorkerConfig
		dec := yaml.NewDecoder(bytes.NewReader(content))
		dec.KnownFields(true)
		err := dec.Decode(&cfg)
		if err != nil {
			errors = append(errors, fmt.Sprintf("YAML parsing error: %v", err))
			return errors
		}

		// Validate required fields
		if cfg.Worker.ID == "" {
			errors = append(errors, "Worker ID is required")
		}
		if cfg.Worker.MasterURL == "" {
			errors = append(errors, "Worker master_url is required")
		}
		if cfg.Worker.Concurrency <= 0 {
			errors = append(errors, "Worker concurrency must be positive")
		}
	}

	return errors
}

// handleConfig routes GET/POST requests to appropriate config handlers
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.GetConfig(w, r)
	case http.MethodPost:
		s.UpdateConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// submitJobRequest represents a manual job submission request
type submitJobRequest struct {
	SourcePath string `json:"source_path"`
	OutputPath string `json:"output_path,omitempty"`
	Priority   *int   `json:"priority,omitempty"`
}

// HandleBackup creates a consistent online backup of the job database
// (VACUUM INTO, safe with WAL while running) and streams it as a download.
// Backups are also written to disk under database.backup_dir.
func (s *Server) HandleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.auditRequest(r, "admin.backup")

	backupDir := s.masterCfg.Database.BackupDir
	if backupDir == "" {
		backupDir = filepath.Dir(s.masterCfg.Database.Path)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		slog.Error("Failed to create backup dir", "dir", backupDir, "error", err)
		http.Error(w, "Failed to create backup directory", http.StatusInternalServerError)
		return
	}
	// Nanosecond suffix: two backups requested within the same second must not
	// collide on the same filename (VACUUM INTO fails if the target exists).
	now := time.Now().UTC()
	name := fmt.Sprintf("jobs-backup-%s-%d.db", now.Format("20060102-150405"), now.UnixNano())
	dest := filepath.Join(backupDir, name)
	if err := s.db.BackupDatabase(dest); err != nil {
		slog.Error("Backup failed", "dest", dest, "error", err)
		http.Error(w, "Backup failed", http.StatusInternalServerError)
		return
	}
	slog.Info("Database backup created", "path", dest)

	// Stream the backup file as a download, then remove the temp copy.
	f, err := os.Open(dest) // #nosec G304 -- dest is a server-generated path inside backupDir
	if err != nil {
		slog.Error("Failed to open backup for download", "path", dest, "error", err)
		http.Error(w, "Failed to read backup", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = f.Close()
		if keep, _ := strconv.ParseBool(r.URL.Query().Get("keep")); !keep {
			_ = os.Remove(dest)
		}
	}()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "Failed to stat backup", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// serveJSONList is the shared shape of the GET list endpoints: method check,
// ?limit= parsing, then {key: items} as JSON.
func serveJSONList(w http.ResponseWriter, r *http.Request, key string, fetch func(limit int) (any, error)) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}
	items, err := fetch(limit)
	if err != nil {
		slog.Error("Failed to read list", "key", key, "error", err)
		http.Error(w, "Failed to read "+key, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{key: items}); err != nil {
		slog.Error("Failed to encode list", "key", key, "error", err)
	}
}

// HandleWebhookDeliveries lists recent webhook delivery outcomes (newest first).
func (s *Server) HandleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	serveJSONList(w, r, "deliveries", func(limit int) (any, error) {
		return s.db.ListWebhookDeliveries(limit)
	})
}
