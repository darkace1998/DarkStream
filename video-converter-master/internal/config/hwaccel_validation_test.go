package config

import (
	"errors"
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
)

func TestValidateHWAccelAcceptedValues(t *testing.T) {
	for _, hw := range []string{"", constants.HWAccelAuto, constants.HWAccelNone, constants.HWAccelCUDA, constants.HWAccelVAAPI, constants.HWAccelQSV, constants.HWAccelVulkan, "AUTO", " Cuda "} {
		cfg := &ActiveConfig{Worker: getDefaultWorkerConfig()}
		cfg.Video.HWAccel = hw
		if err := validateConfig(cfg); err != nil {
			t.Errorf("hw_accel=%q should be accepted, got %v", hw, err)
		}
	}
}

func TestValidateHWAccelRejectsUnknown(t *testing.T) {
	cfg := &ActiveConfig{Worker: getDefaultWorkerConfig()}
	cfg.Video.HWAccel = "dxva2" // not a supported value
	err := validateConfig(cfg)
	if err == nil {
		t.Fatal("hw_accel=dxva2 should be rejected")
	}
	var verrs *ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	found := false
	for _, e := range verrs.Errors {
		if e.Field == "video.hw_accel" {
			found = true
		}
	}
	if !found {
		t.Errorf("error should name video.hw_accel, got %+v", verrs.Errors)
	}
}

func TestGetConversionSettingsIncludesHWAccel(t *testing.T) {
	m := &Manager{config: &ActiveConfig{}}
	m.config.Video.HWAccel = constants.HWAccelVAAPI
	cs := m.GetConversionSettings()
	if cs.HWAccel != constants.HWAccelVAAPI {
		t.Errorf("ConversionSettings.HWAccel = %q, want %s", cs.HWAccel, constants.HWAccelVAAPI)
	}
}
