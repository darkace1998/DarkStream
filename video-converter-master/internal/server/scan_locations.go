package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
	"github.com/darkace1998/video-converter-master/internal/config"
)

// locationIDPattern restricts scan location IDs to safe slugs.
var locationIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// scanLocationRequest is the payload for creating/updating a scan location.
// Durations are given in seconds in the API; YAML config uses Go durations.
type scanLocationRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	RootPath         string   `json:"root_path"`
	OutputBase       string   `json:"output_base"`
	Engines          int      `json:"engines"`
	HashAlgo         string   `json:"hash_algo"`
	HashMode         string   `json:"hash_mode"`
	ScanIntervalSec  int64    `json:"scan_interval_sec"`
	EnableWatch      bool     `json:"enable_watch"`
	VideoExtensions  []string `json:"video_extensions"`
	RecursiveDepth   int      `json:"recursive_depth"`
	MinFileSize      int64    `json:"min_file_size"`
	MaxFileSize      int64    `json:"max_file_size"`
	SkipHiddenFiles  bool     `json:"skip_hidden_files"`
	SkipHiddenDirs   bool     `json:"skip_hidden_dirs"`
	ReplaceSource    bool     `json:"replace_source"`
	DetectDuplicates bool     `json:"detect_duplicates"`
	Priority         *int     `json:"priority,omitempty"`
}

// HandleScanLocations routes the scan-locations API:
//
//	GET    /api/scan-locations          list locations with runtime status
//	POST   /api/scan-locations          create or update (upsert by id); persists to the config file
//	DELETE /api/scan-locations?id=<id>  stop and remove a location
//	POST   /api/scan-locations?scan_now=<id>  trigger an immediate scan
func (s *Server) HandleScanLocations(w http.ResponseWriter, r *http.Request) {
	if s.locMgr == nil {
		http.Error(w, "Scan location management is not configured", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listScanLocations(w, r)
	case http.MethodPost:
		if id := r.URL.Query().Get("scan_now"); id != "" {
			s.scanLocationNow(w, r, id)
			return
		}
		s.upsertScanLocation(w, r)
	case http.MethodDelete:
		s.deleteScanLocation(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listScanLocations(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{"locations": s.locMgr.List()}); err != nil {
		slog.Error("Failed to encode scan locations", "error", err)
	}
}

func (s *Server) upsertScanLocation(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "scan-location.upsert")
	const maxBodySize = 64 << 10
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req scanLocationRequest
	limitBody(w, r)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	loc, verrs := req.toModel()
	if verrs != nil {
		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]any{fieldError: "validation failed", fieldErrors: verrs.Errors}); err != nil {
			slog.Error("Failed to encode validation errors", "error", err)
		}
		return
	}

	// Upsert: update if the id exists, otherwise add.
	existing := s.locMgr.List()
	exists := false
	for _, l := range existing {
		if l.ID == loc.ID {
			exists = true
			break
		}
	}
	var err error
	if exists {
		err = s.locMgr.Update(loc)
	} else {
		err = s.locMgr.Add(loc)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.AddAllowedDirs(loc.RootPath, loc.OutputBase)

	// Persist the full location list to the config file so locations survive
	// restarts. Failure to persist is logged but does not fail the request —
	// the runtime change is already active.
	if s.persister != nil {
		all := s.locMgr.List()
		locs := make([]models.ScanLocation, 0, len(all))
		for _, l := range all {
			locs = append(locs, l.ScanLocation)
		}
		if perr := s.persister.UpdateScanLocations(locs); perr != nil {
			slog.Error("Failed to persist scan locations to config file", "error", perr)
		}
	}

	slog.Info("Scan location saved", "id", loc.ID, "root", loc.RootPath, "engines", loc.Engines, "hash_algo", loc.HashAlgo)
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"location":   loc,
		fieldMessage: "Scan location saved. It is active now and persisted to the config file.",
	}); err != nil {
		slog.Error("Failed to encode scan location response", "error", err)
	}
}

func (s *Server) deleteScanLocation(w http.ResponseWriter, r *http.Request) {
	s.auditRequest(r, "scan-location.delete")
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id query parameter", http.StatusBadRequest)
		return
	}
	if err := s.locMgr.Remove(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if s.persister != nil {
		all := s.locMgr.List()
		locs := make([]models.ScanLocation, 0, len(all))
		for _, l := range all {
			locs = append(locs, l.ScanLocation)
		}
		if perr := s.persister.UpdateScanLocations(locs); perr != nil {
			slog.Error("Failed to persist scan locations to config file", "error", perr)
		}
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "removed": id}); err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
}

func (s *Server) scanLocationNow(w http.ResponseWriter, _ *http.Request, id string) {
	if err := s.locMgr.ScanNow(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "scanning": id}); err != nil {
		slog.Error("Failed to encode response", "error", err)
	}
}

// toModel converts the API request to a ScanLocation and validates it.
func (req *scanLocationRequest) toModel() (models.ScanLocation, *config.ValidationErrors) {
	loc := models.ScanLocation{
		ID:               req.ID,
		Name:             req.Name,
		RootPath:         req.RootPath,
		OutputBase:       req.OutputBase,
		Engines:          req.Engines,
		HashAlgo:         req.HashAlgo,
		HashMode:         req.HashMode,
		ScanInterval:     time.Duration(req.ScanIntervalSec) * time.Second,
		EnableWatch:      req.EnableWatch,
		VideoExtensions:  req.VideoExtensions,
		RecursiveDepth:   req.RecursiveDepth,
		MinFileSize:      req.MinFileSize,
		MaxFileSize:      req.MaxFileSize,
		SkipHiddenFiles:  req.SkipHiddenFiles,
		SkipHiddenDirs:   req.SkipHiddenDirs,
		ReplaceSource:    req.ReplaceSource,
		DetectDuplicates: req.DetectDuplicates,
		Priority:         req.Priority,
	}

	var errs []config.ValidationError
	add := func(field, msg string) {
		errs = append(errs, config.ValidationError{Field: field, Message: msg})
	}

	if !locationIDPattern.MatchString(loc.ID) {
		add("id", "must be 1-64 chars of [a-zA-Z0-9_-]")
	}
	if loc.RootPath == "" {
		add("root_path", "is required")
	} else if info, statErr := os.Stat(loc.RootPath); statErr != nil || !info.IsDir() {
		add("root_path", "must be an existing directory")
	}
	if loc.OutputBase == "" {
		add("output_base", "is required")
	}
	if loc.Engines < 1 || loc.Engines > 64 {
		add("engines", "must be between 1 and 64")
	}
	validateHashSettings(&loc, add)
	validateScanTuning(&loc, add)

	if len(errs) > 0 {
		return loc, &config.ValidationErrors{Errors: errs}
	}
	return loc, nil
}

// validateHashSettings normalizes and validates hash_algo/hash_mode.
func validateHashSettings(loc *models.ScanLocation, add func(field, msg string)) {
	switch loc.HashAlgo {
	case "", utils.HashAlgoSHA256, utils.HashAlgoSHA1:
		if loc.HashAlgo == "" {
			loc.HashAlgo = utils.HashAlgoSHA1
		}
	default:
		add("hash_algo", "must be sha256 or sha1")
	}
	switch loc.HashMode {
	case "", models.HashModeFull, models.HashModeSkip:
		if loc.HashMode == "" {
			loc.HashMode = models.HashModeFull
		}
	default:
		add("hash_mode", "must be full or skip")
	}
}

// validateScanTuning normalizes and validates the remaining tunables.
func validateScanTuning(loc *models.ScanLocation, add func(field, msg string)) {
	if loc.ScanInterval < 0 {
		add("scan_interval_sec", "must be >= 0")
	}
	if len(loc.VideoExtensions) == 0 {
		loc.VideoExtensions = []string{".mp4", ".mkv", ".mov", ".avi", ".flv", ".webm", ".m4v"}
	}
	if loc.RecursiveDepth == 0 {
		loc.RecursiveDepth = -1
	}
	switch {
	case loc.Priority == nil:
		// Omitted priority means the default (normal); an explicit 0 is a
		// valid low priority and must be preserved.
		p := constants.JobPriorityNormal
		loc.Priority = &p
	case *loc.Priority < 0 || *loc.Priority > 10:
		add("priority", "must be between 0 and 10")
	}
	if loc.Name == "" {
		loc.Name = loc.ID
	}
	// skip mode produces no output checksum, so replace_source would re-convert
	// the file on every scan (nothing detects that the output is current).
	if loc.HashMode == models.HashModeSkip && loc.ReplaceSource {
		add("replace_source", "cannot be true when hash_mode=skip (no checksum to detect already-converted output)")
	}
}
