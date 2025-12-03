// Package db provides database tracking for jobs and workers.
package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/darkace1998/video-converter-common/models"
	// SQLite driver for database/sql
	_ "github.com/mattn/go-sqlite3"
)

// Tracker manages job and worker state in SQLite database
type Tracker struct {
	db *sql.DB
}

// New creates a new database tracker instance
func New(dbPath string) (*Tracker, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
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
		worker_id TEXT,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		error_message TEXT,
		retry_count INTEGER DEFAULT 0,
		max_retries INTEGER DEFAULT 3,
		source_duration REAL,
		output_size INTEGER,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS workers (
		id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		last_heartbeat TIMESTAMP,
		vulkan_available BOOLEAN,
		active_jobs INTEGER DEFAULT 0,
		gpu_name TEXT,
		cpu_usage REAL,
		memory_usage REAL
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_worker_id ON jobs(worker_id);
	CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
	`

	_, err := t.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}
	return nil
}

// CreateJob inserts a new job into the database
// Returns error if insertion failed, nil if successful or if job already exists
func (t *Tracker) CreateJob(job *models.Job) error {
	result, err := t.db.Exec(`
		INSERT OR IGNORE INTO jobs (
			id, source_path, output_path, status, retry_count,
			max_retries, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.SourcePath, job.OutputPath, job.Status,
		job.RetryCount, job.MaxRetries, job.CreatedAt)

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
		SELECT id, source_path, output_path, status, created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
		COALESCE(source_duration, 0), COALESCE(output_size, 0)
	FROM jobs WHERE id = ?
`, jobID).Scan(&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
		&job.WorkerID, &job.RetryCount, &job.MaxRetries,
		&startedAt, &completedAt, &job.ErrorMessage,
		&job.SourceDuration, &job.OutputSize)

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
		SELECT id, source_path, output_path, status, created_at,
			COALESCE(worker_id, ''), retry_count, max_retries,
			started_at, completed_at, COALESCE(error_message, ''),
			COALESCE(source_duration, 0), COALESCE(output_size, 0)
		FROM jobs WHERE status = 'pending'
		ORDER BY created_at ASC LIMIT 1
	`).Scan(&job.ID, &job.SourcePath, &job.OutputPath, &job.Status, &job.CreatedAt,
		&job.WorkerID, &job.RetryCount, &job.MaxRetries,
		&startedAt, &completedAt, &job.ErrorMessage,
		&job.SourceDuration, &job.OutputSize)

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
			status = ?, worker_id = ?, started_at = ?,
			completed_at = ?, error_message = ?, retry_count = ?,
			source_duration = ?, output_size = ?
		WHERE id = ?
	`, job.Status, job.WorkerID, job.StartedAt, job.CompletedAt,
		job.ErrorMessage, job.RetryCount, job.SourceDuration,
		job.OutputSize, job.ID)
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
			active_jobs, gpu_name, cpu_usage, memory_usage
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_heartbeat = excluded.last_heartbeat,
			active_jobs = excluded.active_jobs,
			cpu_usage = excluded.cpu_usage,
			memory_usage = excluded.memory_usage
	`, hb.WorkerID, hb.Hostname, hb.Timestamp, hb.VulkanAvailable,
		hb.ActiveJobs, hb.GPU, hb.CPUUsage, hb.MemoryUsage)
	if err != nil {
		return fmt.Errorf("failed to upsert worker heartbeat: %w", err)
	}
	return nil
}

// GetJobStats returns statistics about job statuses
func (t *Tracker) GetJobStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

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

// Close closes the database connection
func (t *Tracker) Close() error {
	if err := t.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}
