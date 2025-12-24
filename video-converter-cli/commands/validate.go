package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

// Validate validates a configuration file.
func Validate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	configType := fs.String("type", "", "Config type: 'master' or 'worker' (required)")
	configPath := fs.String("file", "", "Path to config file (required)")
	masterURL := fs.String("master-url", "http://localhost:8080", "Master server URL for remote validation")
	local := fs.Bool("local", false, "Validate locally without connecting to master")
	_ = fs.Parse(args)

	if *configType == "" {
		slog.Error("Config type is required")
		slog.Info("Usage: video-converter-cli validate --type <master|worker> --file <config-file>")
		os.Exit(1)
	}

	if *configType != "master" && *configType != "worker" {
		slog.Error("Invalid config type. Must be 'master' or 'worker'")
		os.Exit(1)
	}

	if *configPath == "" {
		slog.Error("Config file path is required")
		slog.Info("Usage: video-converter-cli validate --type <master|worker> --file <config-file>")
		os.Exit(1)
	}

	// Read config file
	// #nosec G304 - configPath is from command-line args, not untrusted network input
	configData, err := os.ReadFile(*configPath)
	if err != nil {
		slog.Error("Failed to read config file", "error", err, "path", *configPath)
		os.Exit(1)
	}

	if *local {
		// Local validation
		errors := validateConfigLocally(*configType, configData)
		printValidationResult(*configPath, *configType, errors)
		if len(errors) > 0 {
			os.Exit(1)
		}
		return
	}

	// Remote validation via master server
	url := fmt.Sprintf("%s/api/validate-config?type=%s", *masterURL, *configType)
	resp, err := http.Post(url, "application/yaml", bytes.NewReader(configData))
	if err != nil {
		slog.Error("Error connecting to master server", "error", err)
		slog.Info("Use --local flag to validate without master server")
		os.Exit(1)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("Error closing response body", "error", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", "error", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("Validation request failed", "status", resp.StatusCode)
		slog.Info(string(body))
		os.Exit(1)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("Error parsing response", "error", err)
		os.Exit(1)
	}

	valid, _ := result["valid"].(bool)
	var errors []string
	if errList, ok := result["errors"].([]any); ok {
		for _, e := range errList {
			if s, ok := e.(string); ok {
				errors = append(errors, s)
			}
		}
	}

	printValidationResult(*configPath, *configType, errors)
	if !valid {
		os.Exit(1)
	}
}

func validateConfigLocally(configType string, content []byte) []string {
	var errors []string

	if len(content) == 0 {
		return []string{"Empty configuration file"}
	}

	// Basic YAML structure check - look for required keys
	switch configType {
	case "master":
		requiredKeys := []string{"server:", "scanner:", "database:"}
		for _, key := range requiredKeys {
			if !bytes.Contains(content, []byte(key)) {
				errors = append(errors, fmt.Sprintf("Missing required section: %s", key))
			}
		}
		// Check for port
		if !bytes.Contains(content, []byte("port:")) {
			errors = append(errors, "Missing server port configuration")
		}
		// Check for root_path
		if !bytes.Contains(content, []byte("root_path:")) {
			errors = append(errors, "Missing scanner root_path configuration")
		}

	case "worker":
		requiredKeys := []string{"worker:", "storage:", "ffmpeg:"}
		for _, key := range requiredKeys {
			if !bytes.Contains(content, []byte(key)) {
				errors = append(errors, fmt.Sprintf("Missing required section: %s", key))
			}
		}
		// Check for master_url
		if !bytes.Contains(content, []byte("master_url:")) {
			errors = append(errors, "Missing worker master_url configuration")
		}
		// Check for worker id
		if !bytes.Contains(content, []byte("id:")) {
			errors = append(errors, "Missing worker id configuration")
		}
	}

	// Warn if logging section is missing
	if !bytes.Contains(content, []byte("logging:")) {
		slog.Warn("No logging section found in configuration")
	}

	return errors
}

func printValidationResult(path, configType string, errors []string) {
	if len(errors) == 0 {
		slog.Info("✅ Configuration is valid",
			"file", path,
			"type", configType)
	} else {
		slog.Error("❌ Configuration validation failed",
			"file", path,
			"type", configType)
		slog.Info("")
		slog.Info("Errors found:")
		for i, e := range errors {
			slog.Info(fmt.Sprintf("  %d. %s", i+1, e))
		}
	}
}
