package worker

import (
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/models"
)

const (
	// maxJobLogEntries caps how many lines are kept for one job. FFmpeg can emit
	// thousands of lines, and the whole log is sent to the master in a single
	// request, so this is bounded deliberately.
	maxJobLogEntries = 300

	// maxJobLogMessage caps a single line. Some FFmpeg errors embed very long
	// filter graphs or option dumps.
	maxJobLogMessage = 2000
)

// JobLog collects the execution log for one job. It is safe for concurrent use:
// FFmpeg output is drained on its own goroutine while the job goroutine records
// stage transitions.
//
// When more than maxJobLogEntries lines arrive, the middle is dropped rather
// than the end: the first lines explain what was attempted and the last lines
// almost always contain the actual failure, so keeping both ends preserves the
// diagnostic value.
type JobLog struct {
	mu      sync.Mutex
	head    []models.JobLogEntry
	tail    []models.JobLogEntry
	dropped int
	// revision counts the entries recorded so far. A running job's log is
	// shipped to the master periodically, and comparing revisions is what lets
	// a flush skip a log that has not changed since the last one.
	revision uint64
}

// NewJobLog creates an empty log collector.
func NewJobLog() *JobLog {
	return &JobLog{}
}

// Add records one entry.
func (l *JobLog) Add(level, stage, message string) {
	if l == nil {
		return
	}

	// Only trailing newlines are stripped: FFmpeg indents continuation lines
	// (" libavcodec ...") and that structure is worth keeping. Whitespace-only
	// lines carry nothing, so they are dropped entirely.
	message = strings.TrimRight(message, "\r\n")
	if strings.TrimSpace(message) == "" {
		return
	}
	if len(message) > maxJobLogMessage {
		message = message[:maxJobLogMessage] + "…"
	}

	entry := models.JobLogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Stage:     stage,
		Message:   message,
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.revision++

	// Keep the first half as context, then slide a window over the rest so the
	// most recent lines — where the failure lands — are always retained.
	halfCap := maxJobLogEntries / 2
	if len(l.head) < halfCap {
		l.head = append(l.head, entry)
		return
	}

	l.tail = append(l.tail, entry)
	if len(l.tail) > halfCap {
		l.tail = l.tail[1:]
		l.dropped++
	}
}

// Revision reports how many entries have been recorded, including ones since
// dropped. It only ever increases, so an unchanged value means an unchanged log.
func (l *JobLog) Revision() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.revision
}

// AddDetected records a line whose severity is inferred from its content. FFmpeg
// writes everything to stderr with no level marker, so classifying here is what
// makes the actual failure stand out in the UI instead of being buried in a wall
// of uniform text.
func (l *JobLog) AddDetected(stage, line string) {
	l.Add(detectLevel(line), stage, line)
}

// detectLevel guesses a severity for an unstructured FFmpeg output line.
func detectLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error"),
		strings.Contains(lower, "failed"),
		strings.Contains(lower, "could not"),
		strings.Contains(lower, "cannot"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "no such file"),
		strings.Contains(lower, "not supported"):
		return models.JobLogError
	case strings.Contains(lower, "warning"),
		strings.Contains(lower, "deprecated"),
		strings.Contains(lower, "unsupported"):
		return models.JobLogWarn
	default:
		return models.JobLogInfo
	}
}

// Infof, Warnf and Errorf are convenience wrappers for the common levels.
func (l *JobLog) Info(stage, message string)  { l.Add(models.JobLogInfo, stage, message) }
func (l *JobLog) Warn(stage, message string)  { l.Add(models.JobLogWarn, stage, message) }
func (l *JobLog) Error(stage, message string) { l.Add(models.JobLogError, stage, message) }

// Entries returns the collected log. When lines were dropped, a marker is
// inserted so the gap is visible rather than silently misleading.
func (l *JobLog) Entries() []models.JobLogEntry {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]models.JobLogEntry, 0, len(l.head)+len(l.tail)+1)
	out = append(out, l.head...)

	if l.dropped > 0 {
		marker := "… " + itoa(l.dropped) + " earlier lines omitted …"
		out = append(out, models.JobLogEntry{
			Timestamp: time.Now(),
			Level:     models.JobLogInfo,
			Stage:     "",
			Message:   marker,
		})
	}

	out = append(out, l.tail...)
	return out
}

// itoa avoids pulling strconv in for a single conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
