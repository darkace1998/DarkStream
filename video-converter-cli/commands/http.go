package commands

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/darkace1998/video-converter-common/utils"
)

const masterAPIKeyEnvVar = "DARKSTREAM_API_KEY"

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
