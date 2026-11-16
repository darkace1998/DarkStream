package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// ServerSettingsRequest is the payload for POST /api/server-settings. These
// settings are persisted to the master config file and require a restart.
type ServerSettingsRequest struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	APIKey       string `json:"api_key"`
	TLSCert      string `json:"tls_cert"`
	TLSKey       string `json:"tls_key"`
	DatabasePath string `json:"database_path"`
	LogLevel     string `json:"log_level"`
	LogFormat    string `json:"log_format"`
}

// SetPersister attaches the config-file persister used by the server-settings
// endpoint. Called once during startup; nil disables persistence (the POST
// handler then answers 503).
func (s *Server) SetPersister(p *config.Persister) {
	s.persister = p
}

// HandleServerSettings routes GET/POST for /api/server-settings.
func (s *Server) HandleServerSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.GetServerSettings(w, r)
	case http.MethodPost:
		s.UpdateServerSettings(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GetServerSettings returns the current effective server settings plus the
// config file they are persisted to.
func (s *Server) GetServerSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.masterCfg
	configFile := ""
	if s.persister != nil {
		configFile = s.persister.Path()
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"host":             cfg.Server.Host,
		"port":             cfg.Server.Port,
		"api_key":          cfg.Server.APIKey,
		"tls_cert":         cfg.Server.TLSCert,
		"tls_key":          cfg.Server.TLSKey,
		"database_path":    cfg.Database.Path,
		"log_level":        cfg.Logging.Level,
		"log_format":       cfg.Logging.Format,
		"config_file":      configFile,
		"restart_required": true,
		fieldMessage:          "Changes to these settings are written to the config file and apply after the master restarts.",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to encode server settings", "error", err)
	}
}

// UpdateServerSettings validates and persists server settings to the config
// file. The running process is not reconfigured; a restart is required.
func (s *Server) UpdateServerSettings(w http.ResponseWriter, r *http.Request) {
	if s.persister == nil {
		http.Error(w, "Server settings persistence is not configured", http.StatusServiceUnavailable)
		return
	}

	const maxBodySize = 64 << 10 // 64 KiB; server settings are tiny
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req ServerSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Merge semantics: empty/zero fields keep the current value from the
	// config file (falling back to the running config when no file is
	// readable yet), so a partial payload cannot silently clear the API key,
	// TLS paths or any other setting. Clearing a setting requires editing the
	// config file.
	current := s.masterCfg
	if loaded, lerr := config.LoadMasterConfig(s.persister.Path()); lerr == nil {
		current = loaded
	}
	merged := mergedWithCurrent(current, req)
	if validationErrs := validateServerSettings(merged); validationErrs != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error":     "validation failed",
			fieldErrors: validationErrs.Errors,
		}); err != nil {
			slog.Error("Failed to encode validation errors", "error", err)
		}
		return
	}

	settings := config.ServerSettings{
		Host:         merged.Host,
		Port:         merged.Port,
		APIKey:       merged.APIKey,
		TLSCert:      merged.TLSCert,
		TLSKey:       merged.TLSKey,
		DatabasePath: merged.DatabasePath,
		LogLevel:     merged.LogLevel,
		LogFormat:    merged.LogFormat,
	}
	if err := s.persister.Update(settings); err != nil {
		slog.Error("Failed to persist server settings", "error", err)
		http.Error(w, "Failed to persist settings to config file", http.StatusInternalServerError)
		return
	}

	slog.Info("Server settings persisted to config file (restart required to apply)",
		"path", s.persister.Path(), "host", merged.Host, "port", merged.Port)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"ok":               true,
		"persisted_to":     s.persister.Path(),
		"restart_required": true,
		fieldMessage:          "Settings saved to the config file. Restart the master to apply them.",
	}); err != nil {
		slog.Error("Failed to encode server settings response", "error", err)
	}
}

// mergedWithCurrent fills empty/zero fields of the request with the current
// effective values from cfg, so a partial update keeps existing settings
// instead of wiping them.
func mergedWithCurrent(cfg *models.MasterConfig, req ServerSettingsRequest) ServerSettingsRequest {
	if req.Host == "" {
		req.Host = cfg.Server.Host
	}
	if req.Port == 0 {
		req.Port = cfg.Server.Port
	}
	if req.APIKey == "" {
		req.APIKey = cfg.Server.APIKey
	}
	if req.TLSCert == "" {
		req.TLSCert = cfg.Server.TLSCert
	}
	if req.TLSKey == "" {
		req.TLSKey = cfg.Server.TLSKey
	}
	if req.DatabasePath == "" {
		req.DatabasePath = cfg.Database.Path
	}
	if req.LogLevel == "" {
		req.LogLevel = cfg.Logging.Level
	}
	if req.LogFormat == "" {
		req.LogFormat = cfg.Logging.Format
	}
	return req
}

// validateServerSettings checks server settings before they are persisted.
func validateServerSettings(req ServerSettingsRequest) *config.ValidationErrors {
	var errs []config.ValidationError

	if req.Host == "" || strings.ContainsAny(req.Host, " \t\n") {
		errs = append(errs, config.ValidationError{Field: "server.host", Message: "must be a non-empty host or IP without whitespace"})
	}
	if req.Port < 1 || req.Port > 65535 {
		errs = append(errs, config.ValidationError{Field: "server.port", Message: "must be between 1 and 65535"})
	}
	if (req.TLSCert == "") != (req.TLSKey == "") {
		errs = append(errs, config.ValidationError{Field: "server.tls", Message: "tls_cert and tls_key must be set together"})
	}
	if req.DatabasePath == "" {
		errs = append(errs, config.ValidationError{Field: "database.path", Message: "must not be empty"})
	}
	switch req.LogLevel {
	case "", constants.LogLevelDebug, constants.LogLevelInfo, constants.LogLevelWarn, constants.LogLevelError:
	default:
		errs = append(errs, config.ValidationError{Field: "logging.level", Message: "must be one of: debug, info, warn, error"})
	}
	switch req.LogFormat {
	case "", constants.LogFormatJSON, constants.LogFormatText:
	default:
		errs = append(errs, config.ValidationError{Field: "logging.format", Message: "must be one of: json, text"})
	}

	if len(errs) > 0 {
		return &config.ValidationErrors{Errors: errs}
	}
	return nil
}
