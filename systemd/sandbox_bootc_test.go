package systemd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSandboxRunsBootcStatus runs "bootc status --json" under every
// SandboxDirectives line, as the packaged updex-update.service does when the
// minimum image version check (ADR-0016) needs the next-boot image. The
// filter once lacked @mount, and seccomp killed the remount of /sysroot that
// bootc makes for status, so the check failed closed on every timer run
// (frostyard/updex#463). The test also checks that the remount stays inside
// the transient unit's mount namespace: the host's /sysroot mount options do
// not change.
//
// It needs root, bootc, systemd-run and a running systemd system manager, so
// it skips in CI, in containers and on hosts without bootc. On a bootc host run it with:
//
//	sudo go test -run TestSandboxRunsBootcStatus -v ./systemd/
func TestSandboxRunsBootcStatus(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: systemd-run starts a system unit")
	}
	bootc, err := exec.LookPath("bootc")
	if err != nil {
		t.Skip("bootc is not installed")
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		t.Skip("systemd-run is not installed")
	}
	// sd_booted(3): /run/systemd/system exists only when systemd is the
	// running system manager. A root container can have bootc and
	// systemd-run without one; systemd-run cannot start a unit there. After
	// this check every failure is fatal, so a seccomp kill never skips.
	if info, err := os.Stat("/run/systemd/system"); err != nil || !info.IsDir() {
		t.Skip("systemd is not the running system manager")
	}

	before, sysrootMounted := mountOptions(t, "/sysroot")

	args := []string{"--wait", "--pipe", "--quiet", "--collect"}
	for _, directive := range SandboxDirectives {
		args = append(args, "--property="+directive)
	}
	args = append(args, bootc, "status", "--json")
	cmd := exec.CommandContext(t.Context(), systemdRun, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bootc status --json under SandboxDirectives: %v\nstderr: %s", err, stderr.String())
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("bootc status --json under SandboxDirectives printed no valid JSON:\n%s", stdout.String())
	}

	if sysrootMounted {
		after, _ := mountOptions(t, "/sysroot")
		if after != before {
			t.Errorf("host /sysroot mount options changed from %q to %q: the remount left the unit's mount namespace", before, after)
		}
	}
}

// mountOptions returns the per-mount options of the last mount at target in
// /proc/self/mountinfo (the one on top), and whether one exists.
func mountOptions(t *testing.T, target string) (string, bool) {
	t.Helper()
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		t.Fatalf("read mountinfo: %v", err)
	}
	defer func() { _ = f.Close() }()
	options, found := "", false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Fields: ID, parent ID, major:minor, root, mount point, options, ...
		fields := strings.Fields(scanner.Text())
		if len(fields) > 5 && fields[4] == target {
			options, found = fields[5], true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read mountinfo: %v", err)
	}
	return options, found
}
