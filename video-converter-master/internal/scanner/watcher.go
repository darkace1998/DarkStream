package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory tree for new video files
type Watcher struct {
	watcher      *fsnotify.Watcher
	scanner      *Scanner
	Jobs         chan *models.Job
	Errors       chan error
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	pendingFiles map[string]*time.Timer
	pendingMu    sync.Mutex
}

// NewWatcher creates a new file system watcher based on the scanner configuration
func NewWatcher(scanner *Scanner) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &Watcher{
		watcher:      fw,
		scanner:      scanner,
		Jobs:         make(chan *models.Job, 100),
		Errors:       make(chan error, 10),
		ctx:          ctx,
		cancel:       cancel,
		pendingFiles: make(map[string]*time.Timer),
	}

	return w, nil
}

// Start begins watching the directory tree
func (w *Watcher) Start() error {
	// Add root path and all subdirectories recursively
	err := w.addDirRecursive(w.scanner.RootPath, 0)
	if err != nil {
		return fmt.Errorf("failed to add directories to watcher: %w", err)
	}

	w.wg.Add(1)
	go w.watchLoop()

	slog.Info("Started filesystem watcher", "root_path", w.scanner.RootPath)
	return nil
}

// Close stops the watcher and closes channels
func (w *Watcher) Close() error {
	w.cancel()
	err := w.watcher.Close()
	w.wg.Wait()
	close(w.Jobs)
	close(w.Errors)
	return err
}

// addDirRecursive adds a directory and its subdirectories to the watcher
func (w *Watcher) addDirRecursive(dir string, currentDepth int) error {
	// Check depth limit
	if w.scanner.Options.MaxDepth >= 0 && currentDepth > w.scanner.Options.MaxDepth {
		return nil
	}

	// Skip hidden directories if configured
	baseName := filepath.Base(dir)
	if strings.HasPrefix(baseName, ".") && w.scanner.Options.SkipHiddenDirs {
		// Only skip if it's not the root path
		if dir != w.scanner.RootPath {
			return nil
		}
	}

	// Add the current directory
	err := w.watcher.Add(dir)
	if err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", dir, err)
	}
	slog.Debug("Added directory to watcher", "path", dir)

	// Read directory contents to find subdirectories
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(dir, entry.Name())
			if err := w.addDirRecursive(fullPath, currentDepth+1); err != nil {
				slog.Warn("Failed to add subdirectory to watcher", "path", fullPath, "error", err)
			}
		}
	}

	return nil
}

// watchLoop handles fsnotify events
func (w *Watcher) watchLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Create and Write events indicate a file is being added/modified.
			// We debounce these events to wait for the file to finish writing.
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				w.debounceEvent(event.Name)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("Filesystem watcher error", "error", err)
			select {
			case w.Errors <- err:
			default:
				// Avoid blocking if error channel is full
			}
		}
	}
}

// debounceEvent schedules the processing of a file after a delay.
// If the file is modified again before the delay expires, the timer resets.
func (w *Watcher) debounceEvent(path string) {
	// If it's a directory, process it immediately without debouncing
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		w.processEventDebounced(path)
		return
	}

	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	// If there's an existing timer, reset it
	if timer, exists := w.pendingFiles[path]; exists {
		timer.Reset(2 * time.Second) // Wait 2 seconds of inactivity before processing
		return
	}

	// Otherwise, create a new timer
	w.pendingFiles[path] = time.AfterFunc(2*time.Second, func() {
		w.processEventDebounced(path)
	})
}

// processEventDebounced is called when a file hasn't been modified for a while.
func (w *Watcher) processEventDebounced(path string) {
	// Clean up the timer from the map
	w.pendingMu.Lock()
	delete(w.pendingFiles, path)
	w.pendingMu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("Failed to stat file after debouncing", "path", path, "error", err)
		}
		return
	}

	// If it's a new directory, add it to the watcher (recursively, though it should be empty initially)
	if info.IsDir() {
		// Calculate depth relative to root path
		relPath, err := filepath.Rel(w.scanner.RootPath, path)
		if err == nil {
			depth := len(strings.Split(relPath, string(os.PathSeparator)))
			if err := w.addDirRecursive(path, depth); err != nil {
				slog.Warn("Failed to watch new directory", "path", path, "error", err)
			}
		}
		return
	}

	// It's a file, process it
	job, err := w.scanner.ProcessFile(path)
	if err != nil {
		slog.Warn("Failed to process new file from watcher", "path", path, "error", err)
		return
	}

	if job != nil {
		slog.Info("Watcher found new video file", "path", path, "job_id", job.ID)
		select {
		case w.Jobs <- job:
		case <-w.ctx.Done():
			return
		}
	}
}
