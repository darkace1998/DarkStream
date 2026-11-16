package utils

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCalculateFileHash_Algos(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "video.mp4")
	content := []byte("fake video content for hashing")
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sha256Sum := sha256.Sum256(content)
	sha1Sum := sha1.Sum(content)

	t.Run("sha256 default", func(t *testing.T) {
		h, err := CalculateFileHash(file, "")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if want := hex.EncodeToString(sha256Sum[:]); h != want {
			t.Errorf("got %s want %s", h, want)
		}
		if len(h) != 64 {
			t.Errorf("sha256 hex length = %d, want 64", len(h))
		}
	})

	t.Run("sha1", func(t *testing.T) {
		h, err := CalculateFileHash(file, HashAlgoSHA1)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if want := hex.EncodeToString(sha1Sum[:]); h != want {
			t.Errorf("got %s want %s", h, want)
		}
		if len(h) != 40 {
			t.Errorf("sha1 hex length = %d, want 40", len(h))
		}
	})

	t.Run("unsupported algo", func(t *testing.T) {
		if _, err := CalculateFileHash(file, "md5"); err == nil {
			t.Error("expected error for unsupported algo")
		}
	})
}

func TestVerifyFileHashAuto(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mp4")
	content := []byte("some bytes")
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sha1Sum := sha1.Sum(content)
	sha1Hex := hex.EncodeToString(sha1Sum[:])

	ok, err := VerifyFileHashAuto(file, sha1Hex)
	if err != nil || !ok {
		t.Errorf("sha1 auto-verify: ok=%v err=%v", ok, err)
	}

	// A sha256-length checksum that is actually sha1 must fail.
	if ok, _ := VerifyFileHashAuto(file, sha1Hex+"0000000000000000000000000000"); ok {
		t.Error("mismatched checksum verified as ok")
	}

	if algo := AlgoForChecksum(sha1Hex); algo != HashAlgoSHA1 {
		t.Errorf("AlgoForChecksum(sha1) = %s", algo)
	}
	sha256Sum := sha256.Sum256(content)
	if algo := AlgoForChecksum(hex.EncodeToString(sha256Sum[:])); algo != HashAlgoSHA256 {
		t.Errorf("AlgoForChecksum(sha256) = %s", algo)
	}
}
