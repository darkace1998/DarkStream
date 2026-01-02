package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// runBinaryCommand is a shared helper to find and run a binary with a config file.
// It takes the command name (for help messages), the binary name, and the arguments.
func runBinaryCommand(cmdName, binaryName string, args []string) {
	_ = flag.NewFlagSet(cmdName, flag.ExitOnError)

	// Expect the config file path as the first positional argument
	if len(args) == 0 {
		slog.Error("config file path is required")
		slog.Info(fmt.Sprintf("Usage: video-converter-cli %s <config-file>", cmdName))
		os.Exit(1)
	}

	configPath := args[0]

	// Find the binary
	binaryPath, err := findBinary(binaryName)
	if err != nil {
		slog.Error("Error", "error", fmt.Sprintf("%v", err))
		slog.Info("")
		slog.Info(fmt.Sprintf("Please build the %s binary first:", cmdName))
		slog.Info(fmt.Sprintf("  cd %s", binaryName))
		slog.Info(fmt.Sprintf("  go build -o %s", binaryName))
		os.Exit(1)
	}

	// Execute the binary with the config
	// #nosec G204 - configPath is from command-line args, not user input from network
	cmd := exec.Command(binaryPath, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err = cmd.Run()
	if err != nil {
		slog.Error(fmt.Sprintf("%s process failed", cmdName), "error", err)
		os.Exit(1)
	}
}

func findBinary(name string) (string, error) {
	// Try to find in PATH
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}

	// Try to find in parent directory structure
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Try current directory
	localPath := filepath.Join(currentDir, name, name)
	_, err = os.Stat(localPath)
	if err == nil {
		return localPath, nil
	}

	// Try parent directory
	parentDir := filepath.Dir(currentDir)
	parentPath := filepath.Join(parentDir, name, name)
	_, err = os.Stat(parentPath)
	if err == nil {
		return parentPath, nil
	}

	return "", fmt.Errorf("binary %s not found in PATH or local directories", name)
}
