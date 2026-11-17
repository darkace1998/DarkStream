package models

import "testing"

// New scan locations default to sha1 (fastest on CPUs without SHA-NI); an
// explicitly configured algorithm is never rewritten.
func TestNormalizeScanLocationsHashAlgoDefault(t *testing.T) {
	cfg := &MasterConfig{}
	cfg.Scanner.RootPath = "/data/videos"
	cfg.NormalizeScanLocations()
	if len(cfg.Scanner.Locations) != 1 {
		t.Fatalf("locations = %d, want 1", len(cfg.Scanner.Locations))
	}
	if got := cfg.Scanner.Locations[0].HashAlgo; got != "sha1" {
		t.Errorf("default hash_algo = %q, want sha1", got)
	}

	cfg2 := &MasterConfig{}
	cfg2.Scanner.Locations = []ScanLocation{{ID: "x", RootPath: "/v", HashAlgo: "sha256"}}
	cfg2.NormalizeScanLocations()
	if got := cfg2.Scanner.Locations[0].HashAlgo; got != "sha256" {
		t.Errorf("explicit hash_algo rewritten to %q, want sha256 preserved", got)
	}
}
