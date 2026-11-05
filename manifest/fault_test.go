package manifest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/internal/fault"
)

// TestFetchMarksFailureClass checks the fault class of each failure Fetch
// marks, and that the mark keeps the error text of today.
func TestFetchMarksFailureClass(t *testing.T) {
	tests := []struct {
		name string
		// manifestStatus is the status of /SHA256SUMS (0: serve the
		// signed content).
		manifestStatus int
		// sig serves /SHA256SUMS.gpg; nil: verification is off.
		sig       func(w http.ResponseWriter, signature []byte)
		keyring   string // "missing": no keyring file; "corrupt": an unreadable one
		wantClass fault.Class
		wantText  string
		wantIs    error
	}{
		{name: "manifest 404", manifestStatus: http.StatusNotFound, wantClass: fault.HTTPStatus, wantText: "manifest fetch failed with status: 404 Not Found"},
		{name: "manifest 503", manifestStatus: http.StatusServiceUnavailable, wantClass: fault.HTTPStatus, wantText: "manifest fetch failed with status: 503 Service Unavailable"},
		{
			name:      "signature 404",
			sig:       func(w http.ResponseWriter, _ []byte) { http.Error(w, "missing", http.StatusNotFound) },
			wantClass: fault.Signature, wantText: "signature verification failed: signature not found: signature fetch failed with status: 404 Not Found",
			wantIs: ErrSignatureNotFound,
		},
		{
			name:      "signature 403",
			sig:       func(w http.ResponseWriter, _ []byte) { http.Error(w, "no", http.StatusForbidden) },
			wantClass: fault.HTTPStatus, wantText: "signature verification failed: signature fetch failed with status: 403 Forbidden",
		},
		{
			name:      "signature 500",
			sig:       func(w http.ResponseWriter, _ []byte) { http.Error(w, "no", http.StatusInternalServerError) },
			wantClass: fault.HTTPStatus, wantText: "signature verification failed: signature fetch failed with status: 500 Internal Server Error",
		},
		{
			name:      "invalid signature",
			sig:       func(w http.ResponseWriter, _ []byte) { _, _ = w.Write([]byte("not a signature")) },
			wantClass: fault.Signature, wantText: "signature verification failed: invalid signature",
		},
		{
			name:      "no keyring",
			sig:       func(w http.ResponseWriter, s []byte) { _, _ = w.Write(s) },
			keyring:   "missing",
			wantClass: fault.Signature, wantText: "signature verification failed: failed to load keyring: no keyring found in",
		},
		{
			name:      "corrupt keyring",
			sig:       func(w http.ResponseWriter, s []byte) { _, _ = w.Write(s) },
			keyring:   "corrupt",
			wantClass: fault.Signature, wantText: "signature verification failed: failed to load keyring: failed to read keyring from",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, signature := signedManifest(t)
			switch tt.keyring {
			case "missing":
				setTestKeyringPaths(t, filepath.Join(t.TempDir(), "absent.gpg"))
			case "corrupt":
				path := filepath.Join(t.TempDir(), "corrupt.gpg")
				if err := os.WriteFile(path, []byte("not a keyring"), 0o600); err != nil {
					t.Fatal(err)
				}
				setTestKeyringPaths(t, path)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/SHA256SUMS":
					if tt.manifestStatus != 0 {
						w.WriteHeader(tt.manifestStatus)
						return
					}
					_, _ = w.Write(content)
				case "/SHA256SUMS.gpg":
					tt.sig(w, signature)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, err := Fetch(t.Context(), server.Client(), server.URL, tt.sig != nil, WithRetryConfig(1, time.Millisecond))
			if err == nil {
				t.Fatal("Fetch() error = nil")
			}
			if !strings.HasPrefix(err.Error(), tt.wantText) {
				t.Errorf("Error() = %q, want the unchanged text %q", err.Error(), tt.wantText)
			}
			if class, ok := fault.ClassOf(err); !ok || class != tt.wantClass {
				t.Errorf("ClassOf() = %q, %v, want %q", class, ok, tt.wantClass)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v) = false", tt.wantIs)
			}
		})
	}
}

// The outer "signature verification failed" wrapper carries no class: a
// network error under it keeps its own (no mark at all here).
func TestFetchSignatureNetworkErrorIsNotMarked(t *testing.T) {
	content, _ := signedManifest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/SHA256SUMS" {
			_, _ = w.Write(content)
			return
		}
		// Drop the connection without a response.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("no hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	_, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(1, time.Millisecond))
	if err == nil || !strings.HasPrefix(err.Error(), "signature verification failed: failed to fetch signature") {
		t.Fatalf("Fetch() error = %v, want the signature fetch failure", err)
	}
	if class, ok := fault.ClassOf(err); ok {
		t.Errorf("ClassOf() = %q, want no mark: the wrapper must not classify a network error", class)
	}
}

func TestHashMismatchIsChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifyHash(path, strings.Repeat("0", 64))
	if err == nil || !strings.HasPrefix(err.Error(), "hash mismatch: expected ") {
		t.Fatalf("VerifyHash() error = %v", err)
	}
	if class, _ := fault.ClassOf(err); class != fault.Checksum {
		t.Errorf("VerifyHash class = %q, want checksum", class)
	}

	r := VerifyHashReader(strings.NewReader("content"), strings.Repeat("0", 64))
	buf := make([]byte, 64)
	for {
		if _, err := r.Read(buf); err != nil {
			break
		}
	}
	err = r.Verify()
	if err == nil || !strings.HasPrefix(err.Error(), "hash mismatch: expected ") {
		t.Fatalf("Verify() error = %v", err)
	}
	if class, _ := fault.ClassOf(err); class != fault.Checksum {
		t.Errorf("Verify class = %q, want checksum", class)
	}
}
