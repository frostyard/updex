package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestMakefileCIRunsTheSecurityScan pins the *Security Scan* job of
// .github/workflows/test.yml into `make ci`. Core ADR-0038 (carried forward
// from ADR-0022, binding per docs/org-adrs.md) requires `make ci` to be the
// one credential-free target that mirrors CI's credential-free jobs in CI's
// fail-fast order. The Security Scan job is credential-free and a required
// status check, so `make ci` must run govulncheck between the lint stage
// (which ends verify-static) and the unit-test stage.
func TestMakefileCIRunsTheSecurityScan(t *testing.T) {
	makefile := readMakefile(t)

	ciRecipe := extractRecipe(t, makefile, "ci:")
	if !strings.Contains(ciRecipe, "==> security scan (govulncheck)") {
		t.Fatalf("Makefile ci recipe must announce the security scan with an `==> security scan (govulncheck)` banner; got:\n%s", ciRecipe)
	}
	if !strings.Contains(ciRecipe, "$(MAKE) security") {
		t.Fatalf("Makefile ci recipe must invoke the security target with `$(MAKE) security`; got:\n%s", ciRecipe)
	}

	banner := strings.Index(ciRecipe, "==> security scan (govulncheck)")
	unitTests := strings.Index(ciRecipe, "==> unit tests with coverage")
	if unitTests < 0 {
		t.Fatalf("Makefile ci recipe no longer has an `==> unit tests with coverage` stage; got:\n%s", ciRecipe)
	}
	if banner > unitTests {
		t.Fatalf("Makefile ci recipe must run the security scan before the unit tests, mirroring test.yml's Lint → Security Scan → Unit Tests order; got:\n%s", ciRecipe)
	}

	// The lint stage lives in verify-static, which ci runs as a prerequisite,
	// so the scan lands after lint as long as it is ci's first own stage.
	if !strings.Contains(ciRecipe, "ci: verify-static") {
		t.Fatalf("Makefile ci target must keep verify-static (which ends in the lint stage) as its prerequisite; got:\n%s", ciRecipe)
	}
	verifyStatic := extractRecipe(t, makefile, "verify-static:")
	if !strings.Contains(verifyStatic, "==> lint") {
		t.Fatalf("Makefile verify-static recipe must keep its `==> lint` stage; got:\n%s", verifyStatic)
	}
	if strings.Contains(verifyStatic, "govulncheck") {
		t.Fatalf("the security scan belongs to ci, not verify-static: `make verify` is the read-only reviewer gate and core ADR-0043 scopes it to tidy/vet/gofmt/lint/tests; got:\n%s", verifyStatic)
	}

	// ADR-0043: no tool is optional. A missing binary must fail loudly with
	// the install command, never degrade the gate to a no-op.
	securityRecipe := extractRecipe(t, makefile, "security:")
	if !strings.Contains(securityRecipe, "govulncheck ./...") {
		t.Fatalf("Makefile security recipe must run `govulncheck ./...`; got:\n%s", securityRecipe)
	}
	if !strings.Contains(securityRecipe, "command -v govulncheck") {
		t.Fatalf("Makefile security recipe must gate on `command -v govulncheck` so a missing binary is distinguished from a real finding; got:\n%s", securityRecipe)
	}
	if !strings.Contains(securityRecipe, "install with: mise install") {
		t.Fatalf("Makefile security recipe must print the install command when govulncheck is absent; got:\n%s", securityRecipe)
	}
	if !strings.Contains(securityRecipe, "exit 1") {
		t.Fatalf("Makefile security recipe must exit non-zero when govulncheck is absent; got:\n%s", securityRecipe)
	}
	if strings.Contains(strings.ToLower(securityRecipe), "skip") {
		t.Fatalf("Makefile security recipe must never skip: core ADR-0043 says no tool is optional; got:\n%s", securityRecipe)
	}
	if strings.Contains(securityRecipe, "govulncheck ./... || ") {
		t.Fatalf("Makefile security recipe must not `|| <fallback>` the govulncheck run, which would swallow real findings; got:\n%s", securityRecipe)
	}
}

// TestGovulncheckPinMatchesWorkflow keeps the mise.toml pin and the
// `go install …@v<version>` step in .github/workflows/test.yml byte-identical.
// The workflow installs govulncheck itself (that path is a protected boundary
// this repository's gates do not edit), so this test — not a shared config —
// is what stops the two pins from drifting apart.
func TestGovulncheckPinMatchesWorkflow(t *testing.T) {
	miseData, err := os.ReadFile("../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	misePin := regexp.MustCompile(`(?m)^"go:golang\.org/x/vuln/cmd/govulncheck" = "([^"]+)"`).FindStringSubmatch(string(miseData))
	if misePin == nil {
		t.Fatalf(`mise.toml [tools] must pin govulncheck as "go:golang.org/x/vuln/cmd/govulncheck" = "<version>" (core ADR-0043: every executable a gate invokes is pinned there)`)
	}

	workflowData, err := os.ReadFile("../.github/workflows/test.yml")
	if err != nil {
		t.Fatalf("read .github/workflows/test.yml: %v", err)
	}
	workflowPin := regexp.MustCompile(`go install golang\.org/x/vuln/cmd/govulncheck@(\S+)`).FindStringSubmatch(string(workflowData))
	if workflowPin == nil {
		t.Fatal("test.yml no longer installs govulncheck with `go install golang.org/x/vuln/cmd/govulncheck@<version>`")
	}

	if want := "v" + misePin[1]; workflowPin[1] != want {
		t.Fatalf("govulncheck pins drifted: mise.toml pins %q (so test.yml must install %q), but test.yml installs %q — bump both in the same commit", misePin[1], want, workflowPin[1])
	}
}

// readMakefile returns the repository Makefile as a string.
func readMakefile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	return string(data)
}
