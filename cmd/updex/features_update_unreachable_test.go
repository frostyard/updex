package updex

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
)

// configureUpdateRun points the update command at configDir and gives it root,
// a mock sysext runner, no refresh and no vacuum, as the timer runs it. The
// SDK's progress lines are silenced so the test log stays readable; the skip
// line goes to the command's own stderr writer, which runUpdateCapturing
// captures.
func configureUpdateRun(t *testing.T, configDir string, jsonOutput bool) {
	t.Helper()
	oldDefinitions, oldNoRefresh, oldNoVac := definitions, noRefresh, featureUpdateNoVac
	oldJSONOutput, oldSilent := clix.JSONOutput, clix.Silent
	oldGetEUID, oldRunner, oldStderr := getEUID, sysextRunner, clix.Stderr
	t.Cleanup(func() {
		definitions, noRefresh, featureUpdateNoVac = oldDefinitions, oldNoRefresh, oldNoVac
		clix.JSONOutput, clix.Silent = oldJSONOutput, oldSilent
		getEUID, sysextRunner, clix.Stderr = oldGetEUID, oldRunner, oldStderr
	})
	definitions = configDir
	noRefresh = true
	featureUpdateNoVac = true
	clix.JSONOutput = jsonOutput
	clix.Silent = true
	getEUID = func() int { return 0 }
	sysextRunner = &sysext.MockRunner{}
	clix.Stderr = &strings.Builder{}
}

// closedLoopbackURL returns an http URL for a loopback port nothing listens
// on, so a request to it fails with connection refused.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on a loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close the loopback listener: %v", err)
	}
	return "http://" + addr
}

// Under systemd, a run that no source could reach prints the normal table,
// keeps the run's usual warnings, adds exactly one skip line and exits 0
// (ADR-0018). The reporter is not silenced here: the test reads both stderr
// routes, the reporter's (clix.Stderr) and the command's own.
func TestRunFeaturesUpdate_AllSourcesUnreachableUnderSystemdSkips(t *testing.T) {
	configDir, targetDir := t.TempDir(), t.TempDir()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", closedLoopbackURL(t))
	configureUpdateRun(t, configDir, false)
	clix.Silent = false
	reporterOut := &strings.Builder{}
	clix.Stderr = reporterOut
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")

	stdout, stderr, err := runUpdateCapturing(t)
	if err != nil {
		t.Fatalf("runFeaturesUpdate under systemd with every source unreachable: %v, want nil", err)
	}
	if !strings.Contains(stdout, "testext") {
		t.Errorf("stdout = %q, want the normal update table", stdout)
	}
	if strings.Count(stderr, "\n") != 1 {
		t.Errorf("command stderr = %q, want exactly one line", stderr)
	}
	for _, want := range []string{"skipping this run", "no update source is reachable"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("command stderr = %q, want it to contain %q", stderr, want)
		}
	}
	combined := reporterOut.String() + stderr
	if got := strings.Count(combined, "skipping this run"); got != 1 {
		t.Errorf("all stderr = %q, want the skip line exactly once, got %d", combined, got)
	}
	// The run's own warnings are not hidden: the journal still shows why the
	// source failed.
	if !strings.Contains(reporterOut.String(), "connection refused") {
		t.Errorf("reporter stderr = %q, want the run's warning with the connection failure", reporterOut.String())
	}
}

// Under systemd, one reachable and one unreachable source is a partial
// failure: the run still exits non-zero and logs no skip line.
func TestRunFeaturesUpdate_OneReachableSourceUnderSystemdFails(t *testing.T) {
	configDir, targetDir := t.TempDir(), t.TempDir()
	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)
	writeFeatureTransferFile(t, configDir, targetDir, "othertex", "testfeature", closedLoopbackURL(t))
	configureUpdateRun(t, configDir, false)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")

	_, stderr, err := runUpdateCapturing(t)
	if err == nil {
		t.Fatal("runFeaturesUpdate = nil, want the unreachable source to fail the run")
	}
	if strings.Contains(stderr, "skipping this run") {
		t.Errorf("stderr = %q: a partial failure must not log a skip", stderr)
	}
}

// Under systemd, a verification failure of a reachable source still fails the
// run: the source answered, so the host is not offline.
func TestRunFeaturesUpdate_VerificationFailureUnderSystemdFails(t *testing.T) {
	configDir, targetDir := t.TempDir(), t.TempDir()
	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex([]byte("other content"))},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)
	configureUpdateRun(t, configDir, false)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")

	_, stderr, err := runUpdateCapturing(t)
	if err == nil {
		t.Fatal("runFeaturesUpdate = nil, want the hash mismatch to fail the run")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("error = %v, want the hash mismatch", err)
	}
	if strings.Contains(stderr, "skipping this run") {
		t.Errorf("stderr = %q: a verification failure must not log a skip", stderr)
	}
}

// In an interactive shell the same condition is an error, so the exit status
// tells the operator that the host is offline.
func TestRunFeaturesUpdate_AllSourcesUnreachableInteractiveIsAnError(t *testing.T) {
	configDir, targetDir := t.TempDir(), t.TempDir()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", closedLoopbackURL(t))
	configureUpdateRun(t, configDir, false)
	t.Setenv("INVOCATION_ID", "")

	stdout, stderr, err := runUpdateCapturing(t)
	if err == nil {
		t.Fatal("runFeaturesUpdate interactive = nil, want the unreachable sources to fail the run")
	}
	if !strings.Contains(err.Error(), "no update source is reachable") {
		t.Errorf("error = %v, want it to name the unreachable sources", err)
	}
	if !strings.Contains(stdout, "testext") {
		t.Errorf("stdout = %q, want the normal update table", stdout)
	}
	if strings.Contains(stderr, "skipping this run") {
		t.Errorf("stderr = %q: an interactive run must not log a skip", stderr)
	}
}

// Under systemd with --json, stdout stays exactly one JSON array and the run
// exits 0.
func TestRunFeaturesUpdate_AllSourcesUnreachableUnderSystemdJSONEmitsArray(t *testing.T) {
	configDir, targetDir := t.TempDir(), t.TempDir()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", closedLoopbackURL(t))
	configureUpdateRun(t, configDir, true)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")

	stdout, _, err := runUpdateCapturing(t)
	if err != nil {
		t.Fatalf("runFeaturesUpdate --json under systemd with every source unreachable: %v, want nil", err)
	}
	var results []updex.UpdateFeaturesResult
	if jsonErr := json.Unmarshal([]byte(stdout), &results); jsonErr != nil {
		t.Fatalf("stdout is not exactly one JSON array: %v\n%s", jsonErr, stdout)
	}
	if len(results) != 1 || len(results[0].Results) != 1 || results[0].Results[0].Error == "" {
		t.Errorf("results = %+v, want the failed transfer reported", results)
	}
}
