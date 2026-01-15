package download

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func TestDecompressReaderGzip(t *testing.T) {
	// Create gzip compressed data
	original := []byte("hello world, this is test data for gzip compression")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	gw.Close()

	// Test decompression
	reader, err := DecompressReader(&buf, "gz")
	if err != nil {
		t.Fatalf("DecompressReader() error = %v", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch: got %q, want %q", string(result), string(original))
	}
}

func TestDecompressReaderZstd(t *testing.T) {
	// Create zstd compressed data
	original := []byte("hello world, this is test data for zstd compression")
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create zstd writer: %v", err)
	}
	if _, err := zw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	zw.Close()

	// Test decompression
	reader, err := DecompressReader(bytes.NewReader(buf.Bytes()), "zstd")
	if err != nil {
		t.Fatalf("DecompressReader() error = %v", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch: got %q, want %q", string(result), string(original))
	}
}

func TestDecompressReaderXZ(t *testing.T) {
	// Create xz compressed data
	original := []byte("hello world, this is test data for xz compression")
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	if _, err := xw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	xw.Close()

	// Test decompression
	reader, err := DecompressReader(bytes.NewReader(buf.Bytes()), "xz")
	if err != nil {
		t.Fatalf("DecompressReader() error = %v", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch: got %q, want %q", string(result), string(original))
	}
}

func TestDecompressReaderNoCompression(t *testing.T) {
	original := []byte("plain text data, no compression")
	reader, err := DecompressReader(bytes.NewReader(original), "")
	if err != nil {
		t.Fatalf("DecompressReader() error = %v", err)
	}
	defer reader.Close()

	result, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("content mismatch: got %q, want %q", string(result), string(original))
	}
}

func TestDecompressReaderUnsupported(t *testing.T) {
	_, err := DecompressReader(bytes.NewReader([]byte{}), "unsupported")
	if err == nil {
		t.Error("expected error for unsupported compression type")
	}
}

func TestDetectCompression(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"file.raw.xz", "xz"},
		{"file.raw.XZ", "xz"},
		{"file.raw.gz", "gz"},
		{"file.raw.GZ", "gz"},
		{"file.raw.zst", "zstd"},
		{"file.raw.zstd", "zstd"},
		{"file.raw.ZSTD", "zstd"},
		{"file.raw", ""},
		{"file.txt", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := detectCompression(tt.filename)
			if result != tt.expected {
				t.Errorf("detectCompression(%q) = %q, want %q", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestDecompressFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create gzip compressed file
	original := []byte("test data for file decompression")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	gw.Close()

	srcPath := tmpDir + "/test.gz"
	dstPath := tmpDir + "/test.out"

	if err := os.WriteFile(srcPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write compressed file: %v", err)
	}

	// Test decompression
	if err := decompressFile(srcPath, dstPath, "gz"); err != nil {
		t.Fatalf("decompressFile() error = %v", err)
	}

	// Verify result
	result, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read decompressed file: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch")
	}
}

func TestDecompressFileZstd(t *testing.T) {
	tmpDir := t.TempDir()

	original := []byte("test data for zstd file decompression")
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create zstd writer: %v", err)
	}
	if _, err := zw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	zw.Close()

	srcPath := tmpDir + "/test.zst"
	dstPath := tmpDir + "/test.out"

	if err := os.WriteFile(srcPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write compressed file: %v", err)
	}

	if err := decompressFile(srcPath, dstPath, "zstd"); err != nil {
		t.Fatalf("decompressFile() error = %v", err)
	}

	result, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read decompressed file: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch")
	}
}

func TestDecompressFileXZ(t *testing.T) {
	tmpDir := t.TempDir()

	original := []byte("test data for xz file decompression")
	var buf bytes.Buffer
	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create xz writer: %v", err)
	}
	if _, err := xw.Write(original); err != nil {
		t.Fatalf("failed to compress test data: %v", err)
	}
	xw.Close()

	srcPath := tmpDir + "/test.xz"
	dstPath := tmpDir + "/test.out"

	if err := os.WriteFile(srcPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write compressed file: %v", err)
	}

	if err := decompressFile(srcPath, dstPath, "xz"); err != nil {
		t.Fatalf("decompressFile() error = %v", err)
	}

	result, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read decompressed file: %v", err)
	}

	if !bytes.Equal(result, original) {
		t.Errorf("decompressed content mismatch")
	}
}

func TestDecompressFileUnsupported(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := tmpDir + "/test.bin"
	dstPath := tmpDir + "/test.out"

	if err := os.WriteFile(srcPath, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := decompressFile(srcPath, dstPath, "unsupported")
	if err == nil {
		t.Error("expected error for unsupported compression type")
	}
}

func TestDecompressFileNonexistentSource(t *testing.T) {
	tmpDir := t.TempDir()
	err := decompressFile("/nonexistent/path", tmpDir+"/test.out", "gz")
	if err == nil {
		t.Error("expected error for nonexistent source file")
	}
}
