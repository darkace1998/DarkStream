package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCalculateFileSHA256(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := []byte("hello world")

	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	hash := sha256.New()
	hash.Write(testContent)
	expectedHash := hex.EncodeToString(hash.Sum(nil))

	t.Run("ValidFile", func(t *testing.T) {
		hash, err := CalculateFileSHA256(testFile)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if hash != expectedHash {
			t.Errorf("Expected hash %s, got %s", expectedHash, hash)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "non_existent.txt")
		hash, err := CalculateFileSHA256(nonExistentFile)
		if err == nil {
			t.Error("Expected error for non-existent file, got nil")
		}
		if hash != "" {
			t.Errorf("Expected empty hash, got %s", hash)
		}
	})
}

func TestVerifyFileSHA256(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := []byte("hello world")

	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	hash := sha256.New()
	hash.Write(testContent)
	expectedHash := hex.EncodeToString(hash.Sum(nil))

	t.Run("ValidMatch", func(t *testing.T) {
		match, err := VerifyFileSHA256(testFile, expectedHash)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !match {
			t.Error("Expected match to be true, got false")
		}
	})

	t.Run("Mismatch", func(t *testing.T) {
		match, err := VerifyFileSHA256(testFile, "invalidhash123")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if match {
			t.Error("Expected match to be false, got true")
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "non_existent.txt")
		match, err := VerifyFileSHA256(nonExistentFile, expectedHash)
		if err == nil {
			t.Error("Expected error for non-existent file, got nil")
		}
		if match {
			t.Error("Expected match to be false, got true")
		}
	})
}
