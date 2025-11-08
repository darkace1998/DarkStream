package converter

import (
	"fmt"
	"os"
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
