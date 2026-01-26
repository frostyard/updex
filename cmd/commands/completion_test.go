package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// createTestRootCmd creates a root command with subcommands for testing
func createTestRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "updex",
		Short: "Test root command",
	}

	// Add commands that should appear in completions
	rootCmd.AddCommand(NewListCmd())
	rootCmd.AddCommand(NewCheckCmd())
	rootCmd.AddCommand(NewUpdateCmd())
	rootCmd.AddCommand(NewInstallCmd())
	rootCmd.AddCommand(NewRemoveCmd())
	rootCmd.AddCommand(NewDaemonCmd())

	return rootCmd
}

// TestCompletionBash verifies bash completion script generation
func TestCompletionBash(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion bash failed: %v", err)
	}

	output := buf.String()

	// Bash completion V2 uses dynamic completion, calling the binary
	// at runtime. The script itself contains infrastructure functions.
	tests := []struct {
		name     string
		contains string
	}{
		{"bash header", "bash completion"},
		{"main function", "__updex"},
		{"completion results function", "__updex_get_completion_results"},
		{"shebang", "shell-script"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(output, tt.contains) {
				t.Errorf("bash completion missing %q", tt.contains)
			}
		})
	}

	// Verify script is non-trivial (at least 100 lines)
	lines := strings.Count(output, "\n")
	if lines < 100 {
		t.Errorf("bash completion script too short: %d lines", lines)
	}
}

// TestCompletionZsh verifies zsh completion script generation
func TestCompletionZsh(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "zsh"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion zsh failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "compdef") {
		t.Error("zsh completion missing compdef")
	}
	if !strings.Contains(output, "_updex") {
		t.Error("zsh completion missing _updex function")
	}
}

// TestCompletionFish verifies fish completion script generation
func TestCompletionFish(t *testing.T) {
	rootCmd := createTestRootCmd()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"completion", "fish"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion fish failed: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "complete") {
		t.Error("fish completion missing complete command")
	}
	if !strings.Contains(output, "updex") {
		t.Error("fish completion missing updex reference")
	}
}
