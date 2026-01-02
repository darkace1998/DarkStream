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

// Default configuration values for ConfigFetcher
const (
	DefaultConfigRefreshInterval = 30 * time.Second
	DefaultMaxRetries            = 3
	DefaultRetryBaseDelay        = 2 * time.Second
	fetchWaitDuration            = 100 * time.Millisecond
	maxFetchWaitRetries          = 50 // Maximum times to retry waiting for another goroutine's fetch
)

// ConfigFetcher handles fetching and caching configuration from the master
type ConfigFetcher struct {
	baseURL         string
	client          *http.Client
	mu              sync.RWMutex
	cachedConfig    *models.ConversionSettings
	lastFetchTime   time.Time
	refreshInterval time.Duration
	maxRetries      int
	retryBaseDelay  time.Duration
	fetching        bool // Prevents concurrent fetch operations
}

// NewConfigFetcher creates a new ConfigFetcher instance with default settings
func NewConfigFetcher(baseURL string) *ConfigFetcher {
	return NewConfigFetcherWithOptions(baseURL, DefaultConfigRefreshInterval, DefaultMaxRetries, DefaultRetryBaseDelay)
}

// NewConfigFetcherWithOptions creates a new ConfigFetcher with custom settings
func NewConfigFetcherWithOptions(baseURL string, refreshInterval time.Duration, maxRetries int, retryBaseDelay time.Duration) *ConfigFetcher {
	if refreshInterval <= 0 {
		refreshInterval = DefaultConfigRefreshInterval
	}
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	if retryBaseDelay <= 0 {
		retryBaseDelay = DefaultRetryBaseDelay
	}
	return &ConfigFetcher{
		baseURL:         baseURL,
		client:          &http.Client{Timeout: 30 * time.Second},
		refreshInterval: refreshInterval,
		maxRetries:      maxRetries,
		retryBaseDelay:  retryBaseDelay,
	}
}

// FetchConfig fetches the current configuration from the master
// It returns the cached config if available and still fresh
// Uses a single-flight pattern to prevent concurrent requests when cache is stale
func (cf *ConfigFetcher) FetchConfig() (*models.ConversionSettings, error) {
	return cf.fetchConfigWithRetry(0)
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

// InvalidateCache invalidates the cached configuration
func (cf *ConfigFetcher) InvalidateCache() {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.cachedConfig = nil
}

// fetchConfigWithRetry implements FetchConfig with a retry counter to prevent infinite recursion
func (cf *ConfigFetcher) fetchConfigWithRetry(waitRetries int) (*models.ConversionSettings, error) {
	// First try with read lock - fast path for cached config
	cf.mu.RLock()
	if cf.cachedConfig != nil && time.Since(cf.lastFetchTime) < cf.refreshInterval {
		cfg := *cf.cachedConfig
		cf.mu.RUnlock()
		return &cfg, nil
	}
	cf.mu.RUnlock()

	// Need to potentially update - acquire write lock
	cf.mu.Lock()

	// Double-check cache after acquiring write lock
	if cf.cachedConfig != nil && time.Since(cf.lastFetchTime) < cf.refreshInterval {
		cfg := *cf.cachedConfig
		cf.mu.Unlock()
		return &cfg, nil
	}

	// If another goroutine is already fetching, wait and use cached result
	if cf.fetching {
		cachedCfg := cf.cachedConfig
		cf.mu.Unlock()
		if cachedCfg != nil {
			cfg := *cachedCfg
			return &cfg, nil
		}
		// No cache available, wait a bit and retry (with bounds to prevent infinite recursion)
		if waitRetries >= maxFetchWaitRetries {
			return nil, fmt.Errorf("timed out waiting for config fetch after %d retries", waitRetries)
		}
		time.Sleep(fetchWaitDuration)
		return cf.fetchConfigWithRetry(waitRetries + 1)
	}

	// Mark as fetching
	cf.fetching = true
	cf.mu.Unlock()

	// Ensure we clear fetching flag when done
	defer func() {
		cf.mu.Lock()
		cf.fetching = false
		cf.mu.Unlock()
	}()

	// Fetch fresh config
	return cf.fetchFromMaster()
}

// fetchFromMaster fetches configuration from the master with retry logic
func (cf *ConfigFetcher) fetchFromMaster() (*models.ConversionSettings, error) {
	var lastErr error
	for attempt := 0; attempt < cf.maxRetries; attempt++ {
		if attempt > 0 {
			delay := cf.retryBaseDelay * time.Duration(1<<(attempt-1))
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

	return nil, fmt.Errorf("failed to fetch config after %d attempts: %w", cf.maxRetries, lastErr)
}

// fetchConfigAttempt performs a single config fetch attempt
func (cf *ConfigFetcher) fetchConfigAttempt() (*models.ConversionSettings, error) {
	url := fmt.Sprintf("%s/api/config", cf.baseURL)

	resp, err := cf.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to request config: %w", err)
	}
	defer func() {
		cerr := resp.Body.Close()
		if cerr != nil {
			slog.Warn("Failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			slog.Warn("Failed to read response body for error reporting", "error", readErr)
			return nil, fmt.Errorf("unexpected status code: %d, failed to read body: %w", resp.StatusCode, readErr)
		}
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

	err = json.NewDecoder(resp.Body).Decode(&activeConfig)
	if err != nil {
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
