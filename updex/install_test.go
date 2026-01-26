package updex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
)

func TestInstall(t *testing.T) {
	tests := []struct {
		name            string
		setupServer     func() *httptest.Server
		component       string
		opts            InstallOptions
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "missing component name - returns error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			component:       "",
			opts:            InstallOptions{Component: "", NoRefresh: true},
			wantErr:         true,
			wantErrContains: "component name is required",
		},
		{
			name: "component not found in index - returns error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/ext/index") {
						// Return index without the requested component
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("other-ext\nanother-ext\n"))
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			component:       "myext",
			opts:            InstallOptions{Component: "myext", NoRefresh: true},
			wantErr:         true,
			wantErrContains: "not found in repository",
		},
		{
			name: "index fetch fails - returns error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/ext/index") {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			component:       "myext",
			opts:            InstallOptions{Component: "myext", NoRefresh: true},
			wantErr:         true,
			wantErrContains: "failed to fetch index",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()

			// Set up mock runner
			mockRunner := &sysext.MockRunner{}
			cleanup := sysext.SetRunner(mockRunner)
			defer cleanup()

			// Set up HTTP server
			var serverURL string
			if tt.setupServer != nil {
				server := tt.setupServer()
				defer server.Close()
				serverURL = server.URL
			}

			client := NewClient(ClientConfig{Definitions: configDir})
			_, err := client.Install(context.Background(), serverURL, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error to contain %q, got %q", tt.wantErrContains, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
