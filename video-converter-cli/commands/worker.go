package commands

// Worker starts the worker process with the provided configuration file.
func Worker(args []string) {
	runBinaryCommand("worker", "video-converter-worker", args)
}
