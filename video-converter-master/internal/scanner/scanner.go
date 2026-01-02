// Package scanner implements file system scanning for video files.
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
	"github.com/darkace1998/video-converter-common/utils"
)

// ScanOptions configures scanner behavior
type ScanOptions struct {
	MaxDepth         int   // -1 for unlimited, 0 for root only, >0 for specific depth
	MinFileSize      int64 // Minimum file size in bytes (0 = no minimum)
	MaxFileSize      int64 // Maximum file size in bytes (0 = no maximum)
	SkipHiddenFiles  bool  // Skip files starting with '.'
	SkipHiddenDirs   bool  // Skip directories starting with '.'
	ReplaceSource    bool  // Replace source file with output (output path = source path)
	DetectDuplicates bool  // Track file hashes to detect duplicates
}

// Scanner discovers video files in a directory tree
type Scanner struct {
	RootPath        string
	VideoExtensions map[string]bool
	OutputBase      string
	Options         ScanOptions
	seenHashes      map[string]string // hash -> first file path (for duplicate detection)
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
		Options: ScanOptions{
			MaxDepth:         -1,   // Unlimited by default
			MinFileSize:      0,    // No minimum
			MaxFileSize:      0,    // No maximum
			SkipHiddenFiles:  true, // Skip hidden files by default
			SkipHiddenDirs:   true, // Skip hidden directories by default
			ReplaceSource:    false,
			DetectDuplicates: false,
		},
		seenHashes: make(map[string]string),
	}
}

// SetOptions configures scanner options
func (s *Scanner) SetOptions(opts ScanOptions) {
	s.Options = opts
	if opts.DetectDuplicates {
		s.seenHashes = make(map[string]string)
	}
}

// ScanDirectory walks the directory tree and finds all video files
func (s *Scanner) ScanDirectory() ([]*models.Job, error) {
	var jobs []*models.Job

	// Reset duplicate detection map if enabled
	if s.Options.DetectDuplicates {
		s.seenHashes = make(map[string]string)
	}

	err := s.scanWithDepth(s.RootPath, 0, &jobs)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	return jobs, nil
}

// scanWithDepth recursively scans directories with depth control
//
//nolint:cyclop,gocognit,unparam // Directory scanning with filtering and deduplication is inherently complex; error return for future-proofing
func (s *Scanner) scanWithDepth(currentPath string, currentDepth int, jobs *[]*models.Job) error {
	// Check depth limit
	if s.Options.MaxDepth >= 0 && currentDepth > s.Options.MaxDepth {
		return nil
	}

	entries, err := os.ReadDir(currentPath)
	if err != nil {
		slog.Warn("Error reading directory", "path", currentPath, "error", err)
		return nil // Continue scanning despite errors
	}

	for _, entry := range entries {
		fullPath := filepath.Join(currentPath, entry.Name())

		// Skip hidden files/directories if configured
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() && s.Options.SkipHiddenDirs {
				slog.Debug("Skipping hidden directory", "path", fullPath)
				continue
			}
			if !entry.IsDir() && s.Options.SkipHiddenFiles {
				slog.Debug("Skipping hidden file", "path", fullPath)
				continue
			}
		}

		if entry.IsDir() {
			// Recursively scan subdirectory
			err := s.scanWithDepth(fullPath, currentDepth+1, jobs)
			if err != nil {
				slog.Warn("Error scanning subdirectory", "path", fullPath, "error", err)
			}
			continue
		}

		// Check file extension
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !s.VideoExtensions[ext] {
			continue
		}

		// Get file info for size filtering
		fileInfo, err := entry.Info()
		if err != nil {
			slog.Warn("Failed to get file info", "path", fullPath, "error", err)
			continue
		}

		// Apply size filters
		fileSize := fileInfo.Size()
		if s.Options.MinFileSize > 0 && fileSize < s.Options.MinFileSize {
			slog.Debug("Skipping file (too small)", "path", fullPath, "size", fileSize, "min", s.Options.MinFileSize)
			continue
		}
		if s.Options.MaxFileSize > 0 && fileSize > s.Options.MaxFileSize {
			slog.Debug("Skipping file (too large)", "path", fullPath, "size", fileSize, "max", s.Options.MaxFileSize)
			continue
		}

		// Detect duplicates if enabled
		if s.Options.DetectDuplicates {
			fileHash, err := computeFileHash(fullPath)
			if err != nil {
				slog.Warn("Failed to compute file hash", "path", fullPath, "error", err)
				// Continue processing even if hash fails
			} else {
				if originalPath, exists := s.seenHashes[fileHash]; exists {
					slog.Info("Duplicate file detected",
						"path", fullPath,
						"original", originalPath,
						"hash", fileHash)
					continue // Skip duplicate
				}
				s.seenHashes[fileHash] = fullPath
			}
		}

		// Generate output path
		var outputPath string
		if s.Options.ReplaceSource {
			// Replace source file with output (same location)
			outputPath = fullPath
		} else {
			// Place in output directory maintaining structure
			relPath, relErr := filepath.Rel(s.RootPath, fullPath)
			if relErr != nil {
				slog.Warn("Failed to compute relative path", "root", s.RootPath, "path", fullPath, "error", relErr)
				continue
			}
			outputPath = filepath.Join(s.OutputBase, strings.TrimSuffix(relPath, ext)+".mp4")
		}

		// Validate paths before creating job
		// This ensures no path traversal issues even if directory structure is compromised
		// Use utils for consistent validation across the codebase
		_, err = utils.ValidatePathWithinBase(s.RootPath, fullPath)
		if err != nil {
			slog.Warn("Source path validation failed, skipping", "root", s.RootPath, "path", fullPath, "error", err)
			continue
		}
		if !s.Options.ReplaceSource {
			_, err = utils.ValidatePathWithinBase(s.OutputBase, outputPath)
			if err != nil {
				slog.Warn("Output path validation failed, skipping", "output_base", s.OutputBase, "path", outputPath, "error", err)
				continue
			}
		}

		// Calculate source file checksum for integrity validation
		sourceChecksum, err := computeFileHash(fullPath)
		if err != nil {
			slog.Warn("Failed to compute source checksum", "path", fullPath, "error", err)
			// Continue without checksum - it will be empty string
			sourceChecksum = ""
		}

		job := &models.Job{
			ID:             generateJobID(fullPath),
			SourcePath:     fullPath,
			OutputPath:     outputPath,
			Status:         "pending",
			Priority:       5, // Default priority (normal)
			CreatedAt:      time.Now(),
			RetryCount:     0,
			MaxRetries:     3,
			SourceChecksum: sourceChecksum,
		}

		*jobs = append(*jobs, job)
		slog.Debug("Found video file", "path", fullPath, "job_id", job.ID, "size", fileSize, "checksum", sourceChecksum)
	}

	return nil
}

// computeFileHash computes SHA256 hash of file for duplicate detection
func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			slog.Warn("Failed to close file during hash computation", "path", filePath, "error", cerr)
		}
	}()

	hash := sha256.New()

	// Read file in chunks to avoid loading entire file into memory
	buf := make([]byte, 8192)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			_, writeErr := hash.Write(buf[:n])
			if writeErr != nil {
				return "", fmt.Errorf("failed to write to hash: %w", writeErr)
			}
		}
		if readErr != nil {
			if readErr.Error() == "EOF" {
				break
			}
			return "", fmt.Errorf("failed to read file: %w", readErr)
		}
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// generateJobID creates a unique ID for a job based on the source path
func generateJobID(path string) string {
	// Use SHA256 hash of the path to create a stable, unique ID
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])[:16]
}
