package scanner

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

// ScanDirectoryParallel walks the directory tree and processes candidate video
// files with engines parallel workers. Directory traversal itself is cheap
// (ReadDir only); the expensive part — hashing files — is spread across the
// engine pool, so scans scale with the disk's I/O parallelism up to engines.
// It returns the jobs found and a stats snapshot.
func (s *Scanner) ScanDirectoryParallel(engines int) ([]*models.Job, ScanStats, error) {
	if engines < 1 {
		engines = 1
	}
	start := time.Now()
	s.resetStats()

	dirs := s.collectDirs()
	s.bump(func(st *ScanStats) { st.Dirs = len(dirs) })

	filesCh := make(chan string, 1024)
	var wg sync.WaitGroup
	var resMu sync.Mutex
	var jobs []*models.Job

	for range engines {
		wg.Go(func() {
			for path := range filesCh {
				job, procErr := s.ProcessFile(path)
				if procErr != nil {
					continue
				}
				if job != nil {
					resMu.Lock()
					jobs = append(jobs, job)
					resMu.Unlock()
					s.bump(func(st *ScanStats) { st.Jobs++ })
				}
			}
		})
	}

	s.feedFiles(dirs, filesCh)
	close(filesCh)
	wg.Wait()

	stats := s.Stats()
	stats.Duration = time.Since(start)
	s.bump(func(st *ScanStats) { st.Duration = stats.Duration })
	return jobs, s.Stats(), nil
}

// feedFiles walks the collected directories and sends candidate video files
// into filesCh. Non-video files are counted and skipped without any stat or
// hash I/O.
func (s *Scanner) feedFiles(dirs []string, filesCh chan<- string) {
	opts := s.GetOptions()
	for _, dir := range dirs {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			slog.Warn("Error reading directory", "path", dir, "error", readErr)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") && opts.SkipHiddenFiles {
				continue
			}
			// Pre-filter by extension: non-video files are skipped without any
			// stat or hash I/O.
			ext := strings.ToLower(filepath.Ext(name))
			if !s.VideoExtensions[ext] {
				s.bump(func(st *ScanStats) { st.Files++ })
				continue
			}
			fullPath := filepath.Join(dir, name)
			if s.IsOutputPath(fullPath) {
				continue
			}
			s.bump(func(st *ScanStats) { st.Videos++ })
			filesCh <- fullPath
		}
	}
}

// collectDirs walks the tree honoring depth, hidden and output-dir rules and
// returns every directory that may contain video files.
func (s *Scanner) collectDirs() []string {
	opts := s.GetOptions()
	var dirs []string

	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if opts.MaxDepth >= 0 && depth > opts.MaxDepth {
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			slog.Warn("Error reading directory", "path", path, "error", err)
			return
		}
		dirs = append(dirs, path)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			fullPath := filepath.Join(path, entry.Name())
			if strings.HasPrefix(entry.Name(), ".") && opts.SkipHiddenDirs {
				continue
			}
			if s.IsOutputPath(fullPath) {
				continue
			}
			walk(fullPath, depth+1)
		}
	}
	walk(s.RootPath, 0)
	return dirs
}
