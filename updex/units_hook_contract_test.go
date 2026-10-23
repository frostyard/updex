package updex

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/systemd"
	"go.yaml.in/yaml/v3"
)

// packagedUnitDestinations are the paths the frostyard-updex package installs
// the auto-update units to (ADR-0016 "Units").
var packagedUnitDestinations = map[string]string{
	"updex-update.timer":   "/usr/lib/systemd/system/updex-update.timer",
	"updex-update.service": "/usr/lib/systemd/system/updex-update.service",
}

// TestUnitsHookMatchesGenerator pins the release unit hook: GoReleaser's
// before-hook scripts/units.sh runs `go run ./cmd/updex-cli units
// --output-dir units`, and nfpms.contents installs the two files from there.
// This runs that script into a temporary directory and compares the files byte
// for byte with systemd.GenerateTimer/GenerateService(DaemonUnits()), the text
// `updex daemon enable` writes (ADR-0016: a contract test fails when the
// packaged units and the generator differ). It also pins the lines that the
// ADR makes the contract, on the generated files themselves, so a change to
// the units has to change this test too. Skipped under -short like the
// man-page hook test, so the race job stays fast.
func TestUnitsHookMatchesGenerator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the go run units hook check in -short mode")
	}

	out := filepath.Join(t.TempDir(), "units")
	cmd := exec.Command("sh", "scripts/units.sh", out)
	cmd.Dir = ".." // the module root, where GoReleaser runs the hook
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\nstderr:\n%s", strings.Join(cmd.Args, " "), err, stderr.String())
	}

	timer, service := DaemonUnits()
	want := map[string]string{
		"updex-update.timer":   systemd.GenerateTimer(timer),
		"updex-update.service": systemd.GenerateService(service),
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("read generated units: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if wantNames := []string{"updex-update.service", "updex-update.timer"}; !slices.Equal(names, wantNames) {
		t.Fatalf("generated files = %v, want exactly %v", names, wantNames)
	}

	got := map[string]string{}
	for name, expected := range want {
		path := filepath.Join(out, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got[name] = string(data)
		if string(data) != expected {
			t.Errorf("%s differs from the generator output\n--- generated file ---\n%s--- generator ---\n%s", name, data, expected)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644", name, info.Mode().Perm())
		}
		if strings.HasPrefix(string(data), "#") {
			t.Errorf("%s starts with a comment; the ownership marker belongs only to units that `daemon enable` writes to /etc", name)
		}
	}

	wantLines := map[string][]string{
		"updex-update.timer": {
			"OnCalendar=*-*-* 00/6:00:00",
			"RandomizedDelaySec=3600s",
			"Persistent=true",
			"WantedBy=timers.target",
		},
		"updex-update.service": append([]string{
			"Wants=network-online.target",
			"After=network-online.target",
			"ConditionACPower=true",
			"Type=oneshot",
			"ExecStart=/usr/bin/updex features update --no-refresh",
		}, systemd.SandboxDirectives...),
	}
	for name, lines := range wantLines {
		have := strings.Split(got[name], "\n")
		for _, line := range lines {
			if !slices.Contains(have, line) {
				t.Errorf("%s has no line %q:\n%s", name, line, got[name])
			}
		}
	}
	if strings.Contains(got["updex-update.service"], "[Install]") {
		t.Error("the service has an [Install] section; only the timer is enabled")
	}
}

// goreleaserConfig is the part of .goreleaser.yaml the units contract reads.
type goreleaserConfig struct {
	Before struct {
		Hooks []string `yaml:"hooks"`
	} `yaml:"before"`
	Nfpms []struct {
		Contents []struct {
			Src      string `yaml:"src"`
			Dst      string `yaml:"dst"`
			FileInfo struct {
				Mode string `yaml:"mode"`
			} `yaml:"file_info"`
		} `yaml:"contents"`
	} `yaml:"nfpms"`
}

// TestGoReleaserShipsUnitsDisabled pins that .goreleaser.yaml generates the
// units (after the man pages, with the other before-hooks) and installs both
// under /usr/lib/systemd/system with mode 0644, and that the package neither
// ships maintainer scripts nor enables anything: the snosi preset or
// `updex daemon enable` does that (ADR-0016 "Units").
func TestGoReleaserShipsUnitsDisabled(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	var config goreleaserConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}

	manpages := slices.Index(config.Before.Hooks, "./scripts/manpages.sh")
	units := slices.Index(config.Before.Hooks, "./scripts/units.sh")
	if units < 0 {
		t.Fatalf("before.hooks = %v, want it to run ./scripts/units.sh", config.Before.Hooks)
	}
	if manpages < 0 || units < manpages {
		t.Errorf("before.hooks = %v, want ./scripts/units.sh after ./scripts/manpages.sh", config.Before.Hooks)
	}

	if len(config.Nfpms) != 1 {
		t.Fatalf("nfpms has %d entries, want 1", len(config.Nfpms))
	}
	for name, dst := range packagedUnitDestinations {
		var found bool
		for _, content := range config.Nfpms[0].Contents {
			if content.Dst != dst {
				continue
			}
			found = true
			if want := "./units/" + name; content.Src != want {
				t.Errorf("nfpms content for %s has src %q, want %q", dst, content.Src, want)
			}
			if content.FileInfo.Mode != "0644" {
				t.Errorf("nfpms content for %s has mode %q, want 0644", dst, content.FileInfo.Mode)
			}
		}
		if !found {
			t.Errorf("nfpms.contents has no entry with dst %s", dst)
		}
	}

	// No maintainer scripts anywhere in the nfpms entry: a postinstall that
	// ran systemctl would enable the timer on every host.
	var generic struct {
		Nfpms []map[string]any `yaml:"nfpms"`
	}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}
	for key, value := range generic.Nfpms[0] {
		if key == "scripts" {
			t.Errorf("nfpms has a scripts section %v: the package must not enable the units", value)
		}
		if nested, ok := value.(map[string]any); ok {
			if _, has := nested["scripts"]; has {
				t.Errorf("nfpms.%s has a scripts section: the package must not enable the units", key)
			}
		}
	}

	gitignore, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(string(gitignore), "\n"), "units/") {
		t.Error(".gitignore must ignore the generated units/ directory, like completions/ and manpages/")
	}
}

// TestUnitsWorkflowChecks pins the CI checks on the units:
//   - the Tests workflow's units job generates them with the hook script,
//     installs the built binary at /usr/bin/updex (the service's ExecStart),
//     and runs systemd-analyze verify on both files;
//   - the snapshot workflow checks the amd64 .deb after GoReleaser ran.
func TestUnitsWorkflowChecks(t *testing.T) {
	tests := readGoReleaserWorkflow(t, "../.github/workflows/test.yml")
	job, ok := tests.Jobs["units"]
	if !ok {
		t.Fatal("Tests workflow is missing the units job")
	}
	var generate, install, verify = -1, -1, -1
	for i, step := range job.Steps {
		switch {
		case strings.Contains(step.Run, "./scripts/units.sh"):
			generate = i
		case strings.Contains(step.Run, "sudo install") && strings.Contains(step.Run, "/usr/bin/updex"):
			install = i
		case strings.Contains(step.Run, "systemd-analyze verify") &&
			strings.Contains(step.Run, "units/updex-update.service") &&
			strings.Contains(step.Run, "units/updex-update.timer"):
			verify = i
			if !strings.Contains(step.Run, "grep -q 'updex-update'") {
				t.Error("the verify step must fail on warnings that name an updex unit: systemd-analyze exits 0 for an unknown directive")
			}
		}
	}
	if generate < 0 || install < 0 || verify < 0 || generate >= verify || install >= verify {
		t.Errorf("units job steps: generate=%d install=%d verify=%d, want all present and both before verify", generate, install, verify)
	}

	snapshot := readGoReleaserWorkflow(t, "../.github/workflows/snapshot.yml")
	steps := snapshot.Jobs["snapshot"].Steps
	release, check := -1, -1
	for i, step := range steps {
		if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
			release = i
		}
		if strings.Contains(step.Run, "scripts/check-deb-units.sh") && strings.Contains(step.Run, "dist/*_amd64.deb") {
			check = i
		}
	}
	if release < 0 || check < 0 || check < release {
		t.Errorf("snapshot steps: goreleaser=%d deb check=%d, want the .deb check after GoReleaser", release, check)
	}
}

// TestCheckDebUnitsScript runs scripts/check-deb-units.sh against a fake
// dpkg-deb, so the check is tested without a real package.
func TestCheckDebUnitsScript(t *testing.T) {
	const dir = "drwxr-xr-x root/root         0 2026-10-23 10:56 ./usr/lib/systemd/system/\n"
	timer := "-rw-r--r-- root/root       156 2026-10-23 10:56 ./usr/lib/systemd/system/updex-update.timer\n"
	service := "-rw-r--r-- root/root       637 2026-10-23 10:56 ./usr/lib/systemd/system/updex-update.service\n"

	tests := []struct {
		name    string
		listing string
		wantErr string // empty: the script must succeed
	}{
		{name: "both units", listing: dir + timer + service},
		{name: "missing service", listing: dir + timer, wantErr: "updex-update.service"},
		{name: "missing timer", listing: dir + service, wantErr: "updex-update.timer"},
		{name: "wrong directory", listing: strings.ReplaceAll(timer+service, "/usr/lib/systemd/system/", "/etc/systemd/system/"), wantErr: "updex-update.timer"},
		{name: "wrong mode", listing: strings.Replace(timer, "-rw-r--r--", "-rwxr-xr-x", 1) + service, wantErr: "updex-update.timer"},
		{name: "a directory is not a file", listing: strings.Replace(timer, "-rw-r--r--", "drwxr-xr-x", 1) + service, wantErr: "updex-update.timer"},
		{name: "similar name", listing: strings.ReplaceAll(timer+service, "updex-update", "updex-update-x"), wantErr: "updex-update.timer"},
	}
	script, err := filepath.Abs("../scripts/check-deb-units.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bin := t.TempDir()
			listing := filepath.Join(bin, "listing")
			if err := os.WriteFile(listing, []byte(test.listing), 0o644); err != nil {
				t.Fatal(err)
			}
			fake := "#!/bin/sh\n[ \"$1\" = -c ] || exit 64\ncat \"" + listing + "\"\n"
			if err := os.WriteFile(filepath.Join(bin, "dpkg-deb"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			pkg := filepath.Join(bin, "updex.deb")
			if err := os.WriteFile(pkg, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("sh", script, pkg)
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("script failed: %v\n%s", err, output)
				}
				return
			}
			if err == nil {
				t.Fatalf("script succeeded, want a failure naming %s\n%s", test.wantErr, output)
			}
			if !strings.Contains(string(output), test.wantErr) {
				t.Errorf("script output does not name %s:\n%s", test.wantErr, output)
			}
		})
	}

	t.Run("no package argument", func(t *testing.T) {
		if err := exec.Command("sh", script).Run(); err == nil {
			t.Error("script succeeded without a package argument")
		}
	})
}
