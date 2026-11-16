package converter

import (
	"fmt"
	"os"

	"github.com/darkace1998/video-converter-common/utils"
)

// Validator handles validation of converted video files
type Validator struct{}

// NewValidator creates a new Validator instance
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateFile performs basic validation on a file
func (v *Validator) ValidateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}

	if info.Size() == 0 {
		return fmt.Errorf("file is empty: %s", path)
	}

	return nil
}

// GetFileSize returns the size of a file in bytes
func (v *Validator) GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}
	return info.Size(), nil
}

// VerifyChecksum verifies that a file's checksum matches the expected value.
// The algorithm is inferred from the expected checksum's hex length
// (SHA-256 = 64 chars, SHA-1 = 40 chars), so locations configured with
// hash_algo: sha1 are verified with SHA-1.
func (v *Validator) VerifyChecksum(filePath, expectedChecksum string) (bool, error) {
	match, err := utils.VerifyFileHashAuto(filePath, expectedChecksum)
	if err != nil {
		return false, fmt.Errorf("failed to verify checksum: %w", err)
	}
	return match, nil
}
