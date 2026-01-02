package worker

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CacheManager manages local file caching with cleanup policies
type CacheManager struct {
	cachePath   string
	maxSize     int64         // Maximum cache size in bytes (0 = unlimited)
	maxAge      time.Duration // Maximum age before cleanup (0 = no age-based cleanup)
	mu          sync.RWMutex
	currentSize int64
	stopped     bool
}

// CacheEntry represents a cached file entry
type CacheEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// NewCacheManager creates a new CacheManager instance
func NewCacheManager(cachePath string, maxSize int64, maxAge time.Duration) *CacheManager {
	cm := &CacheManager{
		cachePath: cachePath,
		maxSize:   maxSize,
		maxAge:    maxAge,
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cachePath, 0o750); err != nil {
		slog.Warn("Failed to create cache directory", "path", cachePath, "error", err)
	}

	// Calculate initial cache size
	cm.calculateCacheSize()

	return cm
}

// calculateCacheSize calculates the current cache size
func (cm *CacheManager) calculateCacheSize() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var totalSize int64
	_ = filepath.WalkDir(cm.cachePath, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip errors and continue walking
		}
		if !d.IsDir() {
			info, infoErr := d.Info()
			if infoErr == nil {
				totalSize += info.Size()
			}
		}
		return nil
	})

	cm.currentSize = totalSize
	slog.Debug("Cache size calculated", "size_bytes", totalSize, "path", cm.cachePath)
}

// GetCacheSize returns the current cache size in bytes
func (cm *CacheManager) GetCacheSize() int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentSize
}

// Cleanup removes old files and enforces size limits
func (cm *CacheManager) Cleanup() error {
	cm.mu.Lock()
	if cm.stopped {
		cm.mu.Unlock()
		return nil
	}
	cm.mu.Unlock()

	entries, err := cm.listCacheEntries()
	if err != nil {
		return err
	}

	var totalSize int64
	var removedCount int
	var removedSize int64
	now := time.Now()

	// First pass: remove old files based on age
	if cm.maxAge > 0 {
		for _, entry := range entries {
			age := now.Sub(entry.ModTime)
			if age > cm.maxAge {
				if err := os.Remove(entry.Path); err != nil {
					slog.Warn("Failed to remove old cache file", "path", entry.Path, "error", err)
					continue
				}
				removedCount++
				removedSize += entry.Size
				slog.Debug("Removed old cache file", "path", entry.Path, "age", age)
			} else {
				totalSize += entry.Size
			}
		}
	} else {
		for _, entry := range entries {
			totalSize += entry.Size
		}
	}

	// Second pass: remove files to enforce size limit (remove oldest first)
	if cm.maxSize > 0 && totalSize > cm.maxSize {
		// Re-fetch entries after age cleanup
		entries, err = cm.listCacheEntries()
		if err != nil {
			return err
		}

		// Sort by modification time (oldest first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ModTime.Before(entries[j].ModTime)
		})

		// Calculate current size and remove files in a single pass
		currentSize := totalSize
		for _, entry := range entries {
			if currentSize <= cm.maxSize {
				break
			}

			if err := os.Remove(entry.Path); err != nil {
				slog.Warn("Failed to remove cache file for size limit", "path", entry.Path, "error", err)
				continue
			}
			currentSize -= entry.Size
			removedCount++
			removedSize += entry.Size
			slog.Debug("Removed cache file for size limit", "path", entry.Path, "size", entry.Size)
		}

		totalSize = currentSize
	}

	cm.mu.Lock()
	cm.currentSize = totalSize
	cm.mu.Unlock()

	if removedCount > 0 {
		slog.Info("Cache cleanup completed",
			"removed_files", removedCount,
			"removed_bytes", removedSize,
			"current_size", totalSize,
		)
	}

	return nil
}

// listCacheEntries lists all cache entries
func (cm *CacheManager) listCacheEntries() ([]CacheEntry, error) {
	var entries []CacheEntry

	err := filepath.WalkDir(cm.cachePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip errors and continue walking
		}
		if !d.IsDir() {
			info, infoErr := d.Info()
			if infoErr == nil {
				entries = append(entries, CacheEntry{
					Path:    path,
					Size:    info.Size(),
					ModTime: info.ModTime(),
				})
			}
		}
		return nil
	})

	if err != nil {
		return entries, fmt.Errorf("failed to walk cache directory: %w", err)
	}
	return entries, nil
}

// AddFile records a new file being added to the cache
func (cm *CacheManager) AddFile(size int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.currentSize += size
}

// RemoveFile records a file being removed from the cache
func (cm *CacheManager) RemoveFile(size int64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.currentSize -= size
	if cm.currentSize < 0 {
		cm.currentSize = 0
	}
}

// Stop stops the cache manager
func (cm *CacheManager) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.stopped = true
	slog.Info("Cache manager stopped")
}

// IsStopped returns whether the cache manager is stopped
func (cm *CacheManager) IsStopped() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.stopped
}
