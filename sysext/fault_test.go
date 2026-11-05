package sysext

import (
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/internal/fault"
)

// TestParseTargetPatternsMarksConfig checks that a target pattern that cannot
// be used is a configuration failure, with the error text of today.
func TestParseTargetPatternsMarksConfig(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantText string
	}{
		{name: "none", wantText: "no target match patterns defined"},
		{name: "no placeholder", patterns: []string{"ext.raw"}, wantText: "invalid target pattern: pattern must contain @v placeholder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &config.Transfer{}
			tr.Target.MatchPatterns = tt.patterns
			_, err := parseTargetPatterns(tr)
			if err == nil || err.Error() != tt.wantText {
				t.Fatalf("parseTargetPatterns() error = %v, want %q", err, tt.wantText)
			}
			if class, _ := fault.ClassOf(err); class != fault.Config {
				t.Errorf("class = %q, want config", class)
			}
		})
	}
}
