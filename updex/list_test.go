package updex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/testutil"
)

func TestList(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(*testing.T) *testutil.TestServerFiles
		setupConfig func(*testing.T, string, string) // (configDir, serverURL)
		setupTarget func(*testing.T, string)         // targetDir
		opts        ListOptions
		wantLen     int
		wantErr     bool
	}{
		{
			name: "list versions with remote and installed",
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
				// Create installed version file
				if err := os.WriteFile(filepath.Join(targetDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:    ListOptions{},
			wantLen: 2, // 2 versions: 1.0.0 (installed), 2.0.0 (available)
			wantErr: false,
		},
		{
			name: "list with no transfer configs",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				// No config files created
			},
			setupTarget: func(t *testing.T, targetDir string) {},
			opts:        ListOptions{},
			wantLen:     0,
			wantErr:     true,
		},
		{
			name: "list filters by component",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return &testutil.TestServerFiles{
					Files: map[string]string{
						"myext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
						"other_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
					},
				}
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
				createTransferFile(t, configDir, "other", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {},
			opts:        ListOptions{Component: "myext"},
			wantLen:     1, // Only myext_1.0.0
			wantErr:     false,
		},
		{
			name: "list with HTTP error returns graceful result",
			setupServer: func(t *testing.T) *testutil.TestServerFiles {
				return nil // Will use error server
			},
			setupConfig: func(t *testing.T, configDir, serverURL string) {
				createTransferFile(t, configDir, "myext", serverURL)
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// Create installed version so we get at least one result
				if err := os.WriteFile(filepath.Join(targetDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:    ListOptions{},
			wantLen: 1, // Only installed version (HTTP failed for available)
			wantErr: false,
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
			results, err := client.List(context.Background(), tt.opts)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(results) != tt.wantLen {
				t.Errorf("got %d results, want %d", len(results), tt.wantLen)
			}
		})
	}
}

// createTransferFile creates a test .transfer file in the config directory
func createTransferFile(t *testing.T, configDir, component, baseURL string) {
	t.Helper()
	content := `[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
MatchPattern=` + component + `_@v.raw
`
	path := filepath.Join(configDir, component+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}
}

// updateTransferTargetPath updates all transfer files to use the given target path
func updateTransferTargetPath(t *testing.T, configDir, targetDir string) {
	t.Helper()
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".transfer" {
			continue
		}
		path := filepath.Join(configDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read transfer file: %v", err)
		}
		// Append Path directive to Target section
		newContent := string(content) + "Path=" + targetDir + "\n"
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			t.Fatalf("failed to update transfer file: %v", err)
		}
	}
}
