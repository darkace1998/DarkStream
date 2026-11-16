package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newAuditTestTracker(t *testing.T) *Tracker {
	t.Helper()
	tracker, err := New(filepath.Join(t.TempDir(), "audit.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker
}

func TestPing(t *testing.T) {
	tracker := newAuditTestTracker(t)
	require.NoError(t, tracker.Ping())
}

func TestRecordAndGetAuditLog(t *testing.T) {
	tracker := newAuditTestTracker(t)

	entries, err := tracker.GetAuditLog(10)
	require.NoError(t, err)
	require.Empty(t, entries)

	for i := 1; i <= 5; i++ {
		err := tracker.RecordAudit("master-key", "action."+string(rune('a'+i-1)), "detail")
		require.NoError(t, err)
	}

	entries, err = tracker.GetAuditLog(10)
	require.NoError(t, err)
	require.Len(t, entries, 5)
	// Most recent first.
	require.Equal(t, "action.e", entries[0].Action)
	require.Equal(t, "action.a", entries[4].Action)
	require.Equal(t, "master-key", entries[0].Actor)
	require.Equal(t, "detail", entries[0].Detail)
	require.False(t, entries[0].TS.IsZero())

	// Limit is honored and still returns newest first.
	entries, err = tracker.GetAuditLog(2)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "action.e", entries[0].Action)
	require.Equal(t, "action.d", entries[1].Action)

	// Non-positive limit falls back to a sane default instead of failing.
	entries, err = tracker.GetAuditLog(0)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
}
