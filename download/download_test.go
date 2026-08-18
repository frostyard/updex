package download

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadRetriesServerErrorThenSucceeds(t *testing.T) {
	content := []byte("download content")
	expectedHash := hashString(content)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, expectedHash, 0644, nil, WithRetryConfig(3, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestDownloadDoesNotRetryNotFound(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, hashString([]byte("unused")), 0644, nil, WithRetryConfig(3, time.Millisecond))
	if err == nil {
		t.Fatal("Download() error = nil, want error")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestDownloadDoesNotRetryChecksumMismatch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("different content"))
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, hashString([]byte("expected content")), 0644, nil, WithRetryConfig(3, time.Millisecond))
	if err == nil {
		t.Fatal("Download() error = nil, want error")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestDownloadRetriesTruncatedBodyThenSucceeds(t *testing.T) {
	content := []byte("complete download content")
	expectedHash := hashString(content)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)+10))
			_, _ = w.Write(content[:len(content)/2])
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, expectedHash, 0644, nil, WithRetryConfig(3, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want 3", requests.Load())
	}
}

func TestDownloadRetriesTooManyRequestsThenSucceeds(t *testing.T) {
	content := []byte("download content")
	expectedHash := hashString(content)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, expectedHash, 0644, nil, WithRetryConfig(3, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
}

func TestDownloadFailurePreservesExistingTarget(t *testing.T) {
	tests := []struct {
		name          string
		urlSuffix     string
		content       []byte
		expectedHash  string
		expectedError string
		status        int
		maxAttempts   int
		wantRequests  int
	}{
		{
			name:          "checksum mismatch",
			urlSuffix:     "/feature.raw",
			content:       []byte("corrupt replacement"),
			expectedHash:  hashString([]byte("expected replacement")),
			expectedError: "hash mismatch",
			status:        http.StatusOK,
			maxAttempts:   3,
			wantRequests:  1,
		},
		{
			name:          "decompression failure",
			urlSuffix:     "/feature.raw.gz",
			content:       []byte("not gzip data"),
			expectedHash:  hashString([]byte("not gzip data")),
			expectedError: "decompression failed",
			status:        http.StatusOK,
			maxAttempts:   3,
			wantRequests:  1,
		},
		{
			name:          "retry exhaustion",
			urlSuffix:     "/feature.raw",
			content:       []byte("temporary failure"),
			expectedError: "download failed with status",
			status:        http.StatusInternalServerError,
			maxAttempts:   2,
			wantRequests:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(tt.status)
				_, _ = w.Write(tt.content)
			}))
			defer server.Close()

			targetDir := t.TempDir()
			targetPath := filepath.Join(targetDir, "feature.raw")
			original := []byte("installed image")
			if err := os.WriteFile(targetPath, original, 0600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			err := Download(t.Context(), server.Client(), server.URL+tt.urlSuffix, targetPath, tt.expectedHash, 0644, nil, WithRetryConfig(tt.maxAttempts, time.Millisecond))
			if err == nil {
				t.Fatal("Download() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("Download() error = %q, want error containing %q", err, tt.expectedError)
			}
			if requests.Load() != int32(tt.wantRequests) {
				t.Fatalf("requests = %d, want %d", requests.Load(), tt.wantRequests)
			}

			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != string(original) {
				t.Errorf("target content = %q, want %q", got, original)
			}
			info, err := os.Stat(targetPath)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if gotMode := info.Mode().Perm(); gotMode != 0600 {
				t.Errorf("target mode = %o, want 600", gotMode)
			}

			entries, err := os.ReadDir(targetDir)
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(targetPath) {
				t.Errorf("target directory entries = %v, want only %q", entries, filepath.Base(targetPath))
			}
		})
	}
}

// recordSyncs replaces the package sync seam for the duration of the test and
// returns the identities of every file it was asked to sync, captured at sync
// time so they can be compared with os.SameFile after a later rename.
func recordSyncs(t *testing.T) *[]os.FileInfo {
	t.Helper()
	original := syncFile
	t.Cleanup(func() { syncFile = original })

	var synced []os.FileInfo
	syncFile = func(f *os.File) error {
		info, err := f.Stat()
		if err != nil {
			t.Fatalf("Stat() at sync error = %v", err)
		}
		synced = append(synced, info)
		return original(f)
	}
	return &synced
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(content); err != nil {
		t.Fatalf("gzip Write() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}
	return buf.Bytes()
}

func TestDownloadSyncsFileBeforeRename(t *testing.T) {
	content := []byte("feature image contents")
	tests := []struct {
		name      string
		urlSuffix string
		body      []byte
		wantSuffx string
	}{
		{name: "uncompressed", urlSuffix: "/feature.raw", body: content, wantSuffx: ""},
		{name: "gzip", urlSuffix: "/feature.raw.gz", body: gzipBytes(t, content), wantSuffx: ".decompressed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synced := recordSyncs(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(tt.body)
			}))
			defer server.Close()

			targetDir := t.TempDir()
			targetPath := filepath.Join(targetDir, "feature.raw")
			if err := Download(t.Context(), server.Client(), server.URL+tt.urlSuffix, targetPath, hashString(tt.body), 0644, nil); err != nil {
				t.Fatalf("Download() error = %v", err)
			}

			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != string(content) {
				t.Fatalf("target content = %q, want %q", got, content)
			}

			target, err := os.Stat(targetPath)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			found := false
			for _, info := range *synced {
				if !os.SameFile(info, target) {
					continue
				}
				found = true
				if !strings.HasPrefix(info.Name(), ".updex-download-") || !strings.HasSuffix(info.Name(), tt.wantSuffx) {
					t.Errorf("synced file name = %q, want temp file %q*%q", info.Name(), ".updex-download-", tt.wantSuffx)
				}
				if info.Size() != int64(len(content)) {
					t.Errorf("synced file size = %d, want %d", info.Size(), len(content))
				}
			}
			if !found {
				t.Errorf("no synced file is the file installed at %s (synced %d file(s))", targetPath, len(*synced))
			}
		})
	}
}

func TestDownloadSyncFailureLeavesNoTarget(t *testing.T) {
	content := []byte("feature image contents")
	tests := []struct {
		name           string
		urlSuffix      string
		body           []byte
		existingTarget bool
	}{
		{name: "uncompressed without target", urlSuffix: "/feature.raw", body: content},
		{name: "uncompressed preserves target", urlSuffix: "/feature.raw", body: content, existingTarget: true},
		{name: "gzip without target", urlSuffix: "/feature.raw.gz", body: gzipBytes(t, content)},
		{name: "gzip preserves target", urlSuffix: "/feature.raw.gz", body: gzipBytes(t, content), existingTarget: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncErr := errors.New("injected sync failure")
			realSync := syncFile
			t.Cleanup(func() { syncFile = realSync })
			syncFile = func(*os.File) error { return syncErr }

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(tt.body)
			}))
			defer server.Close()

			targetDir := t.TempDir()
			targetPath := filepath.Join(targetDir, "feature.raw")
			original := []byte("installed image")
			if tt.existingTarget {
				if err := os.WriteFile(targetPath, original, 0600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}

			err := Download(t.Context(), server.Client(), server.URL+tt.urlSuffix, targetPath, hashString(tt.body), 0644, nil, WithRetryConfig(3, time.Millisecond))
			if err == nil {
				t.Fatal("Download() error = nil, want error")
			}
			if !errors.Is(err, syncErr) {
				t.Errorf("Download() error = %v, want it to wrap %v", err, syncErr)
			}

			entries, err := os.ReadDir(targetDir)
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if !tt.existingTarget {
				if len(entries) != 0 {
					t.Fatalf("target directory entries = %v, want none", entries)
				}
				return
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(targetPath) {
				t.Errorf("target directory entries = %v, want only %q", entries, filepath.Base(targetPath))
			}
			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != string(original) {
				t.Errorf("target content = %q, want %q", got, original)
			}
			info, err := os.Stat(targetPath)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			if gotMode := info.Mode().Perm(); gotMode != 0600 {
				t.Errorf("target mode = %o, want 600", gotMode)
			}
		})
	}
}

func hashString(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}
