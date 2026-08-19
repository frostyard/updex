package updex

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestManPageHookProducesRoff pins the release man-page hook: GoReleaser's
// before-hook scripts/manpages.sh runs `go run ./cmd/updex-cli man | gzip`
// at tag time, and the `man` subcommand is hidden and injected by
// charmbracelet/fang through github.com/frostyard/clix App.Run — a
// dependency bump that drops or renames it would otherwise pass every
// pull-request check and fail for the first time inside GoReleaser. This
// reproduces the hook's command and asserts on the roff it prints, so the
// Unit Tests job fails on the pull request instead. Skipped under -short so
// the race job stays fast; the Unit Tests job does not pass -short.
func TestManPageHookProducesRoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the go run man-page hook check in -short mode")
	}

	cmd := exec.Command("go", "run", "./cmd/updex-cli", "man")
	cmd.Dir = ".." // the module root, where scripts/manpages.sh runs it
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\nstderr:\n%s", strings.Join(cmd.Args, " "), err, stderr.String())
	}

	page := stdout.String()
	if !strings.HasPrefix(page, ".TH UPDEX 1") {
		t.Errorf("man page does not begin with the roff header %q; got %q", ".TH UPDEX 1", firstLine(page))
	}
	if !strings.Contains(page, ".SH NAME") {
		t.Errorf("man page has no %q section", ".SH NAME")
	}
	if len(page) < 1000 {
		t.Errorf("man page is %d bytes, want at least 1000 (empty or truncated output)", len(page))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
