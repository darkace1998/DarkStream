package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// maxJobLogTail is how much of a per-job log file is shipped to the master as
// the job's log tail (the master caps stored tails at 64 KiB).
const maxJobLogTail = 16 << 10 // 16 KiB

// multiHandler fans a log record out to several slog handlers, so a job's log
// can go to the worker's normal logger and to the per-job log file at once.
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiSlogHandler(hs ...slog.Handler) slog.Handler { return multiHandler{hs} }

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		_ = h.Handle(ctx, r)
	}
	return nil
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return multiHandler{hs}
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return multiHandler{hs}
}

// readLogTail returns the last maxJobLogTail bytes of the given log file, or ""
// when the file is missing/empty/unreadable.
func readLogTail(path string) string {
	f, err := os.Open(path) // #nosec G304 -- path is inside the worker cache dir, built from the job ID
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}
	off := max(info.Size()-maxJobLogTail, 0)
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(data)
}
