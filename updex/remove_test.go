package updex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
)

func TestRemove(t *testing.T) {
	tests := []struct {
		name            string
		component       string
		setupConfig     func(*testing.T, string) // configDir
		setupTarget     func(*testing.T, string) // targetDir
		opts            RemoveOptions
		wantSuccess     bool
		wantUnmerged    bool
		wantFilesCount  int
		wantErr         bool
		wantErrContains string
	}{
		{
			name:      "removes all versions of a component",
			component: "myext",
			setupConfig: func(t *testing.T, configDir string) {
				createTransferFile(t, configDir, "myext", "http://example.com")
			},
			setupTarget: func(t *testing.T, targetDir string) {
				// Create some installed versions
				for _, v := range []string{"1.0.0", "2.0.0", "2.1.0"} {
					if err := os.WriteFile(filepath.Join(targetDir, "myext_"+v+".raw"), []byte("test"), 0644); err != nil {
						t.Fatalf("failed to create test file: %v", err)
					}
				}
				// Note: symlink won't be removed because CurrentSymlink isn't set in transfer
			},
			opts:           RemoveOptions{NoRefresh: true},
			wantSuccess:    true,
			wantUnmerged:   false,
			wantFilesCount: 3, // 3 versions only (CurrentSymlink not configured)
			wantErr:        false,
		},
		{
			name:      "removes with --now calls unmerge",
			component: "myext",
			setupConfig: func(t *testing.T, configDir string) {
				createTransferFile(t, configDir, "myext", "http://example.com")
			},
			setupTarget: func(t *testing.T, targetDir string) {
				if err := os.WriteFile(filepath.Join(targetDir, "myext_1.0.0.raw"), []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
			},
			opts:           RemoveOptions{Now: true, NoRefresh: true},
			wantSuccess:    true,
			wantUnmerged:   true, // --now should call unmerge
			wantFilesCount: 1,    // 1 version only (CurrentSymlink not configured)
			wantErr:        false,
		},
		{
			name:      "empty component name - returns error",
			component: "",
			setupConfig: func(t *testing.T, configDir string) {
				createTransferFile(t, configDir, "myext", "http://example.com")
			},
			setupTarget:     func(t *testing.T, targetDir string) {},
			opts:            RemoveOptions{NoRefresh: true},
			wantSuccess:     false,
			wantErr:         true,
			wantErrContains: "component name is required",
		},
		{
			name:      "component not found - returns error",
			component: "nonexistent",
			setupConfig: func(t *testing.T, configDir string) {
				createTransferFile(t, configDir, "myext", "http://example.com")
			},
			setupTarget:     func(t *testing.T, targetDir string) {},
			opts:            RemoveOptions{NoRefresh: true},
			wantSuccess:     false,
			wantErr:         true,
			wantErrContains: "nonexistent",
		},
		{
			name:      "no files to remove - still succeeds",
			component: "myext",
			setupConfig: func(t *testing.T, configDir string) {
				createTransferFile(t, configDir, "myext", "http://example.com")
			},
			setupTarget:    func(t *testing.T, targetDir string) {},
			opts:           RemoveOptions{NoRefresh: true},
			wantSuccess:    true,
			wantFilesCount: 0,
			wantErr:        false,
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

			// Set up config and target
			tt.setupConfig(t, configDir)
			tt.setupTarget(t, targetDir)

			// Update transfer files to point to the target directory
			updateTransferTargetPath(t, configDir, targetDir)

			client := NewClient(ClientConfig{Definitions: configDir})
			result, err := client.Remove(context.Background(), tt.component, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrContains != "" && !containsString(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error to contain %q, got %q", tt.wantErrContains, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if result != nil {
				if result.Success != tt.wantSuccess {
					t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
				}
				if result.Unmerged != tt.wantUnmerged {
					t.Errorf("Unmerged = %v, want %v", result.Unmerged, tt.wantUnmerged)
				}
				if len(result.RemovedFiles) != tt.wantFilesCount {
					t.Errorf("RemovedFiles count = %d, want %d (files: %v)", len(result.RemovedFiles), tt.wantFilesCount, result.RemovedFiles)
				}
			}

			// Check if Unmerge was called when --now is specified
			if tt.opts.Now && tt.wantSuccess {
				if !mockRunner.UnmergeCalled {
					t.Error("expected UnmergeCalled = true when Now option is set")
				}
			}
		})
	}
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
