// Package db provides database tracking for jobs and workers.
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	// SQLite driver for database/sql
	_ "github.com/mattn/go-sqlite3"
)

// Tracker manages job and worker state in SQLite database
type Tracker struct {
	db *sql.DB
}

// ConnectionPoolConfig holds configuration for database connection pooling
type ConnectionPoolConfig struct {
	MaxOpenConnections int           // Maximum number of open connections to the database
	MaxIdleConnections int           // Maximum number of idle connections in the pool
	ConnMaxLifetime    time.Duration // Maximum lifetime of a connection (0 = unlimited)
	ConnMaxIdleTime    time.Duration // Maximum idle time of a connection (0 = unlimited)
}

// DefaultConnectionPoolConfig returns default connection pool configuration
func DefaultConnectionPoolConfig() ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MaxOpenConnections: 25,                 // SQLite typically works well with 25 connections
		MaxIdleConnections: 5,                  // Keep 5 idle connections ready
		ConnMaxLifetime:    time.Hour,          // Recycle connections after 1 hour
		ConnMaxIdleTime:    10 * time.Minute,   // Close idle connections after 10 minutes
	}
}

// New creates a new database tracker instance with default connection pool configuration
func New(dbPath string) (*Tracker, error) {
	return NewWithConfig(dbPath, DefaultConnectionPoolConfig())
}

// NewWithConfig creates a new database tracker instance with custom connection pool configuration
func NewWithConfig(dbPath string, poolConfig ConnectionPoolConfig) (*Tracker, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	if poolConfig.MaxOpenConnections > 0 {
		db.SetMaxOpenConns(poolConfig.MaxOpenConnections)
		slog.Info("Database connection pool configured",
			"max_open_connections", poolConfig.MaxOpenConnections)
	}
	
	if poolConfig.MaxIdleConnections > 0 {
		db.SetMaxIdleConns(poolConfig.MaxIdleConnections)
		slog.Info("Database idle connections configured",
			"max_idle_connections", poolConfig.MaxIdleConnections)
	}
	
	if poolConfig.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
		slog.Info("Database connection max lifetime configured",
			"conn_max_lifetime", poolConfig.ConnMaxLifetime)
	}
	
	if poolConfig.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(poolConfig.ConnMaxIdleTime)
		slog.Info("Database connection max idle time configured",
			"conn_max_idle_time", poolConfig.ConnMaxIdleTime)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	tracker := &Tracker{db: db}
	if err := tracker.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return tracker, nil
}

func (t *Tracker) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		source_path TEXT NOT NULL,
		output_path TEXT NOT NULL,
		status TEXT NOT NULL,
		priority INTEGER DEFAULT 5,
		worker_id TEXT,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		error_message TEXT,
		retry_count INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		source_duration REAL,
		output_size INTEGER,
		created_at TIMESTAMP NOT NULL,
		source_checksum TEXT,
		output_checksum TEXT
	);

	CREATE TABLE IF NOT EXISTS workers (
		id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		last_heartbeat TIMESTAMP,
		vulkan_available BOOLEAN,
		active_jobs INTEGER DEFAULT 0,
		gpu_name TEXT,
		cpu_usage REAL,
		memory_usage REAL,
		status TEXT DEFAULT 'online'
	);

	CREATE TABLE IF NOT EXISTS job_progress (
		job_id TEXT PRIMARY KEY,
		worker_id TEXT NOT NULL,
		progress REAL DEFAULT 0,
		fps REAL DEFAULT 0,
		stage TEXT DEFAULT 'pending',
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_worker_id ON jobs(worker_id);
	CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
	CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority);
	CREATE INDEX IF NOT EXISTS idx_job_progress_worker ON job_progress(worker_id);
	`

	_, err := t.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}
	
	// Add checksum columns if they don't exist (migration)
	if err := t.migrateChecksumColumns(); err != nil {
		return fmt.Errorf("failed to migrate checksum columns: %w", err)
	}
	
	// Add worker status column if it doesn't exist (migration)
	if err := t.migrateWorkerStatusColumn(); err != nil {
		return fmt.Errorf("failed to migrate worker status column: %w", err)
	}
	
	// Add priority column if it doesn't exist (migration)
	if err := t.migratePriorityColumn(); err != nil {
		return fmt.Errorf("failed to migrate priority column: %w", err)
	}
	
	return nil
}

// migrateChecksumColumns adds checksum columns if they don't exist
func (t *Tracker) migrateChecksumColumns() error {
	// Check if source_checksum column exists
	var columnExists int
	err := t.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='source_checksum'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check for source_checksum column: %w", err)
	}

	if columnExists == 0 {
		slog.Info("Adding source_checksum column to jobs table")
		_, err := t.db.Exec(`ALTER TABLE jobs ADD COLUMN source_checksum TEXT`)
		if err != nil {
			return fmt.Errorf("failed to add source_checksum column: %w", err)
		}
	}

	// Check if output_checksum column exists
	err = t.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='output_checksum'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check for output_checksum column: %w", err)
	}

	if columnExists == 0 {
		slog.Info("Adding output_checksum column to jobs table")
		_, err := t.db.Exec(`ALTER TABLE jobs ADD COLUMN output_checksum TEXT`)
		if err != nil {
			return fmt.Errorf("failed to add output_checksum column: %w", err)
		}
	}

	return nil
}

// migrateWorkerStatusColumn adds status column to workers table if it doesn't exist
func (t *Tracker) migrateWorkerStatusColumn() error {
	// Check if status column exists in workers table
	var columnExists int
	err := t.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('workers') WHERE name='status'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check for status column: %w", err)
	}

	if columnExists == 0 {
		slog.Info("Adding status column to workers table")
		_, err := t.db.Exec(`ALTER TABLE workers ADD COLUMN status TEXT DEFAULT 'online'`)
		if err != nil {
			return fmt.Errorf("failed to add status column: %w", err)
		}
	}

	return nil
}

// migratePriorityColumn adds priority column to jobs table if it doesn't exist
func (t *Tracker) migratePriorityColumn() error {
	// Check if priority column exists in jobs table
	var columnExists int
	err := t.db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name='priority'
	`).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check for priority column: %w", err)
	}

	if columnExists == 0 {
		slog.Info("Adding priority column to jobs table")
		_, err := t.db.Exec(`ALTER TABLE jobs ADD COLUMN priority INTEGER DEFAULT 5`)
		if err != nil {
			return fmt.Errorf("failed to add priority column: %w", err)
		}
		
		// Create index on priority for efficient sorting
		slog.Info("Creating index on priority column")
		_, err = t.db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority)`)
		if err != nil {
			return fmt.Errorf("failed to create priority index: %w", err)
		}
	}

	return nil
}

// CreateJob inserts a new job into the database
// Returns error if insertion failed, nil if successful or if job already exists
func (t *Tracker) CreateJob(job *models.Job) error {
	result, err := t.db.Exec(`
		INSERT OR IGNORE INTO jobs (
			id, source_path, output_path, status, priority, retry_count,
			max_retries, created_at, source_checksum
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.SourcePath, job.OutputPath, job.Status, job.Priority,
		job.RetryCount, job.MaxRetries, job.CreatedAt, job.SourceChecksum)
	if err != nil {
		return fmt.Errorf("failed to execute insert: %w", err)
	}

	// Check if a row was actually inserted
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Return a sentinel error if job already existed
	if rowsAffected == 0 {
		return sql.ErrNoRows // Job already exists
	}

	return nil
}

// GetJobByID retrieves a job by its ID
func (t *Tracker) GetJobByID(jobID string) (*models.Job, error) {
	var job models.Job
	var startedAt, completedAt sql.NullTime

	err := t.db.QueryRow(`
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
		COALESCE(source_duration, 0), COALESCE(output_size, 0),
		COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
	FROM jobs WHERE id = ?
`, jobID).Scan(&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.Priority, &job.CreatedAt,
		&job.WorkerID, &job.RetryCount, &job.MaxRetries,
		&startedAt, &completedAt, &job.ErrorMessage,
		&job.SourceDuration, &job.OutputSize,
		&job.SourceChecksum, &job.OutputChecksum)

	if err != nil {
		return nil, fmt.Errorf("failed to scan job row: %w", err)
	}

	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

// GetNextPendingJob retrieves the next pending job from the queue
func (t *Tracker) GetNextPendingJob() (*models.Job, error) {
	var job models.Job
	var startedAt, completedAt sql.NullTime

	err := t.db.QueryRow(`
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs WHERE status = 'pending'
		ORDER BY priority DESC, created_at ASC LIMIT 1
	`).Scan(&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.Priority, &job.CreatedAt,
		&job.WorkerID, &job.RetryCount, &job.MaxRetries,
		&startedAt, &completedAt, &job.ErrorMessage,
		&job.SourceDuration, &job.OutputSize,
		&job.SourceChecksum, &job.OutputChecksum)

	if err != nil {
		return nil, fmt.Errorf("failed to scan job row: %w", err)
	}

	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

// UpdateJob updates an existing job in the database
func (t *Tracker) UpdateJob(job *models.Job) error {
	_, err := t.db.Exec(`
		UPDATE jobs SET
			status = ?, priority = ?, worker_id = ?, started_at = ?,
			completed_at = ?, error_message = ?, retry_count = ?,
			source_duration = ?, output_size = ?,
			source_checksum = ?, output_checksum = ?
		WHERE id = ?
	`, job.Status, job.Priority, job.WorkerID, job.StartedAt, job.CompletedAt,
		job.ErrorMessage, job.RetryCount, job.SourceDuration,
		job.OutputSize, job.SourceChecksum, job.OutputChecksum, job.ID)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}
	return nil
}

// UpdateWorkerHeartbeat updates worker status in the database
func (t *Tracker) UpdateWorkerHeartbeat(hb *models.WorkerHeartbeat) error {
	_, err := t.db.Exec(`
		INSERT INTO workers (
			id, hostname, last_heartbeat, vulkan_available,
			active_jobs, gpu_name, cpu_usage, memory_usage, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_heartbeat = excluded.last_heartbeat,
			active_jobs = excluded.active_jobs,
			cpu_usage = excluded.cpu_usage,
			memory_usage = excluded.memory_usage,
			status = excluded.status
	`, hb.WorkerID, hb.Hostname, hb.Timestamp, hb.VulkanAvailable,
		hb.ActiveJobs, hb.GPU, hb.CPUUsage, hb.MemoryUsage, hb.Status)
	if err != nil {
		return fmt.Errorf("failed to upsert worker heartbeat: %w", err)
	}
	return nil
}

// GetJobStats returns statistics about job statuses
func (t *Tracker) GetJobStats() (map[string]any, error) {
	stats := make(map[string]any)

	rows, err := t.db.Query(`
		SELECT status, COUNT(*) as count
		FROM jobs
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query job stats: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			slog.Warn("Failed to scan job stats row", "error", err)
			continue
		}
		stats[status] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job stats rows: %w", err)
	}

	return stats, nil
}

// GetWorkers returns all workers from the database
func (t *Tracker) GetWorkers() ([]*models.WorkerHeartbeat, error) {
	rows, err := t.db.Query(`
		SELECT id, hostname, last_heartbeat, vulkan_available,
			active_jobs, gpu_name, cpu_usage, memory_usage,
			COALESCE(status, 'online') as status
		FROM workers
		ORDER BY last_heartbeat DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query workers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var workers []*models.WorkerHeartbeat
	for rows.Next() {
		var w models.WorkerHeartbeat
		var lastHeartbeat sql.NullTime
		if err := rows.Scan(
			&w.WorkerID, &w.Hostname, &lastHeartbeat, &w.VulkanAvailable,
			&w.ActiveJobs, &w.GPU, &w.CPUUsage, &w.MemoryUsage, &w.Status,
		); err != nil {
			slog.Warn("Failed to scan worker row", "error", err)
			continue
		}
		if lastHeartbeat.Valid {
			w.Timestamp = lastHeartbeat.Time
		}
		workers = append(workers, &w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating worker rows: %w", err)
	}

	return workers, nil
}

// GetJobsByStatus retrieves all jobs with a specific status
func (t *Tracker) GetJobsByStatus(status string, limit int) ([]*models.Job, error) {
	// Validate limit parameter to prevent SQL injection
	if limit < 0 {
		return nil, fmt.Errorf("limit must be non-negative")
	}

	query := `
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs WHERE status = ?
		ORDER BY created_at DESC
	`
	
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := t.db.Query(query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs by status: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
			&job.WorkerID, &job.RetryCount, &job.MaxRetries,
			&startedAt, &completedAt, &job.ErrorMessage,
			&job.SourceDuration, &job.OutputSize,
			&job.SourceChecksum, &job.OutputChecksum,
		); err != nil {
			slog.Warn("Failed to scan job row", "error", err)
			continue
		}
		
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}
		
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

// GetJobHistory retrieves job history within a time range
func (t *Tracker) GetJobHistory(startTime, endTime string, limit int) ([]*models.Job, error) {
	// Validate limit parameter to prevent SQL injection
	if limit < 0 {
		return nil, fmt.Errorf("limit must be non-negative")
	}

	query := `
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs
		WHERE created_at BETWEEN ? AND ?
		ORDER BY created_at DESC
	`
	
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := t.db.Query(query, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query job history: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
			&job.WorkerID, &job.RetryCount, &job.MaxRetries,
			&startedAt, &completedAt, &job.ErrorMessage,
			&job.SourceDuration, &job.OutputSize,
			&job.SourceChecksum, &job.OutputChecksum,
		); err != nil {
			slog.Warn("Failed to scan job row", "error", err)
			continue
		}
		
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}
		
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

// GetJobMetrics returns aggregated metrics for jobs
func (t *Tracker) GetJobMetrics() (map[string]any, error) {
	metrics := make(map[string]any)

	// Get total conversion time
	var totalDuration sql.NullFloat64
	err := t.db.QueryRow(`
		SELECT SUM(
			JULIANDAY(completed_at) - JULIANDAY(started_at)
		) * 24 * 60 * 60 as total_duration_seconds
		FROM jobs
		WHERE status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
	`).Scan(&totalDuration)
	
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate total duration: %w", err)
	}
	
	if totalDuration.Valid {
		metrics["total_conversion_time_seconds"] = totalDuration.Float64
	} else {
		metrics["total_conversion_time_seconds"] = 0.0
	}

	// Get average conversion time
	var avgDuration sql.NullFloat64
	err = t.db.QueryRow(`
		SELECT AVG(
			JULIANDAY(completed_at) - JULIANDAY(started_at)
		) * 24 * 60 * 60 as avg_duration_seconds
		FROM jobs
		WHERE status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
	`).Scan(&avgDuration)
	
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate average duration: %w", err)
	}
	
	if avgDuration.Valid {
		metrics["average_conversion_time_seconds"] = avgDuration.Float64
	} else {
		metrics["average_conversion_time_seconds"] = 0.0
	}

	// Get total output size
	var totalSize sql.NullInt64
	err = t.db.QueryRow(`
		SELECT SUM(output_size) as total_size
		FROM jobs
		WHERE status = 'completed'
	`).Scan(&totalSize)
	
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate total size: %w", err)
	}
	
	if totalSize.Valid {
		metrics["total_output_size_bytes"] = totalSize.Int64
	} else {
		metrics["total_output_size_bytes"] = int64(0)
	}

	// Get job counts by status
	statusCounts := make(map[string]int)
	rows, err := t.db.Query(`
		SELECT status, COUNT(*) as count
		FROM jobs
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query status counts: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			slog.Warn("Failed to scan status count", "error", err)
			continue
		}
		statusCounts[status] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating status rows: %w", err)
	}

	metrics["status_counts"] = statusCounts

	return metrics, nil
}

// GetActiveWorkers returns workers that have sent a heartbeat within the threshold
func (t *Tracker) GetActiveWorkers(heartbeatThresholdSeconds int) ([]*models.WorkerHeartbeat, error) {
	query := `
		SELECT id, hostname, last_heartbeat, vulkan_available,
			active_jobs, gpu_name, cpu_usage, memory_usage,
			COALESCE(status, 'online') as status
		FROM workers
		WHERE last_heartbeat >= datetime('now', '-' || ? || ' seconds')
		ORDER BY last_heartbeat DESC
	`

	rows, err := t.db.Query(query, heartbeatThresholdSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to query active workers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var workers []*models.WorkerHeartbeat
	for rows.Next() {
		var w models.WorkerHeartbeat
		var lastHeartbeat sql.NullTime
		if err := rows.Scan(
			&w.WorkerID, &w.Hostname, &lastHeartbeat, &w.VulkanAvailable,
			&w.ActiveJobs, &w.GPU, &w.CPUUsage, &w.MemoryUsage, &w.Status,
		); err != nil {
			slog.Warn("Failed to scan worker row", "error", err)
			continue
		}
		if lastHeartbeat.Valid {
			w.Timestamp = lastHeartbeat.Time
		}
		workers = append(workers, &w)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating worker rows: %w", err)
	}

	return workers, nil
}

// MarkWorkerOffline marks a worker as offline in the database
func (t *Tracker) MarkWorkerOffline(workerID string) error {
	_, err := t.db.Exec(`
		UPDATE workers
		SET status = 'offline'
		WHERE id = ?
	`, workerID)
	if err != nil {
		return fmt.Errorf("failed to mark worker offline: %w", err)
	}
	return nil
}

// GetWorkerStats returns aggregated statistics for workers
func (t *Tracker) GetWorkerStats() (map[string]any, error) {
	stats := make(map[string]any)

	// Get total workers
	var totalWorkers int
	err := t.db.QueryRow(`SELECT COUNT(*) FROM workers`).Scan(&totalWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to count workers: %w", err)
	}
	stats["total_workers"] = totalWorkers

	// Get workers with Vulkan support
	var vulkanWorkers int
	err = t.db.QueryRow(`
		SELECT COUNT(*) FROM workers WHERE vulkan_available = 1
	`).Scan(&vulkanWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to count vulkan workers: %w", err)
	}
	stats["vulkan_workers"] = vulkanWorkers

	// Get average active jobs
	var avgActiveJobs sql.NullFloat64
	err = t.db.QueryRow(`
		SELECT AVG(active_jobs) FROM workers
	`).Scan(&avgActiveJobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate average active jobs: %w", err)
	}
	if avgActiveJobs.Valid {
		stats["average_active_jobs"] = avgActiveJobs.Float64
	} else {
		stats["average_active_jobs"] = 0.0
	}

	// Get average CPU usage
	var avgCPUUsage sql.NullFloat64
	err = t.db.QueryRow(`
		SELECT AVG(cpu_usage) FROM workers WHERE cpu_usage > 0
	`).Scan(&avgCPUUsage)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate average CPU usage: %w", err)
	}
	if avgCPUUsage.Valid {
		stats["average_cpu_usage"] = avgCPUUsage.Float64
	} else {
		stats["average_cpu_usage"] = 0.0
	}

	// Get average memory usage
	var avgMemoryUsage sql.NullFloat64
	err = t.db.QueryRow(`
		SELECT AVG(memory_usage) FROM workers WHERE memory_usage > 0
	`).Scan(&avgMemoryUsage)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to calculate average memory usage: %w", err)
	}
	if avgMemoryUsage.Valid {
		stats["average_memory_usage"] = avgMemoryUsage.Float64
	} else {
		stats["average_memory_usage"] = 0.0
	}

	return stats, nil
}

// GetStaleProcessingJobs returns jobs stuck in processing state longer than timeout
func (t *Tracker) GetStaleProcessingJobs(timeoutSeconds int) ([]*models.Job, error) {
	query := `
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs
		WHERE status = 'processing'
		AND started_at IS NOT NULL
		AND started_at <= datetime('now', '-' || ? || ' seconds')
		ORDER BY started_at ASC
	`

	rows, err := t.db.Query(query, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale jobs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
			&job.WorkerID, &job.RetryCount, &job.MaxRetries,
			&startedAt, &completedAt, &job.ErrorMessage,
			&job.SourceDuration, &job.OutputSize,
			&job.SourceChecksum, &job.OutputChecksum,
		); err != nil {
			slog.Warn("Failed to scan job row", "error", err)
			continue
		}

		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

// GetRetryableFailedJobs returns failed jobs that can be retried
func (t *Tracker) GetRetryableFailedJobs() ([]*models.Job, error) {
	query := `
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs
		WHERE status = 'failed'
		AND retry_count < max_retries
		ORDER BY created_at ASC
	`

	rows, err := t.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query retryable jobs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
			&job.WorkerID, &job.RetryCount, &job.MaxRetries,
			&startedAt, &completedAt, &job.ErrorMessage,
			&job.SourceDuration, &job.OutputSize,
			&job.SourceChecksum, &job.OutputChecksum,
		); err != nil {
			slog.Warn("Failed to scan job row", "error", err)
			continue
		}

		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

// GetJobsForWorker returns all jobs assigned to a worker
func (t *Tracker) GetJobsForWorker(workerID string) ([]*models.Job, error) {
	query := `
		SELECT id, source_path, output_path, status, COALESCE(priority, 5), created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0),
			COALESCE(source_checksum, ''), COALESCE(output_checksum, '')
		FROM jobs
		WHERE worker_id = ? AND status = 'processing'
		ORDER BY started_at ASC
	`

	rows, err := t.db.Query(query, workerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query worker jobs: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("Failed to close rows", "error", err)
		}
	}()

	var jobs []*models.Job
	for rows.Next() {
		var job models.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
			&job.WorkerID, &job.RetryCount, &job.MaxRetries,
			&startedAt, &completedAt, &job.ErrorMessage,
			&job.SourceDuration, &job.OutputSize,
			&job.SourceChecksum, &job.OutputChecksum,
		); err != nil {
			slog.Warn("Failed to scan job row", "error", err)
			continue
		}

		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating job rows: %w", err)
	}

	return jobs, nil
}

// ResetJobToPending resets a job status to pending for retry
func (t *Tracker) ResetJobToPending(jobID string, incrementRetry bool) error {
	var err error
	if incrementRetry {
		_, err = t.db.Exec(`
			UPDATE jobs
			SET status = 'pending', worker_id = '', error_message = '', retry_count = retry_count + 1
			WHERE id = ?
		`, jobID)
	} else {
		_, err = t.db.Exec(`
			UPDATE jobs
			SET status = 'pending', worker_id = '', error_message = ''
			WHERE id = ?
		`, jobID)
	}

	if err != nil {
		return fmt.Errorf("failed to reset job to pending: %w", err)
	}
	return nil
}

// Close closes the database connection
func (t *Tracker) Close() error {
	if err := t.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// UpdateJobProgress updates the progress of a job
func (t *Tracker) UpdateJobProgress(progress *models.JobProgress) error {
	_, err := t.db.Exec(`
		INSERT INTO job_progress (job_id, worker_id, progress, fps, stage, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
			progress = excluded.progress,
			fps = excluded.fps,
			stage = excluded.stage,
			updated_at = excluded.updated_at
	`, progress.JobID, progress.WorkerID, progress.Progress, progress.FPS,
		progress.Stage, progress.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert job progress: %w", err)
	}
	return nil
}

// GetJobProgress retrieves the progress of a job
func (t *Tracker) GetJobProgress(jobID string) (*models.JobProgress, error) {
	var progress models.JobProgress
	err := t.db.QueryRow(`
		SELECT job_id, worker_id, progress, fps, stage, updated_at
		FROM job_progress
		WHERE job_id = ?
	`, jobID).Scan(&progress.JobID, &progress.WorkerID, &progress.Progress,
		&progress.FPS, &progress.Stage, &progress.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get job progress: %w", err)
	}
	return &progress, nil
}

// DeleteJobProgress deletes the progress record for a job
func (t *Tracker) DeleteJobProgress(jobID string) error {
	_, err := t.db.Exec(`DELETE FROM job_progress WHERE job_id = ?`, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete job progress: %w", err)
	}
	return nil
}
