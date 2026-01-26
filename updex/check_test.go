package updex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/testutil"
)

func TestCheckNew(t *testing.T) {
	tests := []struct {
		name            string
		setupServer     func(*testing.T) *testutil.TestServerFiles
		setupConfig     func(*testing.T, string, string) // (configDir, serverURL)
		setupTarget     func(*testing.T, string)         // targetDir
		opts            CheckOptions
		wantUpdateAvail bool
		wantCurrentVer  string
		wantNewestVer   string
		wantResultCount int
		wantErr         bool
	}{
		{
			name: "update available - remote has newer version",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
						"myext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// Installed version 1.0.0
				if err := os.WriteFile(filepath.Join(targetDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:            CheckOptions{},
			wantUpdateAvail: true,
			wantCurrentVer:  "1.0.0",
			wantNewestVer:   "2.0.0",
			wantResultCount: 1,
			wantErr:         false,
		},
		{
			name: "up to date - installed matches newest",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
						"myext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// Installed latest version 2.0.0
				if err := os.WriteFile(filepath.Join(targetDir, "myext_2.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:            CheckOptions{},
			wantUpdateAvail: false,
			wantCurrentVer:  "2.0.0",
			wantNewestVer:   "2.0.0",
			wantResultCount: 1,
			wantErr:         false,
		},
		{
			name: "no installed versions - update available",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// No installed files
			},
			opts:            CheckOptions{},
			wantUpdateAvail: true,
			wantCurrentVer:  "",
			wantNewestVer:   "1.0.0",
			wantResultCount: 1,
			wantErr:         false,
		},
		{
			name: "remote fetch fails - component skipped",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return nil // Will use error server
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				if err := os.WriteFile(filepath.Join(targetDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:            CheckOptions{},
			wantResultCount: 0, // Component skipped due to HTTP error
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			targetDir := t.TempDir()

			// Set up mock runner
			mockRunner := &sysext.MockRunner{}
			cleanup := sysext.SetRunner(mockRunner)
			defer cleanup()

			// Set up HTTP server
			var serverURL string
			if tt.setupServer != nil {
				files := tt.setupServer(t)
				if files != nil {
					server := testutil.NewTestServer(t, *files)
					defer server.Close()
					serverURL = server.URL
				} else {
					// Use error server
					server := testutil.NewErrorServer(t, 500)
					defer server.Close()
					serverURL = server.URL
				}
			}

			// Set up config and target
			tt.setupConfig(t, configDir, serverURL)
			tt.setupTarget(t, targetDir)

			// Update transfer files to point to the target directory
			updateTransferTargetPath(t, configDir, targetDir)

			client := NewClient(ClientConfig{Definitions: configDir})
			results, err := client.CheckNew(context.Background(), tt.opts)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(results) != tt.wantResultCount {
				t.Errorf("got %d results, want %d", len(results), tt.wantResultCount)
			}

			// Check specific result properties if we have results
			if len(results) > 0 {
				result := results[0]
				if result.UpdateAvailable != tt.wantUpdateAvail {
					t.Errorf("UpdateAvailable = %v, want %v", result.UpdateAvailable, tt.wantUpdateAvail)
				}
				if result.CurrentVersion != tt.wantCurrentVer {
					t.Errorf("CurrentVersion = %q, want %q", result.CurrentVersion, tt.wantCurrentVer)
				}
				if result.NewestVersion != tt.wantNewestVer {
					t.Errorf("NewestVersion = %q, want %q", result.NewestVersion, tt.wantNewestVer)
				}
			}
		})
	}
}
