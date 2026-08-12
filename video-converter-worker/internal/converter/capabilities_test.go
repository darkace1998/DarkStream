package converter

import (
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/models"
)

// The specific reason a backend failed must survive; FFmpeg's generic closing
// lines are useless on their own.
func TestSummarizeFFmpegErrorPrefersSpecificLine(t *testing.T) {
	output := strings.Join([]string{
		"[vost#0:0/h264_vulkan @ 0x1] Error while opening encoder - maybe incorrect parameters",
		"[out#0/mp4 @ 0x2] Nothing was written into output file",
		"Conversion failed!",
	}, "\n")

	got := summarizeFFmpegError(output, errFake{})
	if !strings.Contains(got, "Error while opening encoder") {
		t.Errorf("summary = %q, want the specific encoder error", got)
	}
	if strings.EqualFold(got, "Conversion failed!") {
		t.Error("summary fell back to FFmpeg's generic closing line")
	}
}

// When only generic lines exist, one of them is better than nothing.
func TestSummarizeFFmpegErrorFallsBackToGeneric(t *testing.T) {
	got := summarizeFFmpegError("Conversion failed!\n", errFake{})
	if got == "" {
		t.Error("summary is empty when only a generic line is present")
	}
}

func TestSummarizeFFmpegErrorUsesRunErrorWhenNoOutput(t *testing.T) {
	got := summarizeFFmpegError("", errFake{})
	if got != "fake error" {
		t.Errorf("summary = %q, want the run error", got)
	}
}

type errFake struct{}

func (errFake) Error() string { return "fake error" }

func TestCapabilitiesSupports(t *testing.T) {
	caps := &models.WorkerCapabilities{
		Backends: []models.BackendCapability{
			{Name: "none", Working: true},
			{Name: "vulkan", Available: true, Working: false},
		},
	}

	if !caps.Supports("none") {
		t.Error("Supports(none) = false, want true")
	}
	// Available but not working must not count as supported: that is exactly the
	// case that fails every job.
	if caps.Supports("vulkan") {
		t.Error("Supports(vulkan) = true, want false for an available-but-broken backend")
	}
	if caps.Supports("nvenc") {
		t.Error("Supports(nvenc) = true for a backend DarkStream no longer supports")
	}
	// An empty backend name means the CPU default.
	if !caps.Supports("") {
		t.Error(`Supports("") = false, want it to resolve to the CPU backend`)
	}
}

func TestCapabilitiesWorkingBackends(t *testing.T) {
	caps := &models.WorkerCapabilities{
		Backends: []models.BackendCapability{
			{Name: "none", Working: true},
			{Name: "vulkan", Working: false},
		},
	}

	got := caps.WorkingBackends()
	if len(got) != 1 || got[0] != "none" {
		t.Errorf("WorkingBackends() = %v, want [none]", got)
	}
}

// A nil capability set must be safe: workers predating probing report none.
func TestNilCapabilitiesAreSafe(t *testing.T) {
	var caps *models.WorkerCapabilities

	if caps.Supports("vulkan") {
		t.Error("nil capabilities reported support")
	}
	if got := caps.WorkingBackends(); got != nil {
		t.Errorf("nil capabilities returned %v, want nil", got)
	}
}
