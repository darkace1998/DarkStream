package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"gopkg.in/yaml.v3"
)

// yamlScanLocation mirrors models.ScanLocation but renders durations as Go
// duration strings ("5m") so the persisted config stays human-editable.
type yamlScanLocation struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	RootPath         string   `yaml:"root_path"`
	OutputBase       string   `yaml:"output_base"`
	Engines          int      `yaml:"engines"`
	HashAlgo         string   `yaml:"hash_algo"`
	ScanInterval     string   `yaml:"scan_interval"`
	EnableWatch      bool     `yaml:"enable_watch"`
	VideoExtensions  []string `yaml:"video_extensions"`
	RecursiveDepth   int      `yaml:"recursive_depth"`
	MinFileSize      int64    `yaml:"min_file_size"`
	MaxFileSize      int64    `yaml:"max_file_size"`
	SkipHiddenFiles  bool     `yaml:"skip_hidden_files"`
	SkipHiddenDirs   bool     `yaml:"skip_hidden_dirs"`
	ReplaceSource    bool     `yaml:"replace_source"`
	DetectDuplicates bool     `yaml:"detect_duplicates"`
	Priority         int      `yaml:"priority"`
}

// UpdateScanLocations persists the full scan-location list under
// scanner.locations in the config file, preserving all other sections.
func (p *Persister) UpdateScanLocations(locations []models.ScanLocation) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	doc := map[string]any{}
	// #nosec G304 - path is the operator-configured config file, not request input
	data, err := os.ReadFile(p.path)
	switch {
	case err != nil && errors.Is(err, os.ErrNotExist):
		// create fresh below
	case err != nil:
		return fmt.Errorf("failed to read config file: %w", err)
	case len(bytes.TrimSpace(data)) > 0:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse existing config file: %w", err)
		}
	}

	yamlLocs := make([]yamlScanLocation, 0, len(locations))
	for _, loc := range locations {
		yamlLocs = append(yamlLocs, yamlScanLocation{
			ID:               loc.ID,
			Name:             loc.Name,
			RootPath:         loc.RootPath,
			OutputBase:       loc.OutputBase,
			Engines:          loc.Engines,
			HashAlgo:         loc.HashAlgo,
			ScanInterval:     durationString(loc.ScanInterval),
			EnableWatch:      loc.EnableWatch,
			VideoExtensions:  loc.VideoExtensions,
			RecursiveDepth:   loc.RecursiveDepth,
			MinFileSize:      loc.MinFileSize,
			MaxFileSize:      loc.MaxFileSize,
			SkipHiddenFiles:  loc.SkipHiddenFiles,
			SkipHiddenDirs:   loc.SkipHiddenDirs,
			ReplaceSource:    loc.ReplaceSource,
			DetectDuplicates: loc.DetectDuplicates,
			Priority:         loc.Priority,
		})
	}

	scannerSection, ok := doc["scanner"].(map[string]any)
	if !ok {
		scannerSection = map[string]any{}
		doc["scanner"] = scannerSection
	}
	scannerSection["locations"] = yamlLocs

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if dir := filepath.Dir(p.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}
	tempPath := p.path + ".tmp"
	if err := os.WriteFile(tempPath, out, 0o600); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tempPath, p.path); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}
	return nil
}

// durationString renders a duration as "5m0s"-style text (or "0s"), which
// yaml.v3 decodes back into time.Duration.
func durationString(d time.Duration) string {
	return d.String()
}

// ServerSettings are the master settings that must persist across restarts.
// They are stored in the config file (config.yaml) and only take effect on
// restart; runtime-editable settings live in active-config.json instead.
type ServerSettings struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	APIKey       string `json:"api_key"`
	TLSCert      string `json:"tls_cert"`
	TLSKey       string `json:"tls_key"`
	DatabasePath string `json:"database_path"`
	LogLevel     string `json:"log_level"`
	LogFormat    string `json:"log_format"`
}

// Persister writes server settings back to the master config file. It edits
// the file as a generic YAML mapping so sections it does not own (scanner,
// conversion, notifications, ...) are preserved untouched.
type Persister struct {
	mu   sync.Mutex
	path string
}

// NewPersister creates a Persister for the given config file path. The file
// does not need to exist yet; Update creates it.
func NewPersister(path string) *Persister {
	return &Persister{path: path}
}

// Path returns the config file path the persister writes to.
func (p *Persister) Path() string {
	return p.path
}

// Update merges s into the config file, creating the file if necessary, and
// writes it back atomically with restrictive permissions (it may contain the
// API key).
func (p *Persister) Update(s ServerSettings) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	doc := map[string]any{}
	// #nosec G304 - path is the operator-configured config file, not request input
	data, err := os.ReadFile(p.path)
	switch {
	case err != nil && errors.Is(err, os.ErrNotExist):
		// create fresh below
	case err != nil:
		return fmt.Errorf("failed to read config file: %w", err)
	case len(bytes.TrimSpace(data)) > 0:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse existing config file: %w", err)
		}
	}

	setSection(doc, "server", map[string]any{
		"host":     s.Host,
		"port":     s.Port,
		"api_key":  s.APIKey,
		"tls_cert": s.TLSCert,
		"tls_key":  s.TLSKey,
	})
	setSection(doc, "database", map[string]any{
		"path": s.DatabasePath,
	})
	setSection(doc, "logging", map[string]any{
		"level":  s.LogLevel,
		"format": s.LogFormat,
	})

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if dir := filepath.Dir(p.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}
	tempPath := p.path + ".tmp"
	if err := os.WriteFile(tempPath, out, 0o600); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tempPath, p.path); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}
	return nil
}

// setSection sets keys on the named top-level mapping of doc, creating the
// mapping if it does not exist or is not a mapping.
func setSection(doc map[string]any, name string, values map[string]any) {
	section, ok := doc[name].(map[string]any)
	if !ok {
		section = map[string]any{}
		doc[name] = section
	}
	maps.Copy(section, values)
}
