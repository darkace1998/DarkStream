package utils

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
)

// captureStdout intercepts stdout during the execution of f
func captureStdout(f func()) string {
	old := os.Stdout // keep backup of the real stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	outC := make(chan string)
	// copy the output in a separate goroutine so printing can't block indefinitely
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	w.Close()
	os.Stdout = old // restoring the real stdout
	return <-outC
}

func TestInitLogger_JSON_Debug(t *testing.T) {
	output := captureStdout(func() {
		InitLogger(constants.LogLevelDebug, constants.LogFormatJSON, "")
		slog.Debug("test debug message json")
	})

	if output == "" {
		t.Fatal("expected output, got none")
	}

	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(output), &logEntry)
	if err != nil {
		t.Fatalf("expected JSON output, but unmarshal failed: %v\nOutput was: %s", err, output)
	}

	if logEntry["msg"] != "test debug message json" {
		t.Errorf("expected msg to be 'test debug message json', got '%v'", logEntry["msg"])
	}
	if logEntry["level"] != "DEBUG" {
		t.Errorf("expected level to be 'DEBUG', got '%v'", logEntry["level"])
	}
}

func TestInitLogger_Text_Info(t *testing.T) {
	output := captureStdout(func() {
		InitLogger(constants.LogLevelInfo, constants.LogFormatText, "")
		slog.Debug("this debug message should not appear")
		slog.Info("test info message text")
	})

	if output == "" {
		t.Fatal("expected output, got none")
	}

	if strings.Contains(output, "this debug message should not appear") {
		t.Errorf("debug message should not be logged at Info level")
	}

	if !strings.Contains(output, "test info message text") {
		t.Errorf("expected output to contain 'test info message text', got '%s'", output)
	}

	if !strings.Contains(output, "level=INFO") {
		t.Errorf("expected output to contain 'level=INFO', got '%s'", output)
	}

	// Make sure it's not JSON
	var dummy map[string]interface{}
	if err := json.Unmarshal([]byte(output), &dummy); err == nil {
		t.Errorf("output appears to be JSON, but text format was requested")
	}
}

func TestInitLogger_Fallback(t *testing.T) {
	output := captureStdout(func() {
		InitLogger("invalid_level", "invalid_format", "")
		slog.Debug("this debug message should not appear")
		slog.Info("fallback level is info")
	})

	if output == "" {
		t.Fatal("expected output, got none")
	}

	// Default level is Info, so debug should not appear
	if strings.Contains(output, "this debug message should not appear") {
		t.Errorf("debug message should not be logged at default (Info) level")
	}

	// Default format is Text, so it should not be valid JSON
	var dummy map[string]interface{}
	if err := json.Unmarshal([]byte(output), &dummy); err == nil {
		t.Errorf("expected default text format, but output parsed as JSON: %s", output)
	}

	if !strings.Contains(output, "fallback level is info") {
		t.Errorf("expected info message to be logged at default level")
	}
}
