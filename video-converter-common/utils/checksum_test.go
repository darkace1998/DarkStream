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

// The SHA-256 implementation is hardware-accelerated (sha256-simd); it must
// still produce standard digests so hashes stored by older versions and
// checksums computed by workers/tools remain valid.
func TestNewHashSHA256MatchesStdlib(t *testing.T) {
	inputs := [][]byte{
		{},
		[]byte("hello world"),
		make([]byte, 55),      // one block minus padding
		make([]byte, 64),      // exactly one block
		make([]byte, 1<<20+1), // spans the read buffer
	}
	for i := range inputs {
		for j := range inputs[i] {
			inputs[i][j] = byte(i*31 + j)
		}
		h, err := NewHash(HashAlgoSHA256)
		if err != nil {
			t.Fatalf("NewHash(sha256): %v", err)
		}
		h.Write(inputs[i])
		got := hex.EncodeToString(h.Sum(nil))

		ref := sha256.New()
		ref.Write(inputs[i])
		want := hex.EncodeToString(ref.Sum(nil))

		if got != want {
			t.Errorf("input %d: simd sha256 %s != stdlib %s", i, got, want)
		}
	}

	// Explicit algo names and the default must behave the same.
	for _, algo := range []string{"", "sha256", "SHA256"} {
		h, err := NewHash(algo)
		if err != nil {
			t.Fatalf("NewHash(%q): %v", algo, err)
		}
		h.Write([]byte("abc"))
		if got, want := hex.EncodeToString(h.Sum(nil)), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
			t.Errorf("NewHash(%q) digest of \"abc\" = %s, want %s", algo, got, want)
		}
	}
}

// BenchmarkHashSHA256 measures raw hash throughput (MB/s) of the active
// SHA-256 implementation. On CPUs with SHA-NI/AVX2/AVX-512 this should be
// well above 500 MB/s; the generic fallback is ~100-200 MB/s.
func BenchmarkHashSHA256(b *testing.B) {
	data := make([]byte, 64<<20)
	for i := range data {
		data[i] = byte(i)
	}
	h, err := NewHash(HashAlgoSHA256)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Reset()
		if _, err := h.Write(data); err != nil {
			b.Fatal(err)
		}
		_ = h.Sum(nil)
	}
}
