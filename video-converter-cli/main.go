// Package main implements the video converter CLI application.
package main

import (
	"fmt"
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
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`
Video Converter CLI

Usage:
  video-converter-cli <command> [options]

Commands:
  master <config>        Start master coordinator
  worker <config>        Start worker process
  status [--master-url]  Show conversion progress
  stats [--master-url]   Show detailed statistics
  retry [--master-url]   Retry failed jobs
  detect                 Detect GPU/Vulkan capabilities

Examples:
  video-converter-cli master config.yaml
  video-converter-cli worker config.yaml
  video-converter-cli status --master-url http://localhost:8080
  video-converter-cli stats --master-url http://localhost:8080
  video-converter-cli detect
  `)
}
