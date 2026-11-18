package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const validMasterYAML = `
server:
  host: 0.0.0.0
  port: 8080
scanner:
  root_path: /data/videos
  output_base: /data/output
  video_extensions: [.mp4, .mkv]
database:
  path: ./jobs.db
`

const validWorkerYAML = `
worker:
  id: w1
  master_url: http://localhost:8080
  concurrency: 3
`

func TestValidateConfigEndpoint(t *testing.T) {
	srv := newTestServer(t)

	post := func(query, body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/api/validate-config"+query, strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.ValidateConfig(rec, req)
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp
	}

	// Valid master config.
	code, resp := post("?type=master", validMasterYAML)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, resp["valid"])

	// Valid worker config.
	code, resp = post("?type=worker", validWorkerYAML)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, resp["valid"])

	// Unknown field is rejected (KnownFields).
	code, resp = post("?type=master", validMasterYAML+"bogus_key: 1\n")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, resp["valid"])
	require.NotEmpty(t, resp["errors"])

	// Missing required fields.
	code, resp = post("?type=master", "server:\n  port: 8080\n")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, resp["valid"])

	// Bad type / bad method / empty body.
	code, _ = post("?type=nope", validMasterYAML)
	require.Equal(t, http.StatusBadRequest, code)
	req := httptest.NewRequest(http.MethodGet, "/api/validate-config?type=master", nil)
	rec := httptest.NewRecorder()
	srv.ValidateConfig(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	code, resp = post("?type=master", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, resp["valid"])
}
