package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-master/internal/config"
	"github.com/stretchr/testify/require"
)

func newTestServerWithPersister(t *testing.T) (*Server, string) {
	t.Helper()
	srv := newTestServer(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	srv.SetPersister(config.NewPersister(path))
	return srv, path
}

func TestGetServerSettings(t *testing.T) {
	srv, path := newTestServerWithPersister(t)

	req := httptest.NewRequest(http.MethodGet, "/api/server-settings", nil)
	rec := httptest.NewRecorder()
	srv.HandleServerSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Port       int    `json:"port"`
		Host       string `json:"host"`
		ConfigFile string `json:"config_file"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 8080, resp.Port)
	require.Equal(t, "0.0.0.0", resp.Host)
	require.Equal(t, path, resp.ConfigFile)
}

func TestUpdateServerSettings_PersistsToConfigFile(t *testing.T) {
	srv, path := newTestServerWithPersister(t)

	body := `{"host":"127.0.0.1","port":9090,"api_key":"sekret","tls_cert":"","tls_key":"","database_path":"./master.db","log_level":"info","log_format":"json"}`
	req := httptest.NewRequest(http.MethodPost, "/api/server-settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleServerSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, true, resp["ok"])
	require.Equal(t, true, resp["restart_required"])
	require.Equal(t, path, resp["persisted_to"])

	// The config file now holds the new server settings and still loads.
	cfg, err := config.LoadMasterConfig(path)
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, "127.0.0.1", cfg.Server.Host)
	require.Equal(t, "sekret", cfg.Server.APIKey)
	require.Equal(t, "./master.db", cfg.Database.Path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), 64<<10)
}

func TestUpdateServerSettings_ValidationErrors(t *testing.T) {
	srv, _ := newTestServerWithPersister(t)

	cases := map[string]string{
		"bad port":        `{"host":"0.0.0.0","port":0,"database_path":"./jobs.db","log_level":"info","log_format":"json"}`,
		"tls half set":    `{"host":"0.0.0.0","port":8080,"tls_cert":"/c.pem","database_path":"./jobs.db","log_level":"info","log_format":"json"}`,
		"bad log level":   `{"host":"0.0.0.0","port":8080,"database_path":"./jobs.db","log_level":"verbose","log_format":"json"}`,
		"empty db path":   `{"host":"0.0.0.0","port":8080,"database_path":"","log_level":"info","log_format":"json"}`,
		"host whitespace": `{"host":"0.0.0.0 x","port":8080,"database_path":"./jobs.db","log_level":"info","log_format":"json"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/server-settings", strings.NewReader(body))
			rec := httptest.NewRecorder()
			srv.HandleServerSettings(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			var resp map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.NotEmpty(t, resp["errors"])
		})
	}
}

func TestUpdateServerSettings_NoPersister(t *testing.T) {
	srv := newTestServer(t) // no persister attached

	body := `{"host":"0.0.0.0","port":8080,"database_path":"./jobs.db","log_level":"info","log_format":"json"}`
	req := httptest.NewRequest(http.MethodPost, "/api/server-settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.HandleServerSettings(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleServerSettings_MethodNotAllowed(t *testing.T) {
	srv, _ := newTestServerWithPersister(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/server-settings", nil)
	rec := httptest.NewRecorder()
	srv.HandleServerSettings(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
