// Package testutil provides test utilities for the updex project.
package testutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServerFiles configures the test HTTP server's responses.
type TestServerFiles struct {
	// Files maps filename to SHA256 hash (appears in SHA256SUMS manifest)
	Files map[string]string
	// Content maps filename to file content (for downloads)
	Content map[string][]byte
}

// NewTestServer creates an httptest.Server that serves SHA256SUMS manifest and files.
// The server serves:
//   - /SHA256SUMS - manifest with hash + filename entries
//   - /{filename} - file content from Content map
func NewTestServer(t *testing.T, files TestServerFiles) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path == "SHA256SUMS" {
			var lines []string
			for filename, hash := range files.Files {
				lines = append(lines, fmt.Sprintf("%s  %s", hash, filename))
			}
			w.Write([]byte(strings.Join(lines, "\n")))
			return
		}

		if content, ok := files.Content[path]; ok {
			w.Write(content)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// NewErrorServer creates a server that always returns the given status code.
func NewErrorServer(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
}
