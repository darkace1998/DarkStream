package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// maxChunkBodySize caps a single chunk upload body (256 MiB).
const maxChunkBodySize = 256 << 20

// chunkUploadState tracks one in-progress chunked upload.
type chunkUploadState struct {
	mu       sync.Mutex
	jobID    string
	partPath string
	total    int64
	received int64
	offsets  map[int64]int64 // offset -> length
}

// UploadChunk handles POST /api/worker/upload-chunk?job_id=&worker_id=&offset=&total_size=
// with a raw chunk body. Chunks are written at their offset into a .part file;
// when the received byte count reaches total_size the file is renamed into
// place and the job is completed exactly like a single-stream upload.
func (s *Server) UploadChunk(w http.ResponseWriter, r *http.Request) {
	s.audit(r, "worker.upload-chunk", r.URL.RawQuery)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID, offset, totalSize, ok := parseChunkParams(w, r)
	if !ok {
		return
	}

	job, validatedPath, ok := s.resolveChunkTarget(w, jobID)
	if !ok {
		return
	}

	chunk, ok := readChunkBody(w, r, offset, totalSize)
	if !ok {
		return
	}

	st, err := s.getChunkState(job, validatedPath, totalSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	complete, err := st.writeChunk(offset, chunk)
	if err != nil {
		slog.Error("Failed to write chunk", fieldJobID, jobID, "offset", offset, "error", err)
		http.Error(w, "Failed to write chunk", http.StatusInternalServerError)
		return
	}

	if !complete {
		w.Header().Set("Content-Type", contentTypeJSON)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"received": st.progress(),
			"complete": false,
		}); err != nil {
			slog.Error("Failed to encode chunk response", "error", err)
		}
		return
	}

	// All chunks received: finalize the upload like a single-stream upload.
	if err := st.finalize(); err != nil {
		slog.Error("Failed to finalize chunked upload", fieldJobID, jobID, "error", err)
		http.Error(w, "Failed to finalize upload", http.StatusInternalServerError)
		return
	}
	s.removeChunkState(jobID)

	outputChecksum, err := utils.CalculateFileSHA256(validatedPath)
	if err != nil {
		slog.Error("Failed to calculate output checksum", "path", validatedPath, "error", err)
		outputChecksum = ""
	}

	updated, err := s.db.MarkJobCompleted(job.ID, job.WorkerID, totalSize, outputChecksum, job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", fieldJobID, jobID, "error", err)
		http.Error(w, "Failed to update job status", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while uploading", http.StatusConflict)
		return
	}

	s.finalizeUploadedJob(job, validatedPath, outputChecksum, nil)
	s.recordJobCompletion(job)
	slog.Info("Chunked upload completed", fieldJobID, jobID, "size", totalSize, "checksum", outputChecksum, "output", validatedPath)

	w.Header().Set("Content-Type", contentTypeJSON)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"received":  totalSize,
		"complete":  true,
		"file_size": totalSize,
		fieldStatus: statusCompleted,
	}); err != nil {
		slog.Error("Failed to encode chunk response", "error", err)
	}
}

// getChunkState returns the upload state for a job, creating the .part file
// on first use.
func (s *Server) getChunkState(job *models.Job, outputPath string, totalSize int64) (*chunkUploadState, error) {
	s.chunkMu.Lock()
	defer s.chunkMu.Unlock()

	if st, ok := s.chunkUploads[job.ID]; ok {
		if st.total != totalSize {
			return nil, fmt.Errorf("total_size changed for in-progress upload")
		}
		return st, nil
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	partPath := outputPath + ".part"
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to create part file: %w", err)
	}
	if err := f.Truncate(totalSize); err != nil { // preallocate
		_ = f.Close()
		return nil, fmt.Errorf("failed to preallocate part file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close part file: %w", err)
	}

	st := &chunkUploadState{
		jobID:    job.ID,
		partPath: partPath,
		total:    totalSize,
		offsets:  map[int64]int64{},
	}
	s.chunkUploads[job.ID] = st
	return st, nil
}

func (s *Server) removeChunkState(jobID string) {
	s.chunkMu.Lock()
	defer s.chunkMu.Unlock()
	delete(s.chunkUploads, jobID)
}

// writeChunk writes one chunk at its offset and reports whether the upload is
// now complete (received bytes == total, no gaps).
func (st *chunkUploadState) writeChunk(offset int64, chunk []byte) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	f, err := os.OpenFile(st.partPath, os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteAt(chunk, offset); err != nil {
		return false, err
	}

	if _, dup := st.offsets[offset]; !dup {
		st.offsets[offset] = int64(len(chunk))
		st.received += int64(len(chunk))
	}

	// Complete only when every byte [0,total) is covered exactly once.
	if st.received < st.total {
		return false, nil
	}
	offs := make([]int64, 0, len(st.offsets))
	for off := range st.offsets {
		offs = append(offs, off)
	}
	slices.Sort(offs)
	covered := int64(0)
	for _, off := range offs {
		if off != covered { // gap or overlap
			return false, nil
		}
		covered += st.offsets[off]
	}
	return covered == st.total, nil
}

// progress returns the received byte count (called with state lock held or not;
// uses its own lock).
func (st *chunkUploadState) progress() int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.received
}

// finalize renames the completed .part file into its final output path.
func (st *chunkUploadState) finalize() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := os.Rename(st.partPath, st.partPath[:len(st.partPath)-len(".part")]); err != nil {
		return err
	}
	if err := os.Chmod(st.partPath[:len(st.partPath)-len(".part")], 0o644); err != nil {
		return err
	}
	return nil
}

// parseChunkParams validates the query parameters of a chunk upload request.
func parseChunkParams(w http.ResponseWriter, r *http.Request) (jobID string, offset, totalSize int64, ok bool) {
	jobID = r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return "", 0, 0, false
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		http.Error(w, "Invalid offset parameter", http.StatusBadRequest)
		return "", 0, 0, false
	}
	totalSize, err = strconv.ParseInt(r.URL.Query().Get("total_size"), 10, 64)
	if err != nil || totalSize <= 0 {
		http.Error(w, "Invalid total_size parameter", http.StatusBadRequest)
		return "", 0, 0, false
	}
	return jobID, offset, totalSize, true
}

// resolveChunkTarget loads the job and validates its output path.
func (s *Server) resolveChunkTarget(w http.ResponseWriter, jobID string) (*models.Job, string, bool) {
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return nil, "", false
	}
	if job.Status != statusProcessing {
		http.Error(w, "Job is not in processing status", http.StatusBadRequest)
		return nil, "", false
	}
	validatedPath, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), job.OutputPath)
	if err != nil {
		slog.Error("Path validation failed for chunked upload", fieldJobID, jobID, "path", job.OutputPath, "error", err)
		http.Error(w, "Invalid file path", http.StatusForbidden)
		return nil, "", false
	}
	return job, validatedPath, true
}

// readChunkBody reads and validates the raw chunk body.
func readChunkBody(w http.ResponseWriter, r *http.Request, offset, totalSize int64) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxChunkBodySize)
	chunk, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read chunk body", http.StatusBadRequest)
		return nil, false
	}
	if len(chunk) == 0 {
		http.Error(w, "Empty chunk", http.StatusBadRequest)
		return nil, false
	}
	if offset+int64(len(chunk)) > totalSize {
		http.Error(w, "Chunk exceeds total_size", http.StatusBadRequest)
		return nil, false
	}
	return chunk, true
}
