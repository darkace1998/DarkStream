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
  master <config>    Start master coordinator
  worker <config>    Start worker process
  status             Show conversion progress
  stats              Show detailed statistics
  retry              Retry failed jobs
  detect             Detect GPU/Vulkan capabilities
	`)
}
