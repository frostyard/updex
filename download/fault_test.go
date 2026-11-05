package download

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/internal/fault"
)

// TestDownloadMarksFailureClass checks the fault class of each failure
// Download marks, and that the mark keeps the error text of today.
func TestDownloadMarksFailureClass(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantClass fault.Class
		wantText  string
	}{
		{name: "404", status: http.StatusNotFound, wantClass: fault.HTTPStatus, wantText: "download failed with status: 404 Not Found"},
		{name: "503 after retries", status: http.StatusServiceUnavailable, wantClass: fault.HTTPStatus, wantText: "download failed with status: 503 Service Unavailable"},
		{name: "hash mismatch", status: http.StatusOK, body: "other content", wantClass: fault.Checksum,
			wantText: "hash mismatch: expected " + hashString([]byte("expected content")) + ", got " + hashString([]byte("other content"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			target := filepath.Join(t.TempDir(), "feature.raw")
			err := Download(t.Context(), server.Client(), server.URL+"/feature.raw", target, hashString([]byte("expected content")), 0o644, nil, WithRetryConfig(2, time.Millisecond))
			if err == nil {
				t.Fatal("Download() error = nil")
			}
			if err.Error() != tt.wantText {
				t.Errorf("Error() = %q, want the unchanged text %q", err.Error(), tt.wantText)
			}
			if class, ok := fault.ClassOf(err); !ok || class != tt.wantClass {
				t.Errorf("ClassOf() = %q, %v, want %q", class, ok, tt.wantClass)
			}
		})
	}
}
