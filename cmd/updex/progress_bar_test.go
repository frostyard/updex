package updex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
)

// TestNewProgressBarSuppressedForMachineOutput verifies that the download
// progress bar is not created when stdout must carry only machine-readable
// data. In JSON or silent mode a rendered bar (and its completion newline)
// would corrupt the stdout stream that consumers parse, so newProgressBar must
// return nil — which download.Download treats as "no progress". In plain text
// mode the bar is still created (and, per its options, written to stderr).
func TestNewProgressBarSuppressedForMachineOutput(t *testing.T) {
	oldJSON, oldSilent := clix.JSONOutput, clix.Silent
	t.Cleanup(func() {
		clix.JSONOutput, clix.Silent = oldJSON, oldSilent
	})

	cases := []struct {
		name       string
		jsonOutput bool
		silent     bool
		wantNil    bool
	}{
		{name: "text mode renders a bar", jsonOutput: false, silent: false, wantNil: false},
		{name: "json mode suppresses the bar", jsonOutput: true, silent: false, wantNil: true},
		{name: "silent mode suppresses the bar", jsonOutput: false, silent: true, wantNil: true},
		{name: "json and silent suppress the bar", jsonOutput: true, silent: true, wantNil: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clix.JSONOutput = tc.jsonOutput
			clix.Silent = tc.silent

			w := newProgressBar(22)
			if tc.wantNil && w != nil {
				t.Fatalf("newProgressBar returned a non-nil writer in JSON/silent mode; the bar would corrupt stdout")
			}
			if !tc.wantNil && w == nil {
				t.Fatalf("newProgressBar returned nil in text mode; expected a progress bar")
			}
		})
	}
}

// setReporterFlags swaps the clix output flags and the terminal seam for one
// test.
func setReporterFlags(t *testing.T, jsonOutput, silent, stderrTerminal bool) {
	t.Helper()
	oldJSON, oldSilent, oldTerminal := clix.JSONOutput, clix.Silent, stderrIsTerminal
	t.Cleanup(func() {
		clix.JSONOutput, clix.Silent, stderrIsTerminal = oldJSON, oldSilent, oldTerminal
	})
	clix.JSONOutput = jsonOutput
	clix.Silent = silent
	stderrIsTerminal = func() bool { return stderrTerminal }
}

// TestDownloadProgressEnabled verifies that the download bar is installed only
// for a text run whose stderr is a terminal. Under systemd (or any redirect)
// the bar's carriage-return redraws would reach the journal as raw text.
func TestDownloadProgressEnabled(t *testing.T) {
	cases := []struct {
		name           string
		jsonOutput     bool
		silent         bool
		stderrTerminal bool
		want           bool
	}{
		{name: "text mode on a terminal", stderrTerminal: true, want: true},
		{name: "text mode without a terminal", stderrTerminal: false, want: false},
		{name: "json mode on a terminal", jsonOutput: true, stderrTerminal: true, want: false},
		{name: "silent mode on a terminal", silent: true, stderrTerminal: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setReporterFlags(t, tc.jsonOutput, tc.silent, tc.stderrTerminal)
			if got := downloadProgressEnabled(); got != tc.want {
				t.Errorf("downloadProgressEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNewReporterGoesToStderr verifies the reporter each output mode gets:
// nothing in silent mode, JSON Lines on stderr in JSON mode, and clix's text
// reporter otherwise. A JSON reporter on stdout would put progress events in
// front of the command's result.
func TestNewReporterGoesToStderr(t *testing.T) {
	t.Run("silent discards events", func(t *testing.T) {
		setReporterFlags(t, false, true, true)
		if _, ok := newReporter().(reporter.NoopReporter); !ok {
			t.Fatalf("newReporter() = %T, want reporter.NoopReporter", newReporter())
		}
	})

	t.Run("json writes JSON lines to stderr", func(t *testing.T) {
		setReporterFlags(t, true, false, true)
		var errBuf bytes.Buffer
		oldStderr := clix.Stderr
		clix.Stderr = &errBuf
		t.Cleanup(func() { clix.Stderr = oldStderr })

		r := newReporter()
		if _, ok := r.(*reporter.JSONReporter); !ok {
			t.Fatalf("newReporter() = %T, want *reporter.JSONReporter", r)
		}
		if !r.IsJSON() {
			t.Error("newReporter() in JSON mode does not report IsJSON")
		}
		r.Message("progress %d", 1)
		if !strings.Contains(errBuf.String(), `"type":"message"`) || !strings.Contains(errBuf.String(), "progress 1") {
			t.Errorf("stderr does not carry the JSON progress event: %q", errBuf.String())
		}
	})

	t.Run("text uses the clix text reporter", func(t *testing.T) {
		setReporterFlags(t, false, false, true)
		if _, ok := newReporter().(*reporter.TextReporter); !ok {
			t.Fatalf("newReporter() = %T, want *reporter.TextReporter", newReporter())
		}
	})
}
