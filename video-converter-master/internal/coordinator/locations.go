package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/db"
	"github.com/darkace1998/video-converter-master/internal/metrics"
	"github.com/darkace1998/video-converter-master/internal/scanner"
	"github.com/darkace1998/video-converter-master/internal/server"
)

// LocationManager runs one scan pool (and optional fsnotify watcher) per
// configured scan location. Locations can be added, updated and removed at
// runtime (e.g. from the Web UI / /api/scan-locations).
type LocationManager struct {
	mu       sync.Mutex
	db       *db.Tracker
	srv      *server.Server
	rootCtx  context.Context
	runtimes map[string]*locationRuntime
	wg       sync.WaitGroup
}

// locationRuntime is the running state of a single scan location.
type locationRuntime struct {
	loc     models.ScanLocation
	scanner *scanner.Scanner
	watcher *scanner.Watcher
	cancel  context.CancelFunc
	running atomic.Bool
}

// NewLocationManager creates a LocationManager. srv may be nil in tests.
func NewLocationManager(tracker *db.Tracker, srv *server.Server, rootCtx context.Context) *LocationManager {
	return &LocationManager{
		db:       tracker,
		srv:      srv,
		rootCtx:  rootCtx,
		runtimes: make(map[string]*locationRuntime),
	}
}

// StartAll normalizes the config and starts every configured location.
func (m *LocationManager) StartAll(cfg *models.MasterConfig) {
	cfg.NormalizeScanLocations()
	for _, loc := range cfg.Scanner.Locations {
		if err := m.Add(loc); err != nil {
			slog.Error("Failed to start scan location", "id", loc.ID, "error", err)
		}
	}
}

// Add starts a new scan location. It performs an initial scan in the
// background, then periodic scans and/or watching per the location settings.
func (m *LocationManager) Add(loc models.ScanLocation) error {
	if loc.RootPath == "" {
		return fmt.Errorf("root_path is required")
	}
	if loc.ID == "" {
		return fmt.Errorf("id is required")
	}

	m.mu.Lock()
	if _, exists := m.runtimes[loc.ID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("scan location %q already exists", loc.ID)
	}

	rt := &locationRuntime{loc: loc}
	rt.scanner = scanner.New(loc.RootPath, loc.VideoExtensions, loc.OutputBase)
	rt.scanner.SetOptions(scanner.ScanOptions{
		MaxDepth:           loc.RecursiveDepth,
		MinFileSize:        loc.MinFileSize,
		MaxFileSize:        loc.MaxFileSize,
		SkipHiddenFiles:    loc.SkipHiddenFiles,
		SkipHiddenDirs:     loc.SkipHiddenDirs,
		ReplaceSource:      loc.ReplaceSource,
		DetectDuplicates:   loc.DetectDuplicates,
		HashAlgo:           loc.HashAlgo,
		HashMode:           loc.HashMode,
		Priority:           loc.Priority,
		FileStore:          m.db, // *db.Tracker implements scanner.FileStore
		DuplicateChecker:   m.db.HasJobWithSourceChecksum,
		KnownOutputChecker: m.db.HasKnownOutputChecksum,
	})

	ctx, cancel := context.WithCancel(m.rootCtx)
	rt.cancel = cancel
	m.runtimes[loc.ID] = rt
	m.mu.Unlock()

	if m.srv != nil {
		m.srv.AddAllowedDirs(loc.RootPath, loc.OutputBase)
	}

	m.wg.Add(1)
	go m.run(rt, ctx)
	return nil
}

// Update replaces a location's settings by restarting its runtime.
func (m *LocationManager) Update(loc models.ScanLocation) error {
	if err := m.Remove(loc.ID); err != nil {
		return err
	}
	return m.Add(loc)
}

// Remove stops and removes a scan location.
func (m *LocationManager) Remove(id string) error {
	m.mu.Lock()
	rt, exists := m.runtimes[id]
	if exists {
		delete(m.runtimes, id)
	}
	m.mu.Unlock()
	if !exists {
		return fmt.Errorf("scan location %q not found", id)
	}
	rt.cancel()
	return nil
}

// List returns all locations with their running state.
func (m *LocationManager) List() []models.LocationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.LocationStatus, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		out = append(out, models.LocationStatus{ScanLocation: rt.loc, Running: rt.running.Load()})
	}
	return out
}

// ScanNow triggers an immediate scan of one location in the background.
func (m *LocationManager) ScanNow(id string) error {
	m.mu.Lock()
	rt, exists := m.runtimes[id]
	m.mu.Unlock()
	if !exists {
		return fmt.Errorf("scan location %q not found", id)
	}
	m.wg.Go(func() {
		m.scanOnce(rt)
	})
	return nil
}

// Shutdown stops all location runtimes.
func (m *LocationManager) Shutdown() {
	m.mu.Lock()
	for _, rt := range m.runtimes {
		rt.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// run performs the initial scan, then the periodic scan loop and (optionally)
// the filesystem watcher for one location.
func (m *LocationManager) run(rt *locationRuntime, ctx context.Context) {
	defer m.wg.Done()
	rt.running.Store(true)
	defer rt.running.Store(false)

	m.scanOnce(rt)

	if rt.loc.ScanInterval > 0 {
		m.wg.Add(1)
		go m.periodicScan(rt, ctx)
	}

	if rt.loc.EnableWatch {
		watcher, err := scanner.NewWatcher(rt.scanner)
		if err != nil {
			slog.Error("Failed to initialize file system watcher", "location", rt.loc.ID, "error", err)
		} else if err := watcher.Start(); err != nil {
			slog.Error("Failed to start file system watcher", "location", rt.loc.ID, "error", err)
		} else {
			rt.watcher = watcher
			m.wg.Add(1)
			go m.listenWatcherEvents(rt, watcher, ctx)
		}
	}

	<-ctx.Done()
	slog.Info("Scan location stopping", "location", rt.loc.ID)
}

// scanOnce runs one parallel scan pass of the location and inserts new jobs.
func (m *LocationManager) scanOnce(rt *locationRuntime) {
	jobs, stats, err := rt.scanner.ScanDirectoryParallel(rt.loc.Engines)
	if err != nil {
		slog.Error("Scan failed", "location", rt.loc.ID, "error", err)
		return
	}
	m.insertJobs(rt.loc.ID, jobs)
	metrics.New().RecordScan(rt.loc.ID, stats.Duration.Seconds(), stats.Hashed, stats.SkippedUnchanged, stats.SkippedOutput, stats.Duplicates, stats.Files)
	slog.Info("Scan complete",
		"location", rt.loc.ID,
		"engines", rt.loc.Engines,
		"hash_algo", rt.loc.HashAlgo,
		"dirs", stats.Dirs,
		"videos", stats.Videos,
		"hashed", stats.Hashed,
		"skipped_unchanged", stats.SkippedUnchanged,
		"new_jobs", stats.Jobs,
		"duration", stats.Duration,
	)
}

// periodicScan rescans the location on its configured interval.
func (m *LocationManager) periodicScan(rt *locationRuntime, ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(rt.loc.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scanOnce(rt)
		}
	}
}

// insertJobs stores scanned jobs, ignoring duplicates.
func (m *LocationManager) insertJobs(locationID string, jobs []*models.Job) {
	newJobs := 0
	for _, job := range jobs {
		err := m.db.CreateJob(job)
		if err != nil {
			if errors.Is(err, db.ErrJobAlreadyExists) {
				continue
			}
			slog.Error("Failed to create job", "location", locationID, "job_id", job.ID, "error", err)
			continue
		}
		newJobs++
	}
	if m.srv != nil {
		m.srv.RefreshQueueDepth()
	}
	if newJobs > 0 {
		slog.Info("Inserted new jobs from scan", "location", locationID, "count", newJobs)
	}
}

// listenWatcherEvents processes jobs emitted by a location's fsnotify watcher.
func (m *LocationManager) listenWatcherEvents(rt *locationRuntime, w *scanner.Watcher, ctx context.Context) {
	defer m.wg.Done()
	defer func() { _ = w.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-w.Jobs:
			if !ok {
				return
			}
			err := m.db.CreateJob(job)
			if err != nil {
				if !errors.Is(err, db.ErrJobAlreadyExists) {
					slog.Error("Failed to insert job from watcher", "location", rt.loc.ID, "job_id", job.ID, "error", err)
				}
			} else {
				slog.Info("Inserted job from watcher", "location", rt.loc.ID, "job_id", job.ID)
				if m.srv != nil {
					m.srv.RefreshQueueDepth()
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Error("Watcher reported error", "location", rt.loc.ID, "error", err)
		}
	}
}
