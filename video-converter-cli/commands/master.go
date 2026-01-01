package commands

// Master starts the master coordinator process with the provided configuration file.
func Master(args []string) {
	runBinaryCommand("master", "video-converter-master", args)
}
