package commands

import (
	"io"
	"net/http"
	"os"
)

const masterAPIKeyEnvVar = "DARKSTREAM_API_KEY"

func newMasterRequest(method, url string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if apiKey := os.Getenv(masterAPIKeyEnvVar); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	return req, nil
}
