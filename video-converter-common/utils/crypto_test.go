package utils

import (
	"strings"
	"testing"
)

func TestGenerateRandomBytes(t *testing.T) {
	sizes := []int{16, 32, 64}
	for _, size := range sizes {
		b, err := GenerateRandomBytes(size)
		if err != nil {
			t.Fatalf("GenerateRandomBytes(%d) error = %v", size, err)
		}
		if len(b) != size {
			t.Errorf("GenerateRandomBytes(%d) length = %d, want %d", size, len(b), size)
		}
	}

	// Test that consecutive calls produce different results
	b1, _ := GenerateRandomBytes(32)
	b2, _ := GenerateRandomBytes(32)
	if string(b1) == string(b2) {
		t.Error("GenerateRandomBytes() produced identical results")
	}
}

func TestGenerateRandomString(t *testing.T) {
	s, err := GenerateRandomString(16)
	if err != nil {
		t.Fatalf("GenerateRandomString(16) error = %v", err)
	}
	if len(s) == 0 {
		t.Error("GenerateRandomString(16) returned empty string")
	}

	// Test that consecutive calls produce different results
	s1, _ := GenerateRandomString(16)
	s2, _ := GenerateRandomString(16)
	if s1 == s2 {
		t.Error("GenerateRandomString() produced identical results")
	}
}

func TestGenerateRandomHex(t *testing.T) {
	s, err := GenerateRandomHex(16)
	if err != nil {
		t.Fatalf("GenerateRandomHex(16) error = %v", err)
	}
	if len(s) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("GenerateRandomHex(16) length = %d, want 32", len(s))
	}

	// Verify it's valid hex
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("GenerateRandomHex() produced invalid hex char: %c", c)
		}
	}
}

func TestHashSHA256(t *testing.T) {
	data := []byte("test data")
	hash := HashSHA256(data)

	// SHA256 produces 64 hex characters
	if len(hash) != 64 {
		t.Errorf("HashSHA256() length = %d, want 64", len(hash))
	}

	// Same input should produce same hash
	hash2 := HashSHA256(data)
	if hash != hash2 {
		t.Error("HashSHA256() produced different hashes for same input")
	}

	// Different input should produce different hash
	hash3 := HashSHA256([]byte("different data"))
	if hash == hash3 {
		t.Error("HashSHA256() produced same hash for different inputs")
	}
}

func TestHashSHA256String(t *testing.T) {
	s := "test string"
	hash := HashSHA256String(s)

	if len(hash) != 64 {
		t.Errorf("HashSHA256String() length = %d, want 64", len(hash))
	}

	// Should match HashSHA256 with byte slice
	expected := HashSHA256([]byte(s))
	if hash != expected {
		t.Error("HashSHA256String() doesn't match HashSHA256()")
	}
}

func TestConstantTimeCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"same", "same", true},
		{"different", "strings", false},
		{"short", "shorter", false},
		{"", "", true},
	}

	for _, tt := range tests {
		got := ConstantTimeCompare(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("ConstantTimeCompare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSecureWipe(t *testing.T) {
	data := []byte("sensitive data")
	SecureWipe(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("SecureWipe() byte %d = %d, want 0", i, b)
		}
	}
}

func TestGenerateID(t *testing.T) {
	id, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID() error = %v", err)
	}

	if len(id) != 16 {
		t.Errorf("GenerateID() length = %d, want 16", len(id))
	}

	// Test uniqueness
	id2, _ := GenerateID()
	if id == id2 {
		t.Error("GenerateID() produced duplicate IDs")
	}
}

func TestGenerateSecureToken(t *testing.T) {
	token, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("GenerateSecureToken() error = %v", err)
	}

	if len(token) != 32 {
		t.Errorf("GenerateSecureToken() length = %d, want 32", len(token))
	}

	// Test uniqueness
	token2, _ := GenerateSecureToken()
	if token == token2 {
		t.Error("GenerateSecureToken() produced duplicate tokens")
	}
}
