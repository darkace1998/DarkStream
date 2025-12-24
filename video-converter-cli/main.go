// Package main implements the video converter CLI application.
package main

import (
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-cli/commands"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	subArgs := os.Args[2:]

	switch cmd {
	case "master":
		commands.Master(subArgs)
	case "worker":
		commands.Worker(subArgs)
	case "status":
		commands.Status(subArgs)
	case "stats":
		commands.Stats(subArgs)
	case "retry":
		commands.Retry(subArgs)
	case "detect":
		commands.Detect(subArgs)
	case "validate":
		commands.Validate(subArgs)
	case "cancel":
		commands.Cancel(subArgs)
	case "workers":
		commands.Workers(subArgs)
	case "jobs":
		commands.Jobs(subArgs)
	case "help", "--help", "-h":
		printUsage()
	default:
		slog.Error("Unknown command", "command", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	slog.Info(`
Video Converter CLI

Usage:
  video-converter-cli <command> [options]

Commands:
  master <config>        Start master coordinator
  worker <config>        Start worker process
  status [options]       Show conversion progress
  stats [options]        Show detailed statistics
  retry [options]        Retry failed jobs
  detect                 Detect GPU/Vulkan capabilities
  validate [options]     Validate configuration file
  cancel [options]       Cancel a job
  workers [options]      List and manage workers
  jobs [options]         List and manage jobs

Status Options:
  --master-url <url>     Master server URL (default: http://localhost:8080)
  --watch                Watch for updates (refresh every 2 seconds)
  --interval <seconds>   Refresh interval for watch mode
  --format <format>      Output format: table, json, csv

Stats Options:
  --master-url <url>     Master server URL
  --detailed             Show detailed metrics including workers
  --format <format>      Output format: table, json, csv

Retry Options:
  --master-url <url>     Master server URL
  --limit <n>            Maximum number of jobs to retry (default: 100)
  --format <format>      Output format: table, json, csv

Validate Options:
  --type <type>          Config type: 'master' or 'worker' (required)
  --file <path>          Path to config file (required)
  --local                Validate locally without connecting to master
  --master-url <url>     Master server URL for remote validation

Cancel Options:
  --master-url <url>     Master server URL
  --job-id <id>          Job ID to cancel (required)
  --format <format>      Output format: table, json, csv

Workers Options:
  --master-url <url>     Master server URL
  --active               Show only active workers
  --watch                Watch for updates
  --format <format>      Output format: table, json, csv

Jobs Options:
  --master-url <url>     Master server URL
  --status <status>      Filter by status: pending, processing, completed, failed
  --limit <n>            Maximum number of jobs to display (default: 50)
  --watch                Watch for updates
  --format <format>      Output format: table, json, csv

Examples:
  video-converter-cli master config.yaml
  video-converter-cli worker config.yaml
  video-converter-cli status --master-url http://localhost:8080
  video-converter-cli status --watch
  video-converter-cli stats --detailed
  video-converter-cli jobs --status pending --format json
  video-converter-cli workers --active
  video-converter-cli validate --type master --file config.yaml --local
  video-converter-cli cancel --job-id abc123
  video-converter-cli detect
  `)
}
