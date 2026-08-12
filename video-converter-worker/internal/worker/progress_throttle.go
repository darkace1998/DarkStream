package worker

import (
	"sync"
	"time"
)

const (
	// progressMinInterval is the longest a job can go without reporting while it
	// is still moving, so the dashboard stays live.
	progressMinInterval = 2 * time.Second

	// progressMinDelta is how much progress must advance before an update is
	// worth sending ahead of the interval.
	progressMinDelta = 1.0
)

// progressThrottle decides which progress updates are worth sending.
//
// FFmpeg emits progress several times a second per job, and every one of those
// was being POSTed to the master. With a couple of jobs running that produced
// tens of thousands of requests, which exhausted the master's shared rate limit
// and starved the calls that actually matter — heartbeats, job dispatch and
// transfers — leaving a busy worker reported as offline.
//
// Updates are still sent promptly when something meaningful changes: the stage,
// a percent of progress, or completion. Only the redundant repeats are dropped.
type progressThrottle struct {
	mu   sync.Mutex
	last map[string]progressSample
}

type progressSample struct {
	at       time.Time
	progress float64
	stage    string
}

func newProgressThrottle() *progressThrottle {
	return &progressThrottle{last: make(map[string]progressSample)}
}

// allow reports whether this update should be sent, recording it when so.
func (p *progressThrottle) allow(jobID string, progress float64, stage string) bool {
	if p == nil {
		return true
	}

	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	previous, seen := p.last[jobID]

	send := !seen ||
		stage != previous.stage || // a new phase is always worth reporting
		progress >= 100 || // never drop the final update
		progress-previous.progress >= progressMinDelta ||
		now.Sub(previous.at) >= progressMinInterval

	// Progress can go backwards when a job is retried; treat that as new.
	if progress < previous.progress {
		send = true
	}

	if send {
		p.last[jobID] = progressSample{at: now, progress: progress, stage: stage}
	}
	return send
}

// forget drops a job's state once it has finished, so the map does not grow for
// the lifetime of the worker.
func (p *progressThrottle) forget(jobID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.last, jobID)
}
