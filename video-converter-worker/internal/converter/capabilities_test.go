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
			{Name: "none", Codec: "h264", Working: true},
			{Name: "vulkan", Codec: "h264", Available: true, Working: false},
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
			{Name: "none", Codec: "h264", Working: true},
			{Name: "none", Codec: "h265", Working: true},
			{Name: "vulkan", Codec: "h264", Working: false},
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

// A backend that encodes h264 does not necessarily encode h265: an FFmpeg build
// can ship libx264 without libx265, and the Vulkan encoder covers only those
// two. Probing one codec and generalising misreported every other choice.
func TestSupportsCodecIsPerCodec(t *testing.T) {
	caps := &models.WorkerCapabilities{
		Backends: []models.BackendCapability{
			{Name: "none", Codec: "h264", Encoder: "libx264", Working: true},
			{Name: "none", Codec: "h265", Encoder: "libx265", Working: false},
			{Name: "vulkan", Codec: "h265", Encoder: "hevc_vulkan", Working: true},
		},
	}

	if !caps.SupportsCodec("none", "h264") {
		t.Error("SupportsCodec(none, h264) = false, want true")
	}
	if caps.SupportsCodec("none", "h265") {
		t.Error("SupportsCodec(none, h265) = true although that encoder failed")
	}
	if !caps.SupportsCodec("vulkan", "h265") {
		t.Error("SupportsCodec(vulkan, h265) = false, want true")
	}
	if caps.SupportsCodec("none", "av1") {
		t.Error("SupportsCodec reported a codec that was never probed")
	}

	// The coarse check still answers "can this backend encode anything".
	if !caps.Supports("none") {
		t.Error("Supports(none) = false although h264 works")
	}
}

// Vulkan has no VP9 or AV1 encoder, so those combinations must not be probed or
// reported at all rather than silently substituting h264.
func TestEncoderForProbeRejectsUnsupportedCombinations(t *testing.T) {
	tests := []struct {
		codec, backend, want string
		ok                   bool
	}{
		{"h264", HWAccelNone, "libx264", true},
		{"h265", HWAccelNone, "libx265", true},
		{"vp9", HWAccelNone, "libvpx-vp9", true},
		{"av1", HWAccelNone, "libaom-av1", true},
		{"h264", HWAccelVulkan, "h264_vulkan", true},
		{"h265", HWAccelVulkan, "hevc_vulkan", true},
		{"vp9", HWAccelVulkan, "", false},
		{"av1", HWAccelVulkan, "", false},
	}

	for _, tc := range tests {
		got, ok := encoderForProbe(tc.codec, tc.backend)
		if ok != tc.ok || got != tc.want {
			t.Errorf("encoderForProbe(%q, %q) = (%q, %v), want (%q, %v)",
				tc.codec, tc.backend, got, ok, tc.want, tc.ok)
		}
	}
}
