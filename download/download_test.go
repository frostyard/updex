package download

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func hashString(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}
