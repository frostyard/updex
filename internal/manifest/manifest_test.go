package manifest

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	// SHA256 hashes are exactly 64 hex characters
	hash1 := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	hash2 := "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"

	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name:    "standard format",
			content: hash1 + "  file1.raw\n" + hash2 + "  file2.raw.xz",
			expected: map[string]string{
				"file1.raw":    hash1,
				"file2.raw.xz": hash2,
			},
		},
		{
			name:    "binary mode indicator",
			content: hash1 + " *file1.raw\n" + hash2 + " *file2.raw",
			expected: map[string]string{
				"file1.raw": hash1,
				"file2.raw": hash2,
			},
		},
		{
			name:    "with comments and empty lines",
			content: "# This is a comment\n" + hash1 + "  file1.raw\n\n# Another comment\n" + hash2 + "  file2.raw\n",
			expected: map[string]string{
				"file1.raw": hash1,
				"file2.raw": hash2,
			},
		},
		{
			name:    "uppercase hash normalized",
			content: "A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2  file1.raw",
			expected: map[string]string{
				"file1.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			},
		},
		{
			name:     "empty content",
			content:  "",
			expected: map[string]string{},
		},
		{
			name:    "invalid hash length ignored",
			content: "abc123  file1.raw\n" + hash1 + "  file2.raw",
			expected: map[string]string{
				"file2.raw": hash1,
			},
		},
		{
			name:    "single field lines ignored",
			content: "onlyonefield\n" + hash1 + "  file1.raw",
			expected: map[string]string{
				"file1.raw": hash1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := parseManifest([]byte(tt.content))
			if err != nil {
				t.Fatalf("parseManifest() error = %v", err)
			}

			if len(m.Files) != len(tt.expected) {
				t.Errorf("got %d files, want %d", len(m.Files), len(tt.expected))
			}

			for filename, expectedHash := range tt.expected {
				actualHash, ok := m.Files[filename]
				if !ok {
					t.Errorf("missing file %q in manifest", filename)
					continue
				}
				if actualHash != expectedHash {
					t.Errorf("Files[%q] = %q, want %q", filename, actualHash, expectedHash)
				}
			}
		})
	}
}

func TestVerifyHash(t *testing.T) {
	// Create a temp file with known content
	tmpFile, err := os.CreateTemp("", "hash-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("hello world\n")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Compute expected hash
	h := sha256.New()
	h.Write(content)
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))

	// Test successful verification
	if err := VerifyHash(tmpFile.Name(), expectedHash); err != nil {
		t.Errorf("VerifyHash() with correct hash error = %v", err)
	}

	// Test failed verification
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := VerifyHash(tmpFile.Name(), wrongHash); err == nil {
		t.Error("VerifyHash() with wrong hash should return error")
	}

	// Test uppercase hash
	if err := VerifyHash(tmpFile.Name(), strings.ToUpper(expectedHash)); err != nil {
		t.Errorf("VerifyHash() with uppercase hash error = %v", err)
	}
}

func TestVerifyHashNonexistentFile(t *testing.T) {
	err := VerifyHash("/nonexistent/file/path", "somehash")
	if err == nil {
		t.Error("VerifyHash() should return error for nonexistent file")
	}
}

func TestHashVerifyReader(t *testing.T) {
	content := []byte("test content for hashing")

	// Compute expected hash
	h := sha256.New()
	h.Write(content)
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))

	// Test with HashVerifyReader
	reader := VerifyHashReader(strings.NewReader(string(content)), expectedHash)

	// Read all content
	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if string(result) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(result), string(content))
	}

	// Verify should succeed
	if err := reader.Verify(); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
}

func TestHashVerifyReaderWrongHash(t *testing.T) {
	content := []byte("test content")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	reader := VerifyHashReader(strings.NewReader(string(content)), wrongHash)

	// Read all content
	_, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	// Verify should fail
	if err := reader.Verify(); err == nil {
		t.Error("Verify() should return error for wrong hash")
	}
}

func TestHashVerifyReaderNotFullyRead(t *testing.T) {
	content := []byte("test content that is longer than what we will read")
	h := sha256.New()
	h.Write(content)
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))

	reader := VerifyHashReader(strings.NewReader(string(content)), expectedHash)

	// Only read part of the content
	buf := make([]byte, 10)
	_, _ = reader.Read(buf)

	// Verify should fail because not fully read
	if err := reader.Verify(); err == nil {
		t.Error("Verify() should return error when not fully read")
	}
}

func TestHashVerifyReaderPartialReads(t *testing.T) {
	content := []byte("this is a longer piece of content for testing partial reads")

	// Compute expected hash
	h := sha256.New()
	h.Write(content)
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))

	reader := VerifyHashReader(strings.NewReader(string(content)), expectedHash)

	// Read in small chunks
	var result []byte
	buf := make([]byte, 5)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}

	if string(result) != string(content) {
		t.Errorf("content mismatch after partial reads")
	}

	// Verify should succeed
	if err := reader.Verify(); err != nil {
		t.Errorf("Verify() after partial reads error = %v", err)
	}
}
