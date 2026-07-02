package updex

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)

// resetQuietState resets the global quiet-related flags after a test.
func resetQuietState(t *testing.T) {
	t.Helper()
	prevQuiet := quiet
	prevSilent := clix.Silent
	prevDefs := definitions
	t.Cleanup(func() {
		quiet = prevQuiet
		clix.Silent = prevSilent
		definitions = prevDefs
	})
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	return buf.String()
}

func TestIsQuiet(t *testing.T) {
	resetQuietState(t)

	tests := []struct {
		name   string
		quiet  bool
		silent bool
		want   bool
	}{
		{"neither", false, false, false},
		{"quiet only", true, false, true},
		{"silent only", false, true, true},
		{"both", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quiet = tt.quiet
			clix.Silent = tt.silent
			if got := isQuiet(); got != tt.want {
				t.Errorf("isQuiet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectReporter(t *testing.T) {
	resetQuietState(t)

	quiet = true
	clix.Silent = false
	if _, ok := selectReporter().(reporter.NoopReporter); !ok {
		t.Errorf("selectReporter() with quiet = %T, want NoopReporter", selectReporter())
	}

	quiet = false
	clix.Silent = true
	if _, ok := selectReporter().(reporter.NoopReporter); !ok {
		t.Errorf("selectReporter() with silent = %T, want NoopReporter", selectReporter())
	}

	quiet = false
	clix.Silent = false
	clix.JSONOutput = false
	r := selectReporter()
	if r.IsJSON() {
		t.Errorf("selectReporter() default should not be JSON reporter")
	}
	if _, ok := r.(reporter.NoopReporter); ok {
		t.Errorf("selectReporter() default should not be NoopReporter")
	}
}

func TestSelectDownloadProgress(t *testing.T) {
	resetQuietState(t)

	quiet = true
	if selectDownloadProgress() != nil {
		t.Error("selectDownloadProgress() with quiet should be nil")
	}

	quiet = false
	clix.Silent = false
	if selectDownloadProgress() == nil {
		t.Error("selectDownloadProgress() without quiet should be non-nil")
	}
}

func TestFeaturesListQuietSuppressesOutput(t *testing.T) {
	resetQuietState(t)
	definitions = t.TempDir() // empty dir -> no features configured

	// Non-quiet: prints the informational line.
	quiet = false
	clix.Silent = false
	out := captureStdout(t, func() {
		if err := runFeaturesList(&cobra.Command{}, nil); err != nil {
			t.Fatalf("runFeaturesList: %v", err)
		}
	})
	if !strings.Contains(out, "No features configured.") {
		t.Errorf("expected informational output, got %q", out)
	}

	// Quiet: suppresses all stdout.
	quiet = true
	out = captureStdout(t, func() {
		if err := runFeaturesList(&cobra.Command{}, nil); err != nil {
			t.Fatalf("runFeaturesList: %v", err)
		}
	})
	if out != "" {
		t.Errorf("expected no output in quiet mode, got %q", out)
	}
}

func TestFeaturesCheckQuietSuppressesOutput(t *testing.T) {
	resetQuietState(t)
	definitions = t.TempDir()

	quiet = true
	clix.Silent = false
	out := captureStdout(t, func() {
		_ = runFeaturesCheck(&cobra.Command{}, nil)
	})
	if out != "" {
		t.Errorf("expected no output in quiet mode, got %q", out)
	}
}
