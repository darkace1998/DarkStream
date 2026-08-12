package server

import (
	"testing"

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
