package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-common/utils"
)

// UploadVideo handles video file upload requests from workers
func (s *Server) UploadVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get job_id from query parameters
	jobID := r.URL.Query().Get(fieldJobID)
	if !validateJobID(jobID) {
		http.Error(w, "Invalid job_id parameter", http.StatusBadRequest)
		return
	}

	// Fetch the job
	job, err := s.db.GetJobByID(jobID)
	if err != nil {
		slog.Error("Failed to fetch job", fieldJobID, jobID, "error", err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if !s.checkWorkerBinding(w, r, job.WorkerID) {
		return
	}

	// Validate job is in processing status
	if job.Status != statusProcessing {
		slog.Warn("Upload rejected - job not in processing status", fieldJobID, jobID, fieldStatus, job.Status)
		http.Error(w, "Job is not in processing status", http.StatusBadRequest)
		return
	}

	// Validate output path to prevent path traversal attacks
	// This is defense-in-depth: paths are validated during job creation,
	// but we re-validate here to protect against database tampering
	validatedPath, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), job.OutputPath)
	if err != nil {
		slog.Error("Path validation failed for output file",
			fieldJobID, jobID,
			"path", job.OutputPath,
			"error", err)
		http.Error(w, "Invalid file path", http.StatusForbidden)
		return
	}

	// Use validated path for all file operations
	outputPath := validatedPath

	// If validated path differs from database path, update the job
	// This can happen if path normalization occurred (e.g., /path//file -> /path/file)
	if outputPath != job.OutputPath {
		slog.Info("Output path normalized during validation",
			fieldJobID, jobID,
			"original", job.OutputPath,
			"validated", outputPath)
		job.OutputPath = outputPath
	}

	// Stream the multipart body straight to its destination. ParseMultipartForm
	// would first spool the whole (multi-gigabyte) upload into the system temp
	// directory and only then copy it, doubling the I/O and requiring as much
	// free temp space as the largest video.
	multipartReader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}
	file, metadata, err := nextVideoPart(multipartReader)
	if err != nil {
		http.Error(w, "Failed to get video file from form", http.StatusBadRequest)
		return
	}
	defer func() {
		cerr := file.Close()
		if cerr != nil {
			slog.Warn("Failed to close uploaded file", "error", cerr)
		}
	}()

	// The container format is a runtime setting, so the file the worker produced
	// may not match the extension chosen when the job was created. Store it
	// under the extension it actually has.
	if adjusted := outputPathForUpload(outputPath, file.FileName()); adjusted != outputPath {
		validatedAdjusted, validateErr := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), adjusted)
		if validateErr != nil {
			slog.Error("Path validation failed for adjusted output file", fieldJobID, jobID, "path", adjusted, "error", validateErr)
			http.Error(w, "Invalid file path", http.StatusForbidden)
			return
		}
		outputPath = validatedAdjusted
	}

	bytesWritten, status, err := s.storeUpload(file, outputPath)
	if err != nil {
		slog.Error("Failed to store uploaded video", fieldJobID, jobID, "output", outputPath, "error", err)
		http.Error(w, err.Error(), status)
		return
	}

	// Calculate output file checksum for integrity validation
	outputChecksum, err := utils.CalculateFileSHA256(outputPath)
	if err != nil {
		slog.Error("Failed to calculate output checksum", "path", outputPath, "error", err)
		// Don't fail the upload if checksum calculation fails - log and continue
		outputChecksum = ""
	}

	// Update job status to completed (only after successful file write)
	updated, err := s.db.MarkJobCompleted(job.ID, job.WorkerID, bytesWritten, outputChecksum, job.StartedAt)
	if err != nil {
		slog.Error("Failed to update job", fieldJobID, jobID, "error", err)
		// File is saved but status update failed - this is acceptable
		// The job will remain in processing state and can be retried
		http.Error(w, "Failed to update job status", http.StatusInternalServerError)
		return
	}
	if !updated {
		http.Error(w, "Job state changed while uploading", http.StatusConflict)
		return
	}

	s.finalizeUploadedJob(job, outputPath, outputChecksum, metadata)
	s.recordJobCompletion(job)

	slog.Info("Video file uploaded", fieldJobID, jobID, "size", bytesWritten, "checksum", outputChecksum, "output", outputPath)

	// Return success response with file size
	w.Header().Set("Content-Type", contentTypeJSON)
	response := map[string]any{
		"file_size": bytesWritten,
		fieldStatus: statusCompleted,
	}
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		slog.Error("Failed to encode upload response", "error", err)
		return
	}
}

// outputPathForUpload returns outputPath with its extension replaced by that of
// the uploaded file, when the upload carries a different, supported container
// extension. Otherwise outputPath is returned unchanged.
func outputPathForUpload(outputPath, uploadName string) string {
	uploadExt := strings.ToLower(filepath.Ext(uploadName))
	currentExt := filepath.Ext(outputPath)
	if !uploadOutputExtensions[uploadExt] || strings.EqualFold(currentExt, uploadExt) {
		return outputPath
	}
	return strings.TrimSuffix(outputPath, currentExt) + uploadExt
}

// storeUpload writes the uploaded video to outputPath atomically (temp file in
// the same directory, then rename). On failure it returns the HTTP status to
// answer with; the returned error text is safe to send to the client.
func (s *Server) storeUpload(file io.Reader, outputPath string) (int64, int, error) {
	// Create output directory if needed
	outputDir := filepath.Dir(outputPath)
	// #nosec G703 - outputDir is derived from the validated output path
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		slog.Error("Failed to create output directory", "path", outputDir, "error", err)
		return 0, http.StatusInternalServerError, errors.New("Failed to create output directory") //nolint:staticcheck // HTTP error text
	}

	// Create a temporary file first to ensure atomic write
	tempFile, err := os.CreateTemp(outputDir, ".upload-*"+filepath.Ext(outputPath)+".tmp")
	if err != nil {
		slog.Error("Failed to create temp file", "error", err)
		return 0, http.StatusInternalServerError, errors.New("Failed to create temp file") //nolint:staticcheck // HTTP error text
	}
	tempPath := tempFile.Name()
	defer cleanupUploadTemp(tempFile, tempPath)

	// Copy the uploaded file to the temp file
	startUploadTime := time.Now()
	bytesWritten, err := io.Copy(tempFile, file)
	if bytesWritten > 0 {
		s.metrics.RecordBytesUploaded(bytesWritten)
		durationSecs := time.Since(startUploadTime).Seconds()
		if durationSecs > 0 {
			s.metrics.RecordTransferSpeed("upload", float64(bytesWritten)/durationSecs)
		}
	}
	if err != nil {
		slog.Error("Failed to write temp file", "error", err)
		return 0, http.StatusInternalServerError, errors.New("Failed to write output file") //nolint:staticcheck // HTTP error text
	}
	if bytesWritten == 0 {
		// Never replace the output (or mark the job done) with an empty file.
		return 0, http.StatusBadRequest, errEmptyUpload
	}

	// Close the temp file before renaming
	if err := tempFile.Close(); err != nil {
		slog.Error("Failed to close temp file before rename", "error", err)
		return 0, http.StatusInternalServerError, errors.New("Failed to finalize output file") //nolint:staticcheck // HTTP error text
	}

	// os.CreateTemp creates the file with mode 0600; converted videos are meant
	// to be consumed by other users/services (media servers, shares), so give
	// the result ordinary file permissions before publishing it.
	// #nosec G302 G703 - output videos are intentionally world-readable; tempPath comes from os.CreateTemp
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		slog.Warn("Failed to set permissions on output file", "path", tempPath, "error", chmodErr)
	}

	// Atomically rename temp file to final location
	// #nosec G703 - tempPath comes from os.CreateTemp and outputPath is the validated output path
	if err := os.Rename(tempPath, outputPath); err != nil {
		slog.Error("Failed to rename temp file to output path", "temp", tempPath, "output", outputPath, "error", err)
		return 0, http.StatusInternalServerError, errors.New("Failed to finalize output file") //nolint:staticcheck // HTTP error text
	}

	return bytesWritten, http.StatusOK, nil
}

// finalizeUploadedJob records the side information of a completed upload: the
// final output location, the source metadata reported by the worker, and the
// output checksum (so the scanner recognises the file as already converted).
// These are best effort; the job is already completed at this point.
func (s *Server) finalizeUploadedJob(job *models.Job, outputPath, outputChecksum string, metadata []byte) {
	if outputPath != job.OutputPath {
		if err := s.db.SetJobOutputPath(job.ID, outputPath); err != nil {
			slog.Warn("Failed to record adjusted output path", fieldJobID, job.ID, "error", err)
		}

		// Replace-source mode (output path == source path): the converted file
		// landed next to the original under a new extension, so remove the
		// original to complete the replacement.
		if job.OutputPath == job.SourcePath {
			if sourcePath, err := utils.ValidatePathInAllowedDirs(s.getAllowedDirs(), job.SourcePath); err == nil {
				// #nosec G703 - sourcePath was validated against the allowed directories
				if rmErr := os.Remove(sourcePath); rmErr != nil && !os.IsNotExist(rmErr) {
					slog.Warn("Failed to remove replaced source file", fieldJobID, job.ID, "path", sourcePath, "error", rmErr)
				}
			}
		}
	}

	if err := s.db.RecordKnownOutput(outputChecksum, outputPath); err != nil {
		slog.Warn("Failed to record output checksum", fieldJobID, job.ID, "error", err)
	}

	if len(metadata) == 0 {
		return
	}
	var reported models.Job
	if err := json.Unmarshal(metadata, &reported); err != nil {
		slog.Warn("Ignoring malformed job metadata from worker", fieldJobID, job.ID, "error", err)
		return
	}
	reported.ID = job.ID
	if err := s.db.UpdateJobSourceMetadata(&reported); err != nil {
		slog.Warn("Failed to store job metadata", fieldJobID, job.ID, "error", err)
	}
}

// recordJobCompletion records completion metrics and fires the "completed"
// webhook for a job that has just been marked completed. The upload is what
// completes a job in the normal worker flow (workers do not send a separate
// job-complete call), so both completion paths share this.
func (s *Server) recordJobCompletion(job *models.Job) {
	if job.StartedAt != nil {
		s.metrics.RecordJobCompleted(time.Since(*job.StartedAt).Seconds())
	}
	s.metrics.RecordJobFinished()

	if updatedJob, fetchErr := s.db.GetJobByID(job.ID); fetchErr == nil {
		s.notifier.Notify(statusCompleted, updatedJob)
	}
}

// cleanupUploadTemp closes the upload temp file and removes it if it was not
// renamed into place (i.e. the upload did not complete).
func cleanupUploadTemp(tempFile *os.File, tempPath string) {
	// The success path closes the file explicitly before renaming it.
	cerr := tempFile.Close()
	if cerr != nil && !errors.Is(cerr, os.ErrClosed) {
		slog.Warn("Failed to close temp file", "error", cerr)
	}
	// #nosec G703 - tempPath is created by os.CreateTemp inside the validated output directory
	if _, statErr := os.Stat(tempPath); statErr != nil {
		return
	}
	// #nosec G703 - tempPath is created by os.CreateTemp inside the validated output directory
	if rerr := os.Remove(tempPath); rerr != nil {
		slog.Warn("Failed to remove temp file", "path", tempPath, "error", rerr)
	}
}

// nextVideoPart advances the multipart stream to the "video" file part. A
// "metadata" field that precedes it (JSON source metadata from the worker) is
// returned alongside; other fields are skipped.
func nextVideoPart(reader *multipart.Reader) (*multipart.Part, []byte, error) {
	var metadata []byte
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, nil, fmt.Errorf("video part not found: %w", err)
		}
		switch part.FormName() {
		case "video":
			return part, metadata, nil
		case "metadata":
			data, readErr := io.ReadAll(io.LimitReader(part, maxUploadMetadataSize))
			if readErr == nil {
				metadata = data
			}
		}
		_ = part.Close()
	}
}
