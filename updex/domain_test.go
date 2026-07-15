package updex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/sysext"
)

// withComponentSearchRoots points config.SearchRoots at four fresh temp
// directories (mimicking /etc, /run, /usr/local/lib, /usr/lib) for the
// duration of the test, restoring the original value on cleanup. Returns
// the roots in priority order.
func withComponentSearchRoots(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	roots := []string{
		filepath.Join(root, "etc"),
		filepath.Join(root, "run"),
		filepath.Join(root, "usr", "local", "lib"),
		filepath.Join(root, "usr", "lib"),
	}
	for _, d := range roots {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed to create root %s: %v", d, err)
		}
	}

	original := config.SearchRoots
	config.SearchRoots = roots
	t.Cleanup(func() { config.SearchRoots = original })

	return roots
}

func writeComponentFeature(t *testing.T, dir, name string, enabled bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	content := "[Feature]\nEnabled=" + enabledStr + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".feature"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write feature file: %v", err)
	}
}

func writeComponentTransfer(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	content := `[Transfer]
Features=` + name + `

[Source]
Type=url-file
Path=https://example.com/` + name + `
MatchPattern=` + name + `_@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=` + name + `_@v.raw
CurrentSymlink=` + name + `.raw
`
	if err := os.WriteFile(filepath.Join(dir, name+".transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write transfer file: %v", err)
	}
}

func writePartitionTransfer(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	content := `[Transfer]
Verify=yes

[Source]
Type=url-file
Path=https://example.com/os
MatchPattern=cayo_@v.root.raw.xz

[Target]
Type=partition
Path=auto
MatchPattern=cayo_@v_r
`
	if err := os.WriteFile(filepath.Join(dir, name+".transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write transfer file: %v", err)
	}
}

func TestLoadDomain_DefaultIsUnionOfComponents(t *testing.T) {
	roots := withComponentSearchRoots(t)
	usrLib := roots[3]

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.d"), "shared", true)
	writeComponentTransfer(t, filepath.Join(usrLib, "sysupdate.d"), "shared")

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)
	writeComponentTransfer(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker")

	client := NewClient(ClientConfig{})
	features, transfers, err := client.loadDomain("")
	if err != nil {
		t.Fatalf(`loadDomain("") error = %v`, err)
	}
	if len(features) != 2 {
		t.Fatalf("expected 2 features from the union, got %d: %+v", len(features), features)
	}
	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfers from the union, got %d: %+v", len(transfers), transfers)
	}
}

func TestLoadDomain_ComponentScoping(t *testing.T) {
	roots := withComponentSearchRoots(t)
	usrLib := roots[3]

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.d"), "shared", true)
	writeComponentTransfer(t, filepath.Join(usrLib, "sysupdate.d"), "shared")

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)
	writeComponentTransfer(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker")

	client := NewClient(ClientConfig{})
	features, transfers, err := client.loadDomain("docker")
	if err != nil {
		t.Fatalf(`loadDomain("docker") error = %v`, err)
	}
	if len(features) != 1 || features[0].Name != "docker" {
		t.Fatalf("expected only the docker feature, got %+v", features)
	}
	if len(transfers) != 1 || transfers[0].Component != "docker" {
		t.Fatalf("expected only the docker transfer, got %+v", transfers)
	}
}

func TestLoadDomain_ComponentConflictsWithDefinitions(t *testing.T) {
	client := NewClient(ClientConfig{Definitions: t.TempDir()})
	_, _, err := client.loadDomain("docker")
	if err == nil {
		t.Fatal("expected an error combining --definitions with --component")
	}
}

func TestLoadDomain_SkipsNonSysextTransfersInUnion(t *testing.T) {
	roots := withComponentSearchRoots(t)
	usrLib := roots[3]

	defaultDir := filepath.Join(usrLib, "sysupdate.d")
	writePartitionTransfer(t, defaultDir, "10-root-verity")

	client := NewClient(ClientConfig{})
	_, transfers, err := client.loadDomain("")
	if err != nil {
		t.Fatalf(`loadDomain("") error = %v`, err)
	}
	if len(transfers) != 0 {
		t.Fatalf("expected the partition transfer to be skipped, got %+v", transfers)
	}
}

func TestClient_Components(t *testing.T) {
	roots := withComponentSearchRoots(t)
	usrLib := roots[3]

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)
	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus", false)
	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus-extra", false)

	client := NewClient(ClientConfig{})
	components, err := client.Components(t.Context())
	if err != nil {
		t.Fatalf("Components() error = %v", err)
	}
	if len(components) != 2 {
		t.Fatalf("expected 2 components, got %d: %+v", len(components), components)
	}
	if components[0].Name != "docker" || components[0].FeatureCount != 1 {
		t.Errorf("docker component = %+v", components[0])
	}
	if components[1].Name != "incus" || components[1].FeatureCount != 2 {
		t.Errorf("incus component = %+v", components[1])
	}
	if components[0].SourceDir != filepath.Join(usrLib, "sysupdate.docker.d") {
		t.Errorf("docker.SourceDir = %q", components[0].SourceDir)
	}
}

func TestClient_Components_NoneDiscovered(t *testing.T) {
	withComponentSearchRoots(t)

	client := NewClient(ClientConfig{})
	components, err := client.Components(t.Context())
	if err != nil {
		t.Fatalf("Components() error = %v", err)
	}
	if len(components) != 0 {
		t.Errorf("expected no components, got %+v", components)
	}
}

// TestWriteFeatureDropIn_ComponentScoped verifies the drop-in path a feature
// resolves to depends on which systemd-sysupdate component it was
// discovered under (see config.ComponentOfPath): component-scoped features
// write under /etc/sysupdate.<name>.d/, everything else (legacy default
// directory or a --definitions override) keeps the legacy
// /etc/sysupdate.d/ path. dryRun=true is used throughout so this never
// touches the real filesystem.
func TestWriteFeatureDropIn_ComponentScoped(t *testing.T) {
	client := NewClient(ClientConfig{})

	tests := []struct {
		name        string
		featureName string
		featureFile string
		wantDropIn  string
	}{
		{
			name:        "component-scoped feature",
			featureName: "docker",
			featureFile: "/usr/lib/sysupdate.docker.d/docker.feature",
			wantDropIn:  filepath.Join("/etc", "sysupdate.docker.d", "docker.feature.d", "00-updex.conf"),
		},
		{
			name:        "component-scoped feature overridden in /etc",
			featureName: "docker",
			featureFile: "/etc/sysupdate.docker.d/docker.feature",
			wantDropIn:  filepath.Join("/etc", "sysupdate.docker.d", "docker.feature.d", "00-updex.conf"),
		},
		{
			name:        "legacy default directory feature",
			featureName: "shared",
			featureFile: "/usr/lib/sysupdate.d/shared.feature",
			wantDropIn:  filepath.Join("/etc", "sysupdate.d", "shared.feature.d", "00-updex.conf"),
		},
		{
			name:        "--definitions override directory",
			featureName: "custom",
			featureFile: "/tmp/my-custom-dir/custom.feature",
			wantDropIn:  filepath.Join("/etc", "sysupdate.d", "custom.feature.d", "00-updex.conf"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &config.Feature{
				Name:     tc.featureName,
				FilePath: tc.featureFile,
			}
			got, err := client.writeFeatureDropIn(f, true, true /* dryRun */)
			if err != nil {
				t.Fatalf("writeFeatureDropIn() error = %v", err)
			}
			if got != tc.wantDropIn {
				t.Errorf("writeFeatureDropIn() = %q, want %q", got, tc.wantDropIn)
			}
		})
	}
}

// TestEnableFeature_ComponentScoping verifies --component scoping end to
// end: enabling a feature that only exists in the "docker" component
// succeeds when scoped to that component, and fails with "not found" when
// scoped to a different (or no matching) component.
func TestEnableFeature_ComponentScoping(t *testing.T) {
	roots := withComponentSearchRoots(t)
	usrLib := roots[3]

	writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", false)
	writeComponentTransfer(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	t.Run("scoped to the right component", func(t *testing.T) {
		result, err := client.EnableFeature(t.Context(), "docker", EnableFeatureOptions{
			DryRun:    true,
			Component: "docker",
		})
		if err != nil {
			t.Fatalf("EnableFeature failed: %v", err)
		}
		if !result.Success {
			t.Errorf("expected Success=true, got false (Error: %s)", result.Error)
		}
	})

	t.Run("scoped to an unrelated component", func(t *testing.T) {
		writeComponentFeature(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus", false)

		_, err := client.EnableFeature(t.Context(), "docker", EnableFeatureOptions{
			DryRun:    true,
			Component: "incus",
		})
		if err == nil {
			t.Fatal("expected an error enabling a feature outside the scoped component")
		}
	})
}
