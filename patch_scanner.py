with open('video-converter-master/internal/scanner/scanner.go', 'r') as f:
    content = f.read()

s1 = """	if strings.HasPrefix(baseName, ".") {
		if opts.SkipHiddenFiles {
			slog.Debug("Skipping hidden file", "path", fullPath)
			return nil, nil
		var sourceChecksum string
		var hashComputed bool

		// Detect duplicates if enabled
		if s.Options.DetectDuplicates {
			fileHash, err := computeFileHash(fullPath)
			if err != nil {
				slog.Warn("Failed to compute file hash", "path", fullPath, "error", err)
				// Continue processing even if hash fails
			} else {
				sourceChecksum = fileHash
				hashComputed = true
				if originalPath, exists := s.seenHashes[fileHash]; exists {
					slog.Info("Duplicate file detected",
						"path", fullPath,
						"original", originalPath,
						"hash", fileHash)
					continue // Skip duplicate
				}
				s.seenHashes[fileHash] = fullPath
			}
		}
	}"""

s2 = """	if strings.HasPrefix(baseName, ".") {
		if opts.SkipHiddenFiles {
			slog.Debug("Skipping hidden file", "path", fullPath)
			return nil, nil
		}
	}"""

content = content.replace(s1, s2)

s3 = """		// Calculate source file checksum for integrity validation
		if !hashComputed {
			var err error
			sourceChecksum, err = computeFileHash(fullPath)
			if err != nil {
				slog.Warn("Failed to compute source checksum", "path", fullPath, "error", err)
				// Continue without checksum - it will be empty string
				sourceChecksum = ""
			}
		}
	}

	// Calculate source file checksum for integrity validation
	sourceChecksum, err := computeFileHash(fullPath)
	if err != nil {
		slog.Warn("Failed to compute source checksum", "path", fullPath, "error", err)
		// Continue without checksum - it will be empty string
		sourceChecksum = ""
	}"""

s4 = """		}
	}

	// Calculate source file checksum for integrity validation
	sourceChecksum, err := computeFileHash(fullPath)
	if err != nil {
		slog.Warn("Failed to compute source checksum", "path", fullPath, "error", err)
		// Continue without checksum - it will be empty string
		sourceChecksum = ""
	}"""

content = content.replace(s3, s4)

with open('video-converter-master/internal/scanner/scanner.go', 'w') as f:
    f.write(content)
