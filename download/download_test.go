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

func TestDownloadMaxDecompressedSize(t *testing.T) {
	content := bytes.Repeat([]byte("a"), 2<<20)
	compressed := gzipBytes(t, content)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	t.Run("rejects oversized decompressed output and cleans up", func(t *testing.T) {
		targetDir := t.TempDir()
		targetPath := filepath.Join(targetDir, "feature.raw")
		err := Download(
			t.Context(),
			server.Client(),
			server.URL+"/feature.raw.gz",
			targetPath,
			hashString(compressed),
			0644,
			nil,
			WithMaxDecompressedSize(1<<20),
		)
		if !errors.Is(err, ErrDecompressedTooLarge) {
			t.Fatalf("Download() error = %v, want ErrDecompressedTooLarge", err)
		}
		if !strings.Contains(err.Error(), "exceeds the maximum size") {
			t.Errorf("Download() error = %q, want maximum-size message", err)
		}
		if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
			t.Errorf("Stat(target) error = %v, want not-exist", err)
		}
		for _, pattern := range []string{".updex-download-*", "*.decompressed"} {
			matches, err := filepath.Glob(filepath.Join(targetDir, pattern))
			if err != nil {
				t.Fatalf("Glob(%q) error = %v", pattern, err)
			}
			if len(matches) != 0 {
				t.Errorf("temporary files matching %q remain: %v", pattern, matches)
			}
		}
	})

	t.Run("default limit accepts the same stream", func(t *testing.T) {
		targetPath := filepath.Join(t.TempDir(), "feature.raw")
		if err := Download(t.Context(), server.Client(), server.URL+"/feature.raw.gz", targetPath, hashString(compressed), 0644, nil); err != nil {
			t.Fatalf("Download() error = %v", err)
		}
		got, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Errorf("downloaded content differs from decompressed fixture")
		}
	})

	t.Run("exact limit accepts the same stream", func(t *testing.T) {
		targetPath := filepath.Join(t.TempDir(), "feature.raw")
		if err := Download(t.Context(), server.Client(), server.URL+"/feature.raw.gz", targetPath, hashString(compressed), 0644, nil, WithMaxDecompressedSize(int64(len(content)))); err != nil {
			t.Fatalf("Download() error = %v", err)
		}
	})
}

func TestDownloadSyncsFileBeforeRename(t *testing.T) {
	content := []byte("feature image contents")
	tests := []struct {
		name       string
		urlSuffix  string
		body       []byte
		wantSuffix string
	}{
		{name: "uncompressed", urlSuffix: "/feature.raw", body: content, wantSuffix: ""},
		{name: "gzip", urlSuffix: "/feature.raw.gz", body: gzipBytes(t, content), wantSuffix: ".decompressed"},
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
				if !strings.HasPrefix(info.Name(), ".updex-download-") || !strings.HasSuffix(info.Name(), tt.wantSuffix) {
					t.Errorf("synced file name = %q, want temp file %q*%q", info.Name(), ".updex-download-", tt.wantSuffix)
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

func TestCopyFileCopiesContentAndMode(t *testing.T) {
	content := []byte("cross-device image contents")
	srcPath := filepath.Join(t.TempDir(), "source.raw")
	if err := os.WriteFile(srcPath, content, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	dstPath := filepath.Join(t.TempDir(), "target.raw")

	if err := copyFile(srcPath, dstPath, 0750); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("target content = %q, want %q", got, content)
	}
	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0750 {
		t.Errorf("target mode = %o, want 750", gotMode)
	}
}

func TestCopyFileCloseFailurePreservesTarget(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "source.raw")
	if err := os.WriteFile(srcPath, []byte("replacement"), 0600); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}
	targetDir := t.TempDir()
	dstPath := filepath.Join(targetDir, "target.raw")
	original := []byte("installed image")
	if err := os.WriteFile(dstPath, original, 0600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}

	closeErr := errors.New("injected close failure")
	realClose := closeCopyFile
	t.Cleanup(func() { closeCopyFile = realClose })
	closeCopyFile = func(f *os.File) error {
		if err := f.Close(); err != nil {
			return err
		}
		return closeErr
	}

	err := copyFile(srcPath, dstPath, 0644)
	if !errors.Is(err, closeErr) {
		t.Fatalf("copyFile() error = %v, want it to wrap %v", err, closeErr)
	}
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("target content = %q, want preserved %q", got, original)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dstPath) {
		t.Errorf("target directory entries = %v, want only %q", entries, filepath.Base(dstPath))
	}
}

func hashString(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}

func TestDownloadRejectsOversizeBody(t *testing.T) {
	// The server streams far more than the configured ceiling. Download must
	// stop at the bound and fail, never writing the whole body to disk.
	const limit = 1024
	body := bytes.Repeat([]byte("x"), limit*8)
	var served atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n, _ := w.Write(body)
		served.Add(int64(n))
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, hashString(body), 0644, nil, WithMaxDownloadSize(limit))
	if !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("Download() error = %v, want ErrDownloadTooLarge", err)
	}
	if _, statErr := os.Stat(targetPath); !os.IsNotExist(statErr) {
		t.Fatalf("target file exists after an oversize download was rejected: %v", statErr)
	}
	// Nothing oversized should remain staged next to the target either.
	entries, err := os.ReadDir(filepath.Dir(targetPath))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		if info.Size() > limit+1 {
			t.Fatalf("a %d-byte temp file was left behind; the download was not bounded", info.Size())
		}
	}
}

func TestDownloadRejectsOversizeContentLength(t *testing.T) {
	// A hostile Content-Length is rejected before any streaming begins.
	const limit = 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", int64(limit)*100))
		w.WriteHeader(http.StatusOK)
		// Write a little and stop; the declared length is what matters here.
		_, _ = w.Write(bytes.Repeat([]byte("x"), 16))
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, hashString([]byte("unused")), 0644, nil, WithMaxDownloadSize(limit))
	if !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("Download() error = %v, want ErrDownloadTooLarge from the Content-Length pre-check", err)
	}
}

func TestDownloadWithinLimitSucceeds(t *testing.T) {
	// An ordinary download comfortably under the ceiling still succeeds and its
	// hash is verified.
	content := []byte("a legitimately sized image payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", targetPath, hashString(content), 0644, nil, WithMaxDownloadSize(int64(len(content))+1))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestDownloadDefaultClientRejectsRedirectDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	withDefaultTransport(t, secure.Client().Transport)

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), nil, secure.URL+"/feature.raw", targetPath, hashString([]byte("payload")), 0644, nil, WithRetryConfig(1, time.Millisecond))
	if err == nil {
		t.Fatal("Download() error = nil, want redirect downgrade error")
	}
	if !strings.Contains(err.Error(), "redirect downgrade") {
		t.Errorf("Download() error = %q, want redirect downgrade", err)
	}
}

func TestDownloadDefaultClientAllowsSameSchemeRedirect(t *testing.T) {
	content := []byte("payload")
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feature.raw" {
			http.Redirect(w, r, server.URL+"/final", http.StatusFound)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	withDefaultTransport(t, server.Client().Transport)

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), nil, server.URL+"/feature.raw", targetPath, hashString(content), 0644, nil, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestDownloadCustomClientKeepsOwnRedirectPolicy(t *testing.T) {
	content := []byte("payload")
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	custom := secure.Client()
	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), custom, secure.URL+"/feature.raw", targetPath, hashString(content), 0644, nil, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v, want a caller-supplied client to keep following the downgrade redirect", err)
	}
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestDownloadDefaultClientKeepsTenMinuteTimeout(t *testing.T) {
	content := []byte("payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
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

	targetPath := filepath.Join(t.TempDir(), "feature.raw")
	err := Download(t.Context(), nil, server.URL+"/feature.raw", targetPath, hashString(content), 0644, nil, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if !hasDeadline {
		t.Fatal("request had no deadline, want the 10-minute default Timeout applied")
	}
	if got := time.Until(deadline); got <= 9*time.Minute || got > 10*time.Minute {
		t.Errorf("deadline ~%v from now, want (9m, 10m]", got)
	}
}

// roundTripFunc adapts a function to http.RoundTripper, so tests can observe
// the request (e.g. its context deadline) before delegating to a real
// transport.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// withDefaultTransport temporarily overrides http.DefaultTransport for tests
// exercising a nil httpClient, which falls back to it. Download tests never
// run in parallel, so this is safe without additional synchronization.
func withDefaultTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	original := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = original })
}
