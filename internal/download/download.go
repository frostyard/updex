package download

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

// Download fetches a file from URL, verifies its hash, decompresses if needed,
// and atomically writes it to the target path
func Download(url, targetPath, expectedHash string, mode uint32) error {
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
		tmpFile.Close()
		os.Remove(tmpPath) // Clean up on failure
	}()

	// Download the file
	client := &http.Client{
		Timeout: 10 * time.Minute, // Long timeout for large files
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// Set up progress bar
	bar := progressbar.NewOptions64(
		resp.ContentLength,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	// Compute hash while downloading
	hasher := sha256.New()
	reader := io.TeeReader(resp.Body, hasher)

	// Write to temp file with progress
	_, err = io.Copy(io.MultiWriter(tmpFile, bar), reader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Verify hash of compressed file
	actualHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if actualHash != strings.ToLower(expectedHash) {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	// Close temp file before decompression
	tmpFile.Close()

	// Determine if decompression is needed and get final path
	finalPath := targetPath
	decompressedPath := tmpPath + ".decompressed"

	compressionType := detectCompression(url)
	if compressionType != "" {
		// Decompress to another temp file
		if err := decompressFile(tmpPath, decompressedPath, compressionType); err != nil {
			os.Remove(decompressedPath)
			return fmt.Errorf("decompression failed: %w", err)
		}
		// Remove compressed temp and use decompressed
		os.Remove(tmpPath)
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
		os.Remove(tmpPath)
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

// copyFile copies a file with the given mode
func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
