package commands

import (
	"io"
	"net/http"
	"testing"
)

func TestNewMasterRequest_RejectsInsecureAuthTransport(t *testing.T) {
	t.Setenv(masterAPIKeyEnvVar, "secret")

	_, err := newMasterRequest(http.MethodGet, "http://example.com/api/status", nil, "")
	if err == nil {
		t.Fatal("newMasterRequest() expected error for insecure auth transport, got nil")
	}
}

func TestNewMasterRequest_AllowsLoopbackHTTPWithAuth(t *testing.T) {
	t.Setenv(masterAPIKeyEnvVar, "secret")

	req, err := newMasterRequest(http.MethodGet, "http://localhost:8080/api/status", nil, "")
	if err != nil {
		t.Fatalf("newMasterRequest() error = %v", err)
	}
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		if len(body) != 0 {
			t.Fatalf("expected empty request body, got %q", string(body))
		}
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer secret")
	}
}
