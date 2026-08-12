package worker

import (
	"testing"
	"time"
)

// FFmpeg reports progress several times a second. Forwarding every one of them
// exhausted the master's rate limit and starved heartbeats and job dispatch, so
// the repeats must be dropped while anything meaningful still gets through.
func TestThrottleDropsRedundantUpdates(t *testing.T) {
	p := newProgressThrottle()

	if !p.allow("job1", 0, "convert") {
		t.Fatal("first update was dropped")
	}

	// A burst at effectively the same progress carries no information.
	sent := 0
	for i := 0; i < 50; i++ {
		if p.allow("job1", 0.1, "convert") {
			sent++
		}
	}
	if sent != 0 {
		t.Errorf("%d redundant updates were sent, want 0", sent)
	}
}

func TestThrottleAllowsMeaningfulProgress(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 10, "convert")

	if !p.allow("job1", 11, "convert") {
		t.Error("a full percent of progress was dropped")
	}
}

// A stage change is the most useful update there is, so it must never wait.
func TestThrottleAlwaysAllowsStageChange(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 50, "convert")

	if !p.allow("job1", 50, "upload") {
		t.Error("stage change was dropped")
	}
}

// Losing the final update would leave a job showing as unfinished.
func TestThrottleAlwaysAllowsCompletion(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 99.9, "convert")

	if !p.allow("job1", 100, "convert") {
		t.Error("completion update was dropped")
	}
}

// Even a job that appears stalled should refresh occasionally so the dashboard
// does not look frozen.
func TestThrottleAllowsAfterInterval(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 10, "convert")

	// Backdate the recorded sample rather than sleeping.
	p.mu.Lock()
	sample := p.last["job1"]
	sample.at = time.Now().Add(-progressMinInterval - time.Second)
	p.last["job1"] = sample
	p.mu.Unlock()

	if !p.allow("job1", 10, "convert") {
		t.Error("no update was sent after the refresh interval elapsed")
	}
}

// Progress restarting means the job was requeued; that is worth reporting.
func TestThrottleAllowsProgressGoingBackwards(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 80, "convert")

	if !p.allow("job1", 0, "convert") {
		t.Error("a restarted job was not reported")
	}
}

func TestThrottleTracksJobsIndependently(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 50, "convert")

	if !p.allow("job2", 1, "convert") {
		t.Error("a different job was throttled by the first job's state")
	}
}

// State must not accumulate for the lifetime of the worker.
func TestThrottleForgetsFinishedJobs(t *testing.T) {
	p := newProgressThrottle()
	p.allow("job1", 50, "convert")
	p.forget("job1")

	p.mu.Lock()
	_, present := p.last["job1"]
	p.mu.Unlock()

	if present {
		t.Error("state for a finished job was retained")
	}
}

func TestNilThrottleIsSafe(t *testing.T) {
	var p *progressThrottle

	if !p.allow("job1", 1, "convert") {
		t.Error("a nil throttle should permit everything rather than block reporting")
	}
	p.forget("job1") // must not panic
}
