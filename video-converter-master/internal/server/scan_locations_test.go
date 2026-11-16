package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/video-converter-common/models"
	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/stretchr/testify/require"
)

// fakeLocationManager implements LocationManager for tests.
type fakeLocationManager struct {
	mu   sync.Mutex
	locs map[string]models.ScanLocation
}

func newFakeLocationManager() *fakeLocationManager {
	return &fakeLocationManager{locs: map[string]models.ScanLocation{}}
}

func (f *fakeLocationManager) List() []models.LocationStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]models.LocationStatus, 0, len(f.locs))
	for _, l := range f.locs {
		out = append(out, models.LocationStatus{ScanLocation: l, Running: true})
	}
	return out
}

func (f *fakeLocationManager) Add(loc models.ScanLocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.locs[loc.ID]; ok {
		return fmt.Errorf("scan location %q already exists", loc.ID)
	}
	f.locs[loc.ID] = loc
	return nil
}

func (f *fakeLocationManager) Update(loc models.ScanLocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.locs[loc.ID]; !ok {
		return fmt.Errorf("scan location %q not found", loc.ID)
	}
	f.locs[loc.ID] = loc
	return nil
}

func (f *fakeLocationManager) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.locs[id]; !ok {
		return fmt.Errorf("scan location %q not found", id)
	}
	delete(f.locs, id)
	return nil
}

func (f *fakeLocationManager) ScanNow(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.locs[id]; !ok {
		return fmt.Errorf("scan location %q not found", id)
	}
	return nil
}

func newLocationsTestServer(t *testing.T) (*Server, *fakeLocationManager, string) {
	t.Helper()
	srv := newTestServer(t)
	lm := newFakeLocationManager()
	srv.SetLocationManager(lm)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	srv.SetPersister(config.NewPersister(cfgPath))
	return srv, lm, cfgPath
}

func TestScanLocations_ListEmpty(t *testing.T) {
	srv, _, _ := newLocationsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/scan-locations", nil)
	rec := httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Locations []models.LocationStatus `json:"locations"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Locations)
}

func TestScanLocations_CreatePersistsAndRuns(t *testing.T) {
	srv, lm, cfgPath := newLocationsTestServer(t)
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")

	body := fmt.Sprintf(`{"id":"movies","name":"Movies","root_path":%q,"output_base":%q,"engines":8,"hash_algo":"sha1","scan_interval_sec":60,"enable_watch":true,"priority":7}`, root, out)
	req := httptest.NewRequest(http.MethodPost, "/api/scan-locations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	// Runtime got the location
	list := lm.List()
	require.Len(t, list, 1)
	require.Equal(t, "movies", list[0].ID)
	require.Equal(t, 8, list[0].Engines)
	require.Equal(t, "sha1", list[0].HashAlgo)
	require.Equal(t, 60*time.Second, list[0].ScanInterval)

	// Config file persisted it and it round-trips through the strict loader
	cfg, err := config.LoadMasterConfig(cfgPath)
	require.NoError(t, err)
	cfg.NormalizeScanLocations()
	require.Len(t, cfg.Scanner.Locations, 1)
	require.Equal(t, "movies", cfg.Scanner.Locations[0].ID)
	require.Equal(t, 60*time.Second, cfg.Scanner.Locations[0].ScanInterval)
	require.Equal(t, "sha1", cfg.Scanner.Locations[0].HashAlgo)
	require.Equal(t, 8, cfg.Scanner.Locations[0].Engines)

	// allowedDirs now include the new location
	dirs := srv.getAllowedDirs()
	require.Contains(t, dirs, root)
	require.Contains(t, dirs, out)
}

func TestScanLocations_Validation(t *testing.T) {
	srv, _, _ := newLocationsTestServer(t)
	root := t.TempDir()

	cases := map[string]string{
		"bad id":       fmt.Sprintf(`{"id":"bad id!","root_path":%q,"output_base":%q,"engines":4}`, root, root),
		"missing root": `{"id":"x","root_path":"","output_base":"/tmp/o","engines":4}`,
		"root not dir": fmt.Sprintf(`{"id":"x","root_path":%q,"output_base":"/tmp/o","engines":4}`, filepath.Join(root, "nope")),
		"bad engines":  fmt.Sprintf(`{"id":"x","root_path":%q,"output_base":%q,"engines":0}`, root, root),
		"bad algo":     fmt.Sprintf(`{"id":"x","root_path":%q,"output_base":%q,"engines":4,"hash_algo":"md5"}`, root, root),
		"bad priority": fmt.Sprintf(`{"id":"x","root_path":%q,"output_base":%q,"engines":4,"priority":99}`, root, root),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/scan-locations", strings.NewReader(body))
			rec := httptest.NewRecorder()
			srv.HandleScanLocations(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

func TestScanLocations_UpdateAndDelete(t *testing.T) {
	srv, lm, cfgPath := newLocationsTestServer(t)
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/scan-locations", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.HandleScanLocations(rec, req)
		return rec
	}

	rec := post(fmt.Sprintf(`{"id":"a","root_path":%q,"output_base":%q,"engines":2}`, root, out))
	require.Equal(t, http.StatusOK, rec.Code)

	// Upsert same id with different engines -> update, still one location
	rec = post(fmt.Sprintf(`{"id":"a","root_path":%q,"output_base":%q,"engines":16}`, root, out))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Len(t, lm.List(), 1)
	require.Equal(t, 16, lm.List()[0].Engines)

	// Delete
	req := httptest.NewRequest(http.MethodDelete, "/api/scan-locations?id=a", nil)
	rec = httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, lm.List())

	// Config file no longer has the location
	cfg, err := config.LoadMasterConfig(cfgPath)
	require.NoError(t, err)
	cfg.NormalizeScanLocations()
	require.Empty(t, cfg.Scanner.Locations)

	// Deleting again -> 404
	req = httptest.NewRequest(http.MethodDelete, "/api/scan-locations?id=a", nil)
	rec = httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestScanLocations_ScanNow(t *testing.T) {
	srv, lm, _ := newLocationsTestServer(t)
	root := t.TempDir()
	require.NoError(t, lm.Add(models.ScanLocation{ID: "a", RootPath: root, OutputBase: root, Engines: 1}))

	req := httptest.NewRequest(http.MethodPost, "/api/scan-locations?scan_now=a", nil)
	rec := httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/scan-locations?scan_now=missing", nil)
	rec = httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestScanLocations_NoManager(t *testing.T) {
	srv := newTestServer(t) // no location manager attached
	req := httptest.NewRequest(http.MethodGet, "/api/scan-locations", nil)
	rec := httptest.NewRecorder()
	srv.HandleScanLocations(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestScanLocations_PersistedFileIsHumanReadable(t *testing.T) {
	_, _, cfgPath := newLocationsTestServer(t)
	p := config.NewPersister(cfgPath)
	err := p.UpdateScanLocations([]models.ScanLocation{{
		ID:           "movies",
		Name:         "Movies",
		RootPath:     "/data/videos",
		OutputBase:   "/data/converted",
		Engines:      6,
		HashAlgo:     "sha1",
		ScanInterval: 5 * time.Minute,
		EnableWatch:  true,
		Priority:     5,
	}})
	require.NoError(t, err)

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "scan_interval: 5m0s")
	require.Contains(t, string(data), "hash_algo: sha1")
}
