package updex

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// The shell-completion contract. cobra's generated bash, zsh, and fish
// scripts are dynamic: none of them embeds the command tree — each shell
// function calls the hidden `updex __complete <words…>` endpoint at
// completion time and presents what it returns. So "the completions name
// every top-level command" is two assertions per shell: the script for that
// shell generates (exit 0, its shell marker present, delegating to
// `__complete`), and `__complete ""` — what every script asks — lists every
// top-level command updex registers. Run from the module root exactly as an
// operator would (`go run ./cmd/updex-cli`). Skipped under -short so the race
// job stays fast; the Unit Tests job and make ci run it.
func TestShellCompletionsNameEveryCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the go run completion contract check in -short mode")
	}

	// The top-level commands updex registers (cmd/updex/root.go NewRootCmd);
	// package updex cannot import cmd/updex (import cycle), so this is the
	// literal set, plus the two cobra adds. Guarded against a vacuous pass
	// below.
	expected := []string{"catalog", "components", "daemon", "features", "completion", "help"}
	if len(expected) == 0 {
		t.Fatal("expected command list is empty; the test would pass vacuously")
	}

	// What every generated script calls: the dynamic completion endpoint,
	// asked for the top-level completions. One line per command:
	// "<name>\t<description>", then a ":<directive>" line.
	complete := run(t, "__complete", "")
	var listed []string
	for _, line := range strings.Split(complete, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		listed = append(listed, strings.SplitN(line, "\t", 2)[0])
	}
	if len(listed) == 0 {
		t.Fatalf("`updex __complete \"\"` listed no commands; output:\n%s", complete)
	}
	missing := func() []string {
		var missing []string
		for _, name := range expected {
			found := false
			for _, got := range listed {
				if got == name {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, name)
			}
		}
		return missing
	}()

	shells := []struct {
		shell    string
		marker   string // proves the right shell's script was generated
		delegate string // proves it asks __complete for its results
	}{
		{"bash", "__updex_get_completion_results", "__complete"},
		{"zsh", "#compdef updex", "__complete"},
		{"fish", "complete -c updex", "__complete"},
	}
	for _, tc := range shells {
		t.Run(tc.shell, func(t *testing.T) {
			script := run(t, "completion", tc.shell)
			if strings.TrimSpace(script) == "" {
				t.Fatalf("completion %s produced no output", tc.shell)
			}
			if !strings.Contains(script, tc.marker) {
				t.Errorf("completion %s output lacks the %s marker %q", tc.shell, tc.shell, tc.marker)
			}
			if !strings.Contains(script, tc.delegate) {
				t.Errorf("completion %s script does not delegate to %q; it cannot name any command", tc.shell, tc.delegate)
			}
			if len(missing) > 0 {
				t.Errorf("completion %s (via `updex __complete \"\"`) does not name %v; listed: %v", tc.shell, missing, listed)
			}
		})
	}
}

// run executes `go run ./cmd/updex-cli <args…>` from the module root and
// returns stdout, failing the test on a non-zero exit.
func run(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "./cmd/updex-cli"}, args...)...)
	cmd.Dir = ".." // the module root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\nstderr:\n%s", strings.Join(cmd.Args, " "), err, stderr.String())
	}
	return stdout.String()
}
