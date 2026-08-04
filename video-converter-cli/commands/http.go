package commands

import (
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/darkace1998/video-converter-common/utils"
)

const masterAPIKeyEnvVar = "DARKSTREAM_API_KEY"

// errCommandFailed signals that a command should exit non-zero. The failure
// has already been reported to the user via slog, so callers only need to
// translate it into a process exit code.
var errCommandFailed = errors.New("command failed")

// Job status constants shared across command output.
const (
	statusPending    = "pending"
	statusProcessing = "processing"
	statusCompleted  = "completed"
	statusFailed     = "failed"
	statusCancelled  = "cancelled"
)

var masterHTTPClient = &http.Client{Timeout: 15 * time.Second}

func newMasterRequest(method, requestURL string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if apiKey := os.Getenv(masterAPIKeyEnvVar); apiKey != "" {
		if err := utils.ValidateSecureTransport(requestURL, true); err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	return req, nil
}

func doMasterRequest(req *http.Request) (*http.Response, error) {
	return masterHTTPClient.Do(req)
}
