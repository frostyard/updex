package updex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/testutil"
)

func TestUpdate(t *testing.T) {
	tests := []struct {
		name           string
		setupServer    func(*testing.T) *testutil.TestServerFiles
		setupConfig    func(*testing.T, string, string) // (configDir, serverURL)
		setupTarget    func(*testing.T, string)         // targetDir
		opts           UpdateOptions
		wantDownloaded bool
		wantInstalled  bool
		wantVersion    string
		wantResultLen  int
		wantErr        bool
		wantRefresh    bool
	}{
		{
			name: "updates to newest version",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				// SHA256 of "fake extension content" = 8653bf0e654b5eef4044b95b5c491dc1b29349f46a4b572737b9d6f92aaf4c82
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
						"myext_2.0.0.raw": "8653bf0e654b5eef4044b95b5c491dc1b29349f46a4b572737b9d6f92aaf4c82",
					},
					Content: map[string][]byte{
						"myext_2.0.0.raw": []byte("fake extension content"),
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
			opts:           UpdateOptions{NoRefresh: true},
			wantDownloaded: true,
			wantInstalled:  true,
			wantVersion:    "2.0.0",
			wantResultLen:  1,
			wantErr:        false,
			wantRefresh:    false, // NoRefresh is true
		},
		{
			name: "already installed and current - skips download",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// Already have latest version
				if err := os.WriteFile(filepath.Join(targetDir, "myext_2.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:           UpdateOptions{NoRefresh: true},
			wantDownloaded: false, // Should skip download
			wantInstalled:  true,
			wantVersion:    "2.0.0",
			wantResultLen:  1,
			wantErr:        false,
			wantRefresh:    false,
		},
		{
			name: "specific version requested",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				// SHA256 of "fake extension content" = 8653bf0e654b5eef4044b95b5c491dc1b29349f46a4b572737b9d6f92aaf4c82
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
						"myext_2.0.0.raw": "8653bf0e654b5eef4044b95b5c491dc1b29349f46a4b572737b9d6f92aaf4c82",
						"myext_3.0.0.raw": "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
					},
					Content: map[string][]byte{
						"myext_2.0.0.raw": []byte("fake extension content"),
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget:    func(t *testing.T, targetDir string) {},
			opts:           UpdateOptions{Version: "2.0.0", NoRefresh: true},
			wantDownloaded: true,
			wantInstalled:  true,
			wantVersion:    "2.0.0",
			wantResultLen:  1,
			wantErr:        false,
			wantRefresh:    false,
		},
		{
			name: "version not found - returns error",
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
			setupTarget:    func(t *testing.T, targetDir string) {},
			opts:           UpdateOptions{Version: "9.9.9", NoRefresh: true},
			wantDownloaded: false,
			wantInstalled:  false,
			wantVersion:    "",
			wantResultLen:  1,
			wantErr:        true,
		},
		{
			name: "download fails - returns error",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
					},
					// No Content - download will fail with 404
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget:    func(t *testing.T, targetDir string) {},
			opts:           UpdateOptions{NoRefresh: true},
			wantDownloaded: false,
			wantInstalled:  false,
			wantVersion:    "",
			wantResultLen:  1,
			wantErr:        true,
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
			results, err := client.Update(context.Background(), tt.opts)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(results) != tt.wantResultLen {
				t.Errorf("got %d results, want %d", len(results), tt.wantResultLen)
			}

			// Check specific result properties if we have results
			if len(results) > 0 {
				result := results[0]
				if result.Error != "" {
					t.Logf("Result error: %s", result.Error)
				}
				if !tt.wantErr {
					if result.Downloaded != tt.wantDownloaded {
						t.Errorf("Downloaded = %v, want %v", result.Downloaded, tt.wantDownloaded)
					}
					if result.Installed != tt.wantInstalled {
						t.Errorf("Installed = %v, want %v", result.Installed, tt.wantInstalled)
					}
					if result.Version != tt.wantVersion {
						t.Errorf("Version = %q, want %q", result.Version, tt.wantVersion)
					}
				}
			}

			// Check if Refresh was called
			if mockRunner.RefreshCalled != tt.wantRefresh {
				t.Errorf("RefreshCalled = %v, want %v", mockRunner.RefreshCalled, tt.wantRefresh)
			}
		})
	}
}
