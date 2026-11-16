// Package scanner implements file system scanning for video files.
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// FileStore abstracts persistence of per-file scan state, so repeat scans can
// skip re-hashing files whose size and mtime are unchanged.
type FileStore interface {
	// GetFileScanState returns size, mtime (ns), stored hash, its algorithm,
	// and ok=false when the file was never scanned.
	GetFileScanState(path string) (size int64, mtimeNs int64, hash string, algo string, ok bool, err error)
	// UpsertFileScanState records the scan state of a file.
	UpsertFileScanState(path string, size int64, mtimeNs int64, hash, algo string) error
}

// ScanOptions configures scanner behavior
type ScanOptions struct {
	MaxDepth         int                                 // -1 for unlimited, 0 for root only, >0 for specific depth
	MinFileSize      int64                               // Minimum file size in bytes (0 = no minimum)
	MaxFileSize      int64                               // Maximum file size in bytes (0 = no maximum)
	SkipHiddenFiles  bool                                // Skip files starting with '.'
	SkipHiddenDirs   bool                                // Skip directories starting with '.'
	ReplaceSource    bool                                // Replace source file with output (output path = source path)
	DetectDuplicates bool                                // Track file hashes to detect duplicates
	HashAlgo         string                              // sha256 (default) or sha1 — sha1 is faster and less CPU-intensive
	HashMode         string                              // "full" (default) hashes files; "skip" never reads file contents (no source checksum, no content dedup)
	Priority         int                                 // Default priority for jobs created by this scanner (0-10)
	FileStore        FileStore                           // Optional store enabling unchanged-file skip
	DuplicateChecker func(checksum string) (bool, error) // Optional callback to check for duplicates in an external system (e.g. database)
	// KnownOutputChecker optionally reports whether a file with this checksum was
	// produced by an earlier conversion, in which case it is not a new source.
	KnownOutputChecker func(checksum string) (bool, error)
}

// ScanStats reports what a scan did, for logging, metrics, and tests.
type ScanStats struct {
	Dirs             int           `json:"dirs"`
	Files            int           `json:"files"`             // non-video files skipped by extension filter
	Videos           int           `json:"videos"`            // video files considered
	Hashed           int           `json:"hashed"`            // files read and hashed
	SkippedUnchanged int           `json:"skipped_unchanged"` // files skipped via stored size+mtime
	SkippedOutput    int           `json:"skipped_output"`    // already-converted files
	Duplicates       int           `json:"duplicates"`
	Jobs             int           `json:"jobs"` // new jobs produced
	Duration         time.Duration `json:"duration_ns"`
}

// convertedExtensions are the container extensions a conversion may produce.
var convertedExtensions = []string{extMP4, extMKV, extWebM, extAVI}

// Container file extensions.
const (
	extMP4  = ".mp4"
	extMKV  = ".mkv"
	extWebM = ".webm"
	extAVI  = ".avi"
)

// Scanner discovers video files in a directory tree
type Scanner struct {
	RootPath        string
	VideoExtensions map[string]bool
	OutputBase      string
	Options         ScanOptions
	seenHashes      map[string]string // hash -> first file path (for duplicate detection)
	stats           ScanStats         // accumulator for the current/last parallel scan
	mu              sync.RWMutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Options = opts
	if opts.DetectDuplicates {
		s.seenHashes = make(map[string]string)
	}
}

// GetOptions returns a snapshot of the current scanner options taken under the
// lock, so callers can read option fields without racing SetOptions.
func (s *Scanner) GetOptions() ScanOptions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Options
}

// ScanDirectory walks the directory tree and finds all video files (single engine)
func (s *Scanner) ScanDirectory() ([]*models.Job, error) {
	jobs, _, err := s.ScanDirectoryParallel(1)
	return jobs, err
}

// bump applies a stats update under the scanner lock.
func (s *Scanner) bump(f func(*ScanStats)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.stats)
}

// resetStats clears the stats accumulator and the duplicate map for a new scan.
func (s *Scanner) resetStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = ScanStats{}
	if s.Options.DetectDuplicates {
		s.seenHashes = make(map[string]string)
	}
}

// Stats returns a snapshot of the current/last scan statistics.
func (s *Scanner) Stats() ScanStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// ProcessFile validates a single file and creates a Job if it matches criteria.
// Returns nil, nil if the file is skipped (e.g., wrong extension, too large).
func (s *Scanner) ProcessFile(fullPath string) (*models.Job, error) {
	s.mu.RLock()
	opts := s.Options
	s.mu.RUnlock()

	// Skip hidden files/directories if configured
	baseName := filepath.Base(fullPath)
	if strings.HasPrefix(baseName, ".") {
		if opts.SkipHiddenFiles {
			slog.Debug("Skipping hidden file", "path", fullPath)
			return nil, nil
		}
	}

	// Check file extension
	ext := strings.ToLower(filepath.Ext(baseName))
	if !s.VideoExtensions[ext] {
		return nil, nil
	}

	// Never treat our own converted output as a new source.
	if s.IsOutputPath(fullPath) {
		return nil, nil
	}

	// Get file info for size filtering
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		slog.Warn("Failed to get file info", "path", fullPath, "error", err)
		return nil, err
	}

	if fileInfo.IsDir() {
		return nil, nil
	}

	// Apply size filters
	fileSize := fileInfo.Size()
	if opts.MinFileSize > 0 && fileSize < opts.MinFileSize {
		slog.Debug("Skipping file (too small)", "path", fullPath, "size", fileSize, "min", opts.MinFileSize)
		return nil, nil
	}
	if opts.MaxFileSize > 0 && fileSize > opts.MaxFileSize {
		slog.Debug("Skipping file (too large)", "path", fullPath, "size", fileSize, "max", opts.MaxFileSize)
		return nil, nil
	}

	sourceChecksum, sourceAlgo, skip := s.checksumUnlessHandled(fullPath, fileInfo, opts)
	if skip {
		return nil, nil
	}

	// Generate output path
	var outputPath string
	if opts.ReplaceSource {
		// Replace source file with output (same location)
		outputPath = fullPath
	} else {
		// Place in output directory maintaining structure
		relPath, relErr := filepath.Rel(s.RootPath, fullPath)
		if relErr != nil {
			slog.Warn("Failed to compute relative path", "root", s.RootPath, "path", fullPath, "error", relErr)
			return nil, relErr
		}
		outputPath = filepath.Join(s.OutputBase, strings.TrimSuffix(relPath, ext)+".mp4")
	}

	// Validate paths before creating job
	// This ensures no path traversal issues even if directory structure is compromised
	// Use utils for consistent validation across the codebase
	_, err = utils.ValidatePathWithinBase(s.RootPath, fullPath)
	if err != nil {
		slog.Warn("Source path validation failed, skipping", "root", s.RootPath, "path", fullPath, "error", err)
		return nil, err
	}
	if !opts.ReplaceSource {
		_, err = utils.ValidatePathWithinBase(s.OutputBase, outputPath)
		if err != nil {
			slog.Warn("Output path validation failed, skipping", "output_base", s.OutputBase, "path", outputPath, "error", err)
			return nil, err
		}
	}

	priority := opts.Priority
	if priority == 0 {
		priority = 5 // Default priority (normal)
	}

	job := &models.Job{
		ID:                 generateJobID(fullPath),
		SourcePath:         fullPath,
		OutputPath:         outputPath,
		Status:             "pending",
		Priority:           priority,
		CreatedAt:          time.Now(),
		RetryCount:         0,
		MaxRetries:         3,
		SourceChecksum:     sourceChecksum,
		SourceChecksumAlgo: sourceAlgo,
	}

	slog.Debug("Found video file", "path", fullPath, "job_id", job.ID, "size", fileSize, "checksum", sourceChecksum)
	return job, nil
}

// checksumUnlessHandled decides whether fullPath still needs converting. It
// returns skip=true when the file is already converted, is itself the product
// of a conversion, or is a duplicate; otherwise it returns the file's checksum
// (empty if it could not be computed) and the algorithm used to produce it.
func (s *Scanner) checksumUnlessHandled(fullPath string, fileInfo os.FileInfo, opts ScanOptions) (checksum, algo string, skip bool) {
	// Skip sources that already have an up-to-date converted file. Without this,
	// pruning finished jobs (or losing the database) makes the next scan queue
	// every already-converted video again.
	if !opts.ReplaceSource && s.hasCurrentOutput(fullPath, fileInfo) {
		slog.Debug("Skipping file (already converted)", "path", fullPath)
		s.bump(func(st *ScanStats) { st.SkippedOutput++ })
		return "", "", true
	}

	algo = opts.HashAlgo
	if algo == "" {
		algo = utils.HashAlgoSHA256
	}

	// hash_mode=skip: trust size+mtime change detection and never read the
	// file at scan time. The job is created without a source checksum (the
	// worker then skips download verification) and content-hash duplicate
	// detection is disabled.
	if opts.HashMode == models.HashModeSkip {
		// Record size+mtime only; never read the file contents.
		if opts.FileStore != nil {
			if storeErr := opts.FileStore.UpsertFileScanState(fullPath, fileInfo.Size(), fileInfo.ModTime().UnixNano(), "", models.HashModeSkip); storeErr != nil {
				slog.Warn("Failed to store file scan state", "path", fullPath, "error", storeErr)
			}
		}
		return "", models.HashModeSkip, false
	}

	// Change detection: if size and mtime match the stored record, reuse the
	// stored hash instead of reading the whole file again.
	if opts.FileStore != nil {
		size, mtimeNs, storedHash, storedAlgo, ok, storeErr := opts.FileStore.GetFileScanState(fullPath)
		if storeErr != nil {
			slog.Warn("Failed to read file scan state", "path", fullPath, "error", storeErr)
		} else if ok && storedHash != "" && size == fileInfo.Size() && mtimeNs == fileInfo.ModTime().UnixNano() {
			s.bump(func(st *ScanStats) { st.SkippedUnchanged++ })
			checksum, algo = storedHash, storedAlgo
			return s.dedupAndFinish(fullPath, checksum, algo, opts)
		}
	}

	// Hash the file once; the result serves both duplicate detection and the
	// job's source checksum (hashing a large video twice doubles the scan I/O).
	checksum, hashErr := utils.CalculateFileHash(fullPath, algo)
	if hashErr != nil {
		slog.Warn("Failed to compute source checksum", "path", fullPath, "error", hashErr)
		// Continue without checksum - it will be empty string
		return "", algo, false
	}
	s.bump(func(st *ScanStats) { st.Hashed++ })

	if opts.FileStore != nil {
		if storeErr := opts.FileStore.UpsertFileScanState(fullPath, fileInfo.Size(), fileInfo.ModTime().UnixNano(), checksum, algo); storeErr != nil {
			slog.Warn("Failed to store file scan state", "path", fullPath, "error", storeErr)
		}
	}

	return s.dedupAndFinish(fullPath, checksum, algo, opts)
}

// dedupAndFinish runs the known-output and duplicate checks for a hashed file
// and reports whether it should be skipped.
func (s *Scanner) dedupAndFinish(fullPath, checksum, algo string, opts ScanOptions) (string, string, bool) {
	// A file this system produced itself (e.g. the result of a replace-source
	// conversion) is not a new source.
	if opts.KnownOutputChecker != nil {
		known, checkErr := opts.KnownOutputChecker(checksum)
		if checkErr != nil {
			slog.Warn("Failed to check known outputs", "path", fullPath, "error", checkErr)
		} else if known {
			slog.Debug("Skipping file (output of an earlier conversion)", "path", fullPath)
			return "", algo, true
		}
	}

	// Detect duplicates if enabled
	if opts.DetectDuplicates && s.isDuplicate(fullPath, checksum, opts) {
		s.bump(func(st *ScanStats) { st.Duplicates++ })
		return "", algo, true
	}

	return checksum, algo, false
}

// hasCurrentOutput reports whether a converted file for the source already
// exists in the output directory and is at least as new as the source. Any of
// the supported output containers counts, since the format is configurable.
func (s *Scanner) hasCurrentOutput(fullPath string, sourceInfo os.FileInfo) bool {
	relPath, err := filepath.Rel(s.RootPath, fullPath)
	if err != nil || s.OutputBase == "" {
		return false
	}
	base := filepath.Join(s.OutputBase, strings.TrimSuffix(relPath, filepath.Ext(relPath)))
	for _, outExt := range convertedExtensions {
		candidate := base + outExt
		if candidate == fullPath {
			continue
		}
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() && info.Size() > 0 && !info.ModTime().Before(sourceInfo.ModTime()) {
			return true
		}
	}
	return false
}

// IsOutputPath reports whether path lies inside the output directory when that
// directory is nested within the scanned root. Converted files written there
// must not be picked up as sources, or every output would be queued for
// conversion again (and its output in turn, indefinitely). It is never true in
// replace-source mode or when the output directory is the root itself.
func (s *Scanner) IsOutputPath(path string) bool {
	s.mu.RLock()
	replace := s.Options.ReplaceSource
	s.mu.RUnlock()
	if replace || s.OutputBase == "" {
		return false
	}

	root, err := filepath.Abs(s.RootPath)
	if err != nil {
		return false
	}
	output, err := filepath.Abs(s.OutputBase)
	if err != nil {
		return false
	}
	if !isStrictlyWithin(root, output) {
		return false
	}

	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return target == output || isStrictlyWithin(output, target)
}

// isStrictlyWithin reports whether path is a descendant of base (not base itself).
func isStrictlyWithin(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

// isDuplicate reports whether fullPath (with content hash fileHash) should be
// skipped as a duplicate, recording the hash for in-scan deduplication and
// consulting the optional external checker.
func (s *Scanner) isDuplicate(fullPath, fileHash string, opts ScanOptions) bool {
	s.mu.Lock()
	if originalPath, exists := s.seenHashes[fileHash]; exists {
		s.mu.Unlock()
		slog.Info("Duplicate file detected in current scan",
			"path", fullPath,
			"original", originalPath,
			"hash", fileHash)
		return true
	}
	s.seenHashes[fileHash] = fullPath
	s.mu.Unlock()

	// Check external duplicate checker if provided
	if opts.DuplicateChecker != nil {
		exists, err := opts.DuplicateChecker(fileHash)
		if err != nil {
			slog.Warn("Failed to check external duplicate checker", "path", fullPath, "error", err)
		} else if exists {
			slog.Info("Duplicate file detected in external checker (e.g. database)",
				"path", fullPath,
				"hash", fileHash)
			return true
		}
	}

	return false
}

// generateJobID creates a unique ID for a job based on the source path
func generateJobID(path string) string {
	// Use SHA256 hash of the path to create a stable, unique ID
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])[:16]
}
