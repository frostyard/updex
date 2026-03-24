package download

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProgressFunc is called before downloading begins with the response content
// length (-1 if unknown). It returns an io.Writer that will receive downloaded
// bytes for progress tracking. Return nil to disable progress tracking.
type ProgressFunc func(contentLength int64) io.Writer

// Download fetches a file from URL, verifies its hash, decompresses if needed,
// and atomically writes it to the target path. If httpClient is nil, a default
// client with a 10-minute timeout is used. If onProgress is non-nil, it is
// called with the content length after the HTTP response is received, and the
// returned writer receives downloaded bytes for progress tracking.
func Download(ctx context.Context, httpClient *http.Client, url, targetPath, expectedHash string, mode uint32, onProgress ProgressFunc) error {
	// Create target directory if needed
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Create temporary file in same directory for atomic rename
	tmpFile, err := os.CreateTemp(targetDir, ".updex-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath) // Clean up on failure
	}()

	// Download the file
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Minute,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Compute hash while downloading
	hasher := sha256.New()
	reader := io.TeeReader(resp.Body, hasher)

	// Write to temp file with optional progress
	var dst io.Writer = tmpFile
	if onProgress != nil {
		if pw := onProgress(resp.ContentLength); pw != nil {
			dst = io.MultiWriter(tmpFile, pw)
		}
	}
	_, err = io.Copy(dst, reader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Verify hash of compressed file
	actualHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if actualHash != strings.ToLower(expectedHash) {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	// Close temp file before decompression
	_ = tmpFile.Close()

	// Determine if decompression is needed and get final path
	finalPath := targetPath
	decompressedPath := tmpPath + ".decompressed"

	compressionType := detectCompression(url)
	if compressionType != "" {
		// Decompress to another temp file
		if err := decompressFile(tmpPath, decompressedPath, compressionType); err != nil {
			_ = os.Remove(decompressedPath)
			return fmt.Errorf("decompression failed: %w", err)
		}
		// Remove compressed temp and use decompressed
		_ = os.Remove(tmpPath)
		tmpPath = decompressedPath
	}

	// Set file mode
	if mode == 0 {
		mode = 0644
	}
	if err := os.Chmod(tmpPath, os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to set file mode: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tmpPath, finalPath); err != nil {
		// Cross-device link? Try copy instead
		if err := copyFile(tmpPath, finalPath, os.FileMode(mode)); err != nil {
			return fmt.Errorf("failed to move file to target: %w", err)
		}
		_ = os.Remove(tmpPath)
	}

	return nil
}

// detectCompression determines compression type from filename
func detectCompression(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".xz"):
		return "xz"
	case strings.HasSuffix(lower, ".gz"):
		return "gz"
	case strings.HasSuffix(lower, ".zst"), strings.HasSuffix(lower, ".zstd"):
		return "zstd"
	default:
		return ""
	}
}

// copyFile atomically copies a file with the given mode. It writes to a temp
// file on the destination device, syncs to disk, then renames into place.
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	// Create temp file on the same device as dst for atomic rename
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".updex-copy-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // clean up on failure

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		_ = tmpFile.Close()
		return err
	}

	// Ensure data is persisted to disk before the atomic rename
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	_ = tmpFile.Close()

	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}

	return os.Rename(tmpPath, dst)
}
