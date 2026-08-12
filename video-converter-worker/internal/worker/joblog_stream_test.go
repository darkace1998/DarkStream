package worker

import (
	"testing"
)

// The log used to be sent to the master once, after the job ended, so opening
// the log view for a job that was still converting showed nothing at all. A
// periodic flush needs to know whether anything has actually changed, or it
// would resend the same log every few seconds for the life of the job.
func TestRevisionAdvancesOnlyWhenSomethingIsRecorded(t *testing.T) {
	log := NewJobLog()

	if log.Revision() != 0 {
		t.Fatalf("Revision() = %d on a new log, want 0", log.Revision())
	}

	log.Info("convert", "first line")
	first := log.Revision()
	if first == 0 {
		t.Fatal("Revision() did not advance after recording a line")
	}

	// Blank lines are dropped, so they must not make an unchanged log look new.
	log.Info("convert", "   ")
	log.Info("convert", "")
	if log.Revision() != first {
		t.Errorf("Revision() = %d after dropped lines, want %d", log.Revision(), first)
	}

	log.Info("convert", "second line")
	if log.Revision() <= first {
		t.Errorf("Revision() = %d after a second line, want more than %d", log.Revision(), first)
	}
}

// Once the log outgrows its cap it drops its middle, so entries are discarded
// while the log is still changing. The revision must keep advancing then, or a
// flush would stop shipping updates for exactly the long jobs that need them.
func TestRevisionAdvancesPastTheEntryCap(t *testing.T) {
	log := NewJobLog()

	for i := 0; i < maxJobLogEntries*2; i++ {
		log.Info("convert", "line")
	}

	entries := log.Entries()
	if len(entries) > maxJobLogEntries+1 { // +1 for the omission marker
		t.Errorf("kept %d entries, want no more than the %d cap", len(entries), maxJobLogEntries)
	}
	if got := log.Revision(); got != uint64(maxJobLogEntries*2) {
		t.Errorf("Revision() = %d, want %d — one per recorded line", got, maxJobLogEntries*2)
	}
}

func TestNilJobLogRevisionIsSafe(t *testing.T) {
	var log *JobLog

	if log.Revision() != 0 {
		t.Error("Revision() on a nil log should be 0 rather than panic")
	}
}

// streamJobLog must return promptly even when there is no master to talk to,
// so a job is never held up by its own diagnostics.
func TestStreamJobLogWithoutMasterIsANoop(t *testing.T) {
	w := &Worker{}

	stop := w.streamJobLog("job1", NewJobLog())
	if stop == nil {
		t.Fatal("streamJobLog returned no stop function")
	}
	stop() // must not panic or block
}
