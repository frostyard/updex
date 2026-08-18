package updex

import (
	"testing"

	"github.com/frostyard/clix"
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
