// Package client provides HTTP client for communicating with the master coordinator.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ConfigFetcher handles fetching and caching configuration from the master
type ConfigFetcher struct {
	baseURL       string
	client        *http.Client
	mu            sync.RWMutex
	cachedConfig  *models.ConversionSettings
	lastFetchTime time.Time
	refreshInterval time.Duration
}

// NewConfigFetcher creates a new ConfigFetcher instance
func NewConfigFetcher(baseURL string) *ConfigFetcher {
	return &ConfigFetcher{
		baseURL:         baseURL,
		client:          &http.Client{Timeout: 30 * time.Second},
		refreshInterval: 30 * time.Second,
	}
}

// FetchConfig fetches the current configuration from the master
// It returns the cached config if available and still fresh
func (cf *ConfigFetcher) FetchConfig() (*models.ConversionSettings, error) {
	cf.mu.RLock()
	if cf.cachedConfig != nil && time.Since(cf.lastFetchTime) < cf.refreshInterval {
		cfg := *cf.cachedConfig
		cf.mu.RUnlock()
		return &cfg, nil
	}
	cf.mu.RUnlock()

	// Fetch fresh config
	return cf.fetchFromMaster()
}

// GetCachedConfig returns the cached configuration without fetching
// Returns nil if no configuration is cached
func (cf *ConfigFetcher) GetCachedConfig() *models.ConversionSettings {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	if cf.cachedConfig == nil {
		return nil
	}
	cfg := *cf.cachedConfig
	return &cfg
}

// fetchFromMaster fetches configuration from the master with retry logic
func (cf *ConfigFetcher) fetchFromMaster() (*models.ConversionSettings, error) {
	maxRetries := 3
	baseDelay := 2 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<(attempt-1))
			slog.Info("Retrying config fetch", "attempt", attempt+1, "delay", delay)
			time.Sleep(delay)
		}

		cfg, err := cf.fetchConfigAttempt()
		if err == nil {
			cf.mu.Lock()
			cf.cachedConfig = cfg
			cf.lastFetchTime = time.Now()
			cf.mu.Unlock()
			return cfg, nil
		}

		lastErr = err
		slog.Warn("Config fetch attempt failed", "attempt", attempt+1, "error", err)
	}

	// If we have a cached config, return it with a warning
	cf.mu.RLock()
	if cf.cachedConfig != nil {
		cfg := *cf.cachedConfig
		cf.mu.RUnlock()
		slog.Warn("Using cached config after fetch failure", "error", lastErr)
		return &cfg, nil
	}
	cf.mu.RUnlock()

	return nil, fmt.Errorf("failed to fetch config after %d attempts: %w", maxRetries, lastErr)
}

// fetchConfigAttempt performs a single config fetch attempt
func (cf *ConfigFetcher) fetchConfigAttempt() (*models.ConversionSettings, error) {
	url := fmt.Sprintf("%s/api/config", cf.baseURL)

	resp, err := cf.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request config: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse the ActiveConfig format from master
	var activeConfig struct {
		Video struct {
			Resolution string `json:"resolution"`
			Codec      string `json:"codec"`
			Bitrate    string `json:"bitrate"`
			Preset     string `json:"preset"`
		} `json:"video"`
		Audio struct {
			Codec   string `json:"codec"`
			Bitrate string `json:"bitrate"`
		} `json:"audio"`
		Output struct {
			Format string `json:"format"`
		} `json:"output"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&activeConfig); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// Convert to ConversionSettings
	cfg := &models.ConversionSettings{
		TargetResolution: activeConfig.Video.Resolution,
		Codec:            activeConfig.Video.Codec,
		Bitrate:          activeConfig.Video.Bitrate,
		Preset:           activeConfig.Video.Preset,
		AudioCodec:       activeConfig.Audio.Codec,
		AudioBitrate:     activeConfig.Audio.Bitrate,
		OutputFormat:     activeConfig.Output.Format,
	}

	slog.Debug("Config fetched from master",
		"resolution", cfg.TargetResolution,
		"codec", cfg.Codec,
		"format", cfg.OutputFormat,
	)

	return cfg, nil
}

// InvalidateCache invalidates the cached configuration
func (cf *ConfigFetcher) InvalidateCache() {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.cachedConfig = nil
}
