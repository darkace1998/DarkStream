package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager returns a Manager backed by a temp file with no seed, i.e. the
// state of a fresh install configured entirely through the Web UI.
func newTestManager(t *testing.T) *Manager {
	t.Helper()

	m, err := NewManagerWithSeed(filepath.Join(t.TempDir(), "active-config.json"), Seed{})
	if err != nil {
		t.Fatalf("NewManagerWithSeed() error = %v", err)
	}
	return m
}

func TestSaveLibraryAssignsIDAndPersists(t *testing.T) {
	m := newTestManager(t)

	saved, err := m.SaveLibrary(&Library{
		Name:       "Movies",
		Path:       "/mnt/movies",
		OutputPath: "/mnt/converted/movies",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}
	if saved.ID == "" {
		t.Fatal("SaveLibrary() did not assign an ID")
	}

	libs := m.GetLibraries()
	if len(libs) != 1 {
		t.Fatalf("GetLibraries() returned %d libraries, want 1", len(libs))
	}
	if libs[0].Name != "Movies" {
		t.Errorf("library name = %q, want %q", libs[0].Name, "Movies")
	}
}

func TestSaveLibraryUpdatesInPlace(t *testing.T) {
	m := newTestManager(t)

	saved, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	saved.Name = "Films"
	saved.Enabled = false
	if _, err := m.SaveLibrary(saved); err != nil {
		t.Fatalf("SaveLibrary() update error = %v", err)
	}

	libs := m.GetLibraries()
	if len(libs) != 1 {
		t.Fatalf("updating created a new library: got %d, want 1", len(libs))
	}
	if libs[0].Name != "Films" {
		t.Errorf("library name = %q, want %q", libs[0].Name, "Films")
	}
	if got := len(m.GetEnabledLibraries()); got != 0 {
		t.Errorf("GetEnabledLibraries() = %d, want 0 after disabling", got)
	}
}

func TestSaveLibraryRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		lib  Library
	}{
		{"missing name", Library{Path: "/mnt/a", OutputPath: "/mnt/b"}},
		{"missing path", Library{Name: "A", OutputPath: "/mnt/b"}},
		{"relative path", Library{Name: "A", Path: "relative/dir", OutputPath: "/mnt/b"}},
		{"missing output", Library{Name: "A", Path: "/mnt/a"}},
		{"relative output", Library{Name: "A", Path: "/mnt/a", OutputPath: "rel"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t)
			if _, err := m.SaveLibrary(&tc.lib); err == nil {
				t.Fatal("SaveLibrary() accepted an invalid library")
			}
			if got := len(m.GetLibraries()); got != 0 {
				t.Errorf("invalid library was stored: %d libraries", got)
			}
		})
	}
}

// A library that converts in place needs no output path.
func TestSaveLibraryReplaceSourceNeedsNoOutput(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.SaveLibrary(&Library{
		Name: "InPlace", Path: "/mnt/a", Enabled: true, ReplaceSource: true,
	}); err != nil {
		t.Fatalf("SaveLibrary() rejected replace_source library: %v", err)
	}
}

func TestSaveLibraryRejectsDuplicatePath(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.SaveLibrary(&Library{
		Name: "A", Path: "/mnt/shared", OutputPath: "/mnt/out-a", Enabled: true,
	}); err != nil {
		t.Fatalf("first SaveLibrary() error = %v", err)
	}

	// A second library scanning the same tree would queue every file twice.
	if _, err := m.SaveLibrary(&Library{
		Name: "B", Path: "/mnt/shared", OutputPath: "/mnt/out-b", Enabled: true,
	}); err == nil {
		t.Fatal("SaveLibrary() accepted a duplicate path")
	}
}

func TestDeleteLibrary(t *testing.T) {
	m := newTestManager(t)

	saved, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	if err := m.DeleteLibrary(saved.ID); err != nil {
		t.Fatalf("DeleteLibrary() error = %v", err)
	}
	if got := len(m.GetLibraries()); got != 0 {
		t.Errorf("GetLibraries() = %d, want 0 after delete", got)
	}
	if err := m.DeleteLibrary("does-not-exist"); err == nil {
		t.Error("DeleteLibrary() accepted an unknown id")
	}
}

// Saving a library must not disturb unrelated configuration sections.
func TestSaveLibraryPreservesOtherSections(t *testing.T) {
	m := newTestManager(t)

	before := m.Get()
	if _, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	after := m.Get()
	if after.Video.Resolution != before.Video.Resolution {
		t.Errorf("video resolution changed: %q -> %q",
			before.Video.Resolution, after.Video.Resolution)
	}
	if after.Worker.Concurrency != before.Worker.Concurrency {
		t.Errorf("worker concurrency changed: %d -> %d",
			before.Worker.Concurrency, after.Worker.Concurrency)
	}
	if len(after.Scanner.VideoExtensions) == 0 {
		t.Error("scanner extensions were wiped by SaveLibrary")
	}
}

// Libraries and scan settings must survive a reload from disk, since the store
// is now the only place configuration lives.
func TestLibrariesPersistAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active-config.json")

	m, err := NewManagerWithSeed(path, Seed{})
	if err != nil {
		t.Fatalf("NewManagerWithSeed() error = %v", err)
	}
	if _, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	reloaded, err := NewManagerWithSeed(path, Seed{})
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}

	libs := reloaded.GetLibraries()
	if len(libs) != 1 || libs[0].Name != "Movies" {
		t.Fatalf("libraries did not survive reload: %+v", libs)
	}
}

func TestOnChangeFiresOnLibraryUpdate(t *testing.T) {
	m := newTestManager(t)

	calls := 0
	m.OnChange(func() { calls++ })

	if _, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnChange callback ran %d times, want 1", calls)
	}
}

// A fresh install must default to CPU encoding: enabling a hardware backend that
// the host cannot use fails every job instead of falling back.
func TestFreshInstallDefaultsToCPU(t *testing.T) {
	m := newTestManager(t)

	cfg := m.Get()
	if cfg.Video.HWAccel != "none" {
		t.Errorf("default hwaccel = %q, want %q", cfg.Video.HWAccel, "none")
	}
	if cfg.Worker.UseVulkan {
		t.Error("default UseVulkan = true, want false")
	}
}

func TestEffectiveConversionSettingsInheritsGlobal(t *testing.T) {
	m := newTestManager(t)

	saved, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	global := m.GetConversionSettings()
	got := m.EffectiveConversionSettings(saved.ID)

	if got.TargetResolution != global.TargetResolution || got.Codec != global.Codec {
		t.Errorf("library without overrides = %+v, want global %+v", got, global)
	}
}

func TestEffectiveConversionSettingsAppliesOverrides(t *testing.T) {
	m := newTestManager(t)

	saved, err := m.SaveLibrary(&Library{
		Name: "Anime", Path: "/mnt/anime", OutputPath: "/mnt/out", Enabled: true,
		Conversion: &ConversionOverride{
			Resolution:   "1280x720",
			Codec:        "h265",
			OutputFormat: "mkv",
		},
	})
	if err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	global := m.GetConversionSettings()
	got := m.EffectiveConversionSettings(saved.ID)

	if got.TargetResolution != "1280x720" {
		t.Errorf("resolution = %q, want %q", got.TargetResolution, "1280x720")
	}
	if got.Codec != "h265" {
		t.Errorf("codec = %q, want %q", got.Codec, "h265")
	}
	if got.OutputFormat != "mkv" {
		t.Errorf("output format = %q, want %q", got.OutputFormat, "mkv")
	}
	// Fields left blank must still come from the global settings.
	if got.Bitrate != global.Bitrate {
		t.Errorf("bitrate = %q, want inherited %q", got.Bitrate, global.Bitrate)
	}
	if got.AudioCodec != global.AudioCodec {
		t.Errorf("audio codec = %q, want inherited %q", got.AudioCodec, global.AudioCodec)
	}
}

// Overriding one library must not affect another, nor the global settings.
func TestEffectiveConversionSettingsIsolatedPerLibrary(t *testing.T) {
	m := newTestManager(t)

	a, err := m.SaveLibrary(&Library{
		Name: "A", Path: "/mnt/a", OutputPath: "/mnt/out-a", Enabled: true,
		Conversion: &ConversionOverride{Bitrate: "12M"},
	})
	if err != nil {
		t.Fatalf("SaveLibrary(A) error = %v", err)
	}
	b, err := m.SaveLibrary(&Library{
		Name: "B", Path: "/mnt/b", OutputPath: "/mnt/out-b", Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveLibrary(B) error = %v", err)
	}

	if got := m.EffectiveConversionSettings(a.ID).Bitrate; got != "12M" {
		t.Errorf("library A bitrate = %q, want %q", got, "12M")
	}
	global := m.GetConversionSettings()
	if got := m.EffectiveConversionSettings(b.ID).Bitrate; got != global.Bitrate {
		t.Errorf("library B bitrate = %q, want global %q", got, global.Bitrate)
	}
	if m.GetConversionSettings().Bitrate != global.Bitrate {
		t.Error("global settings were mutated by a library override")
	}
}

// An unknown library ID (e.g. a legacy job) falls back to global settings.
func TestEffectiveConversionSettingsUnknownLibrary(t *testing.T) {
	m := newTestManager(t)

	global := m.GetConversionSettings()
	got := m.EffectiveConversionSettings("no-such-library")

	if got.TargetResolution != global.TargetResolution {
		t.Errorf("unknown library = %+v, want global %+v", got, global)
	}
	if got := m.EffectiveConversionSettings(""); got.Codec != global.Codec {
		t.Errorf("empty library id = %+v, want global %+v", got, global)
	}
}

func TestSaveLibraryRejectsInvalidOverrides(t *testing.T) {
	tests := []struct {
		name string
		conv ConversionOverride
	}{
		{"bad resolution", ConversionOverride{Resolution: "1080p"}},
		{"bad codec", ConversionOverride{Codec: "h999"}},
		{"bad bitrate", ConversionOverride{Bitrate: "lots"}},
		{"bad preset", ConversionOverride{Preset: "turbo"}},
		{"bad format", ConversionOverride{OutputFormat: "mov"}},
		{"bad hwaccel", ConversionOverride{HWAccel: "cuda"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t)
			conv := tc.conv
			_, err := m.SaveLibrary(&Library{
				Name: "L", Path: "/mnt/l", OutputPath: "/mnt/out",
				Enabled: true, Conversion: &conv,
			})
			if err == nil {
				t.Fatal("SaveLibrary() accepted an invalid conversion override")
			}
		})
	}
}

// A config naming a retired backend must be migrated, not rejected. Rejecting it
// would fail validation on load and silently reset every setting — including the
// configured libraries — back to defaults.
func TestRetiredHWAccelIsMigratedNotRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active-config.json")

	m, err := NewManagerWithSeed(path, Seed{})
	if err != nil {
		t.Fatalf("NewManagerWithSeed() error = %v", err)
	}
	if _, err := m.SaveLibrary(&Library{
		Name: "Movies", Path: "/mnt/movies", OutputPath: "/mnt/out", Enabled: true,
	}); err != nil {
		t.Fatalf("SaveLibrary() error = %v", err)
	}

	// Simulate a config written when NVENC was still selectable.
	raw, err := os.ReadFile(path) // #nosec G304 - test-controlled temp path
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	patched := strings.Replace(string(raw), `"hwaccel": "none"`, `"hwaccel": "nvenc"`, 1)
	if patched == string(raw) {
		t.Fatal("test setup did not inject a retired hwaccel value")
	}
	if err := os.WriteFile(path, []byte(patched), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	reloaded, err := NewManagerWithSeed(path, Seed{})
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}

	cfg := reloaded.Get()
	if cfg.Video.HWAccel != "none" {
		t.Errorf("hwaccel = %q, want it migrated to %q", cfg.Video.HWAccel, "none")
	}
	// The critical assertion: the rest of the configuration survived.
	if len(cfg.Libraries) != 1 {
		t.Errorf("libraries = %d, want 1 — the config was reset instead of migrated", len(cfg.Libraries))
	}
}

// NVENC and other vendor-specific backends must no longer validate.
func TestVendorBackendsRejected(t *testing.T) {
	m := newTestManager(t)

	for _, backend := range []string{"nvenc", "qsv", "vaapi"} {
		t.Run(backend, func(t *testing.T) {
			conv := ConversionOverride{HWAccel: backend}
			if _, err := m.SaveLibrary(&Library{
				Name: "L-" + backend, Path: "/mnt/" + backend, OutputPath: "/mnt/out-" + backend,
				Enabled: true, Conversion: &conv,
			}); err == nil {
				t.Fatalf("SaveLibrary() accepted retired backend %q", backend)
			}
		})
	}
}
