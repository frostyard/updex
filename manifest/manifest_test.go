package manifest

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	content := []byte("hello world\n")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

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

func TestFetchRetriesServerErrorThenSucceeds(t *testing.T) {
	content := validManifestContent()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SHA256SUMS" {
			http.NotFound(w, r)
			return
		}
		if requests.Add(1) <= 2 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	m, err := Fetch(t.Context(), server.Client(), server.URL, false, WithRetryConfig(3, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if got := m.Files["file.raw"]; got != testManifestHash() {
		t.Fatalf("Files[file.raw] = %q, want %q", got, testManifestHash())
	}
}

func TestFetchDoesNotRetryNotFound(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := Fetch(t.Context(), server.Client(), server.URL, false, WithRetryConfig(3, time.Millisecond))
	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestFetchRejectsOversizedManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", maxManifestSize+1)), maxManifestSize+1)
	}))
	defer server.Close()

	_, err := Fetch(t.Context(), server.Client(), server.URL, false, WithRetryConfig(1, time.Millisecond))
	if err == nil {
		t.Fatal("Fetch() error = nil, want oversized manifest error")
	}
	if !strings.Contains(err.Error(), "manifest response exceeds maximum allowed size") {
		t.Fatalf("Fetch() error = %q, want oversized manifest error", err)
	}
}

func TestFetchRetriesTruncatedBodyThenSucceeds(t *testing.T) {
	content := validManifestContent()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SHA256SUMS" {
			http.NotFound(w, r)
			return
		}
		if requests.Add(1) <= 2 {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)+10))
			_, _ = w.Write([]byte(content[:len(content)/2]))
			return
		}
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	m, err := Fetch(t.Context(), server.Client(), server.URL, false, WithRetryConfig(3, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	if got := m.Files["file.raw"]; got != testManifestHash() {
		t.Fatalf("Files[file.raw] = %q, want %q", got, testManifestHash())
	}
}

func TestFetchManifestResponseSizeLimit(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		unit    string
		verify  bool
		wantErr bool
	}{
		{name: "exact limit accepted", size: maxManifestSize, unit: "#\n"},
		{name: "one byte over rejected", size: maxManifestSize + 1, unit: "x", verify: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			var signatureRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/SHA256SUMS.gpg" {
					signatureRequests.Add(1)
					http.Error(w, "signature should not be fetched", http.StatusInternalServerError)
					return
				}
				if r.URL.Path != "/SHA256SUMS" {
					http.NotFound(w, r)
					return
				}
				requests.Add(1)
				remaining := tt.size
				chunk := strings.Repeat(tt.unit, 4096)
				for remaining > 0 {
					if len(chunk) > remaining {
						chunk = chunk[:remaining]
					}
					_, _ = io.WriteString(w, chunk)
					remaining -= len(chunk)
				}
			}))
			defer server.Close()

			_, err := Fetch(t.Context(), server.Client(), server.URL, tt.verify, WithRetryConfig(3, time.Millisecond))
			if tt.wantErr {
				if err == nil {
					t.Fatal("Fetch() error = nil, want oversized response error")
				}
				want := fmt.Sprintf("manifest response exceeds maximum allowed size (%d bytes)", maxManifestSize)
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Fetch() error = %q, want containing %q", err, want)
				}
			} else if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want 1", requests.Load())
			}
			if signatureRequests.Load() != 0 {
				t.Fatalf("signature requests = %d, want 0", signatureRequests.Load())
			}
		})
	}
}

func TestFetchDefaultClientRejectsRedirectDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validManifestContent()))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	withDefaultTransport(t, secure.Client().Transport)

	_, err := Fetch(t.Context(), nil, secure.URL, false, WithRetryConfig(1, time.Millisecond))
	if err == nil {
		t.Fatal("Fetch() error = nil, want redirect downgrade error")
	}
	if !strings.Contains(err.Error(), "redirect downgrade") {
		t.Errorf("Fetch() error = %q, want redirect downgrade", err)
	}
}

func TestFetchDefaultClientAllowsSameSchemeRedirect(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/SHA256SUMS" {
			http.Redirect(w, r, server.URL+"/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(validManifestContent()))
	}))
	defer server.Close()

	withDefaultTransport(t, server.Client().Transport)

	m, err := Fetch(t.Context(), nil, server.URL, false, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := m.Files["file.raw"]; got != testManifestHash() {
		t.Fatalf("Files[file.raw] = %q, want %q", got, testManifestHash())
	}
}

func TestFetchCustomClientKeepsOwnRedirectPolicy(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validManifestContent()))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	custom := secure.Client()
	m, err := Fetch(t.Context(), custom, secure.URL, false, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch() error = %v, want a caller-supplied client to keep following the downgrade redirect", err)
	}
	if got := m.Files["file.raw"]; got != testManifestHash() {
		t.Fatalf("Files[file.raw] = %q, want %q", got, testManifestHash())
	}
}

func TestFetchDefaultClientKeepsThirtySecondTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validManifestContent()))
	}))
	defer server.Close()

	original := http.DefaultTransport
	var deadline time.Time
	var hasDeadline bool
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, hasDeadline = req.Context().Deadline()
		return original.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = original }()

	if _, err := Fetch(t.Context(), nil, server.URL, false, WithRetryConfig(1, time.Millisecond)); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !hasDeadline {
		t.Fatal("request had no deadline, want the 30-second default Timeout applied")
	}
	if got := time.Until(deadline); got <= 25*time.Second || got > 30*time.Second {
		t.Errorf("deadline ~%v from now, want (25s, 30s]", got)
	}
}

// roundTripFunc adapts a function to http.RoundTripper, so tests can observe
// the request (e.g. its context deadline) before delegating to a real
// transport.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// withDefaultTransport temporarily overrides http.DefaultTransport for tests
// exercising a nil httpClient, which falls back to it. Manifest tests never
// run in parallel, so this is safe without additional synchronization.
func withDefaultTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = original })
}

func validManifestContent() string {
	return testManifestHash() + "  file.raw\n"
}

func testManifestHash() string {
	return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
