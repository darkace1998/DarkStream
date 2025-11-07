package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// Scanner discovers video files in a directory tree
type Scanner struct {
	RootPath        string
	VideoExtensions map[string]bool
	OutputBase      string
}

// New creates a new scanner instance
func New(rootPath string, extensions []string, outputBase string) *Scanner {
	exts := make(map[string]bool)
	for _, ext := range extensions {
		exts[strings.ToLower(ext)] = true
	}

	return &Scanner{
		RootPath:        rootPath,
		VideoExtensions: exts,
		OutputBase:      outputBase,
	}
}

// ScanDirectory walks the directory tree and finds all video files
func (s *Scanner) ScanDirectory() ([]*models.Job, error) {
	var jobs []*models.Job

	err := filepath.Walk(s.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			slog.Warn("Error accessing path", "path", path, "error", err)
			return nil // Continue scanning despite errors
		}

		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !s.VideoExtensions[ext] {
			return nil
		}

		// Generate output path
		relPath, relErr := filepath.Rel(s.RootPath, path)
		if relErr != nil {
			slog.Warn("Failed to compute relative path", "root", s.RootPath, "path", path, "error", relErr)
			return nil // Skip this file and continue
		}
		outputPath := filepath.Join(s.OutputBase, strings.TrimSuffix(relPath, ext)+".mp4")

		job := &models.Job{
			ID:         generateJobID(path),
			SourcePath: path,
			OutputPath: outputPath,
			Status:     "pending",
			CreatedAt:  time.Now(),
			RetryCount: 0,
			MaxRetries: 3,
		}

		jobs = append(jobs, job)
		slog.Debug("Found video file", "path", path, "job_id", job.ID)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	return jobs, nil
}

// generateJobID creates a unique ID for a job based on the source path
func generateJobID(path string) string {
	// Use SHA256 hash of the path to create a stable, unique ID
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])[:16]
}
