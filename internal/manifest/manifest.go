package manifest

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Manifest represents a parsed SHA256SUMS manifest
type Manifest struct {
	URL   string            // Base URL where manifest was fetched from
	Files map[string]string // filename -> SHA256 hash
}

// Fetch downloads and parses a SHA256SUMS manifest from the given base URL
// If verify is true, it will also verify the GPG signature
func Fetch(baseURL string, verify bool) (*Manifest, error) {
	manifestURL := strings.TrimRight(baseURL, "/") + "/SHA256SUMS"

	// Download manifest
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest fetch failed with status: %s", resp.Status)
	}

	// Read manifest content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Verify GPG signature if requested
	if verify {
		sigURL := manifestURL + ".gpg"
		if err := verifySignature(client, sigURL, content); err != nil {
			return nil, fmt.Errorf("signature verification failed: %w", err)
		}
	}

	// Parse manifest
	m, err := parseManifest(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	m.URL = baseURL
	return m, nil
}

// parseManifest parses SHA256SUMS format content
func parseManifest(content []byte) (*Manifest, error) {
	m := &Manifest{
		Files: make(map[string]string),
	}

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: <hash>  <filename> or <hash> *<filename> (binary mode)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		hash := parts[0]
		filename := parts[1]

		// Remove leading * or space indicator
		filename = strings.TrimPrefix(filename, "*")
		filename = strings.TrimPrefix(filename, " ")

		// Validate hash length (SHA256 = 64 hex chars)
		if len(hash) != 64 {
			continue
		}

		m.Files[filename] = strings.ToLower(hash)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return m, nil
}

// VerifyHash verifies that a file's SHA256 hash matches the expected value
func VerifyHash(filePath string, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}

	actualHash := fmt.Sprintf("%x", h.Sum(nil))
	if actualHash != strings.ToLower(expectedHash) {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// VerifyHashReader verifies SHA256 hash while reading, returns a reader that checks on close
func VerifyHashReader(r io.Reader, expectedHash string) *HashVerifyReader {
	return &HashVerifyReader{
		reader:       r,
		hasher:       sha256.New(),
		expectedHash: strings.ToLower(expectedHash),
	}
}

// HashVerifyReader wraps a reader and computes SHA256 while reading
type HashVerifyReader struct {
	reader       io.Reader
	hasher       io.Writer
	expectedHash string
	actualHash   string
}

func (h *HashVerifyReader) Read(p []byte) (n int, err error) {
	n, err = h.reader.Read(p)
	if n > 0 {
		h.hasher.Write(p[:n])
	}
	if err == io.EOF {
		// Compute final hash
		if hasher, ok := h.hasher.(interface{ Sum([]byte) []byte }); ok {
			h.actualHash = fmt.Sprintf("%x", hasher.Sum(nil))
		}
	}
	return n, err
}

// Verify checks if the hash matches after reading is complete
func (h *HashVerifyReader) Verify() error {
	if h.actualHash == "" {
		return fmt.Errorf("file not fully read yet")
	}
	if h.actualHash != h.expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", h.expectedHash, h.actualHash)
	}
	return nil
}
