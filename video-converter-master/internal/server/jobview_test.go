package server

import (
	"math"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
)

func TestNewJobViewResolvesByLibraryID(t *testing.T) {
	libs := []config.Library{
		{ID: "movies", Name: "Movies", Path: "/mnt/movies"},
	}
	byID := map[string]config.Library{"movies": libs[0]}

	view := newJobView(&models.Job{
		LibraryID:  "movies",
		SourcePath: "/mnt/movies/action/film.mkv",
	}, byID, libs)

	if view.LibraryName != "Movies" {
		t.Errorf("LibraryName = %q, want %q", view.LibraryName, "Movies")
	}
	if view.RelPath != "action/film.mkv" {
		t.Errorf("RelPath = %q, want %q", view.RelPath, "action/film.mkv")
	}
}

// Jobs created before the library_id column existed must still be attributed,
// otherwise the whole history reads as "Unknown" after an upgrade.
func TestNewJobViewFallsBackToPathMatch(t *testing.T) {
	libs := []config.Library{
		{ID: "shows", Name: "Shows", Path: "/mnt/shows"},
	}
	byID := map[string]config.Library{"shows": libs[0]}

	view := newJobView(&models.Job{
		LibraryID:  "", // legacy job
		SourcePath: "/mnt/shows/s01/e01.mkv",
	}, byID, libs)

	if view.LibraryName != "Shows" {
		t.Errorf("LibraryName = %q, want %q", view.LibraryName, "Shows")
	}
	if view.RelPath != "s01/e01.mkv" {
		t.Errorf("RelPath = %q, want %q", view.RelPath, "s01/e01.mkv")
	}
}

func TestNewJobViewUnknownLibrary(t *testing.T) {
	libs := []config.Library{
		{ID: "movies", Name: "Movies", Path: "/mnt/movies"},
	}
	byID := map[string]config.Library{"movies": libs[0]}

	view := newJobView(&models.Job{
		SourcePath: "/somewhere/else/orphan.mkv",
	}, byID, libs)

	if view.LibraryName != unknownLibraryName {
		t.Errorf("LibraryName = %q, want %q", view.LibraryName, unknownLibraryName)
	}
	// Falling back to the bare filename keeps the row readable.
	if view.RelPath != "orphan.mkv" {
		t.Errorf("RelPath = %q, want %q", view.RelPath, "orphan.mkv")
	}
}

// A library nested inside another must win, so files are not attributed to the
// broader parent tree.
func TestLibraryForPathPrefersLongestMatch(t *testing.T) {
	libs := []config.Library{
		{ID: "all", Name: "All Media", Path: "/mnt/media"},
		{ID: "movies", Name: "Movies", Path: "/mnt/media/movies"},
	}

	lib, ok := libraryForPath("/mnt/media/movies/film.mkv", libs)
	if !ok {
		t.Fatal("libraryForPath() found no library")
	}
	if lib.ID != "movies" {
		t.Errorf("matched library = %q, want %q", lib.ID, "movies")
	}
}

// A sibling directory sharing a name prefix must not match.
func TestLibraryForPathRejectsSiblingPrefix(t *testing.T) {
	libs := []config.Library{
		{ID: "movies", Name: "Movies", Path: "/mnt/movies"},
	}

	if _, ok := libraryForPath("/mnt/movies-archive/film.mkv", libs); ok {
		t.Error("libraryForPath() matched a sibling directory sharing a prefix")
	}
}

// A stale library_id (library since deleted) should fall back to path matching
// rather than reporting the wrong library.
func TestNewJobViewStaleLibraryIDFallsBack(t *testing.T) {
	libs := []config.Library{
		{ID: "movies", Name: "Movies", Path: "/mnt/movies"},
	}
	byID := map[string]config.Library{"movies": libs[0]}

	view := newJobView(&models.Job{
		LibraryID:  "deleted-library",
		SourcePath: "/mnt/movies/film.mkv",
	}, byID, libs)

	if view.LibraryName != "Movies" {
		t.Errorf("LibraryName = %q, want %q", view.LibraryName, "Movies")
	}
}

func TestEstimateRemaining(t *testing.T) {
	started := time.Now().Add(-60 * time.Second)

	tests := []struct {
		name      string
		startedAt *time.Time
		progress  float64
		want      float64 // 0 means "no estimate"
		tolerance float64
	}{
		{"halfway after 60s implies about 60s left", &started, 50, 60, 5},
		{"three quarters after 60s implies about 20s left", &started, 75, 20, 5},
		// Early percentages are far too noisy to extrapolate from, and a wildly
		// wrong estimate is worse than showing none.
		{"too early to estimate", &started, 0.4, 0, 0},
		{"complete", &started, 100, 0, 0},
		{"never started", nil, 50, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateRemaining(tc.startedAt, tc.progress)
			if tc.want == 0 {
				if got != 0 {
					t.Errorf("estimateRemaining() = %.1f, want 0 (no estimate)", got)
				}
				return
			}
			if diff := math.Abs(got - tc.want); diff > tc.tolerance {
				t.Errorf("estimateRemaining() = %.1f, want about %.1f", got, tc.want)
			}
		})
	}
}
