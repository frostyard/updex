package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withTestSearchRoots points SearchRoots at four fresh temp directories
// (mimicking /etc, /run, /usr/local/lib, /usr/lib) for the duration of the
// test, restoring the original value on cleanup. Returns the roots in
// priority order.
func withTestSearchRoots(t *testing.T) []string {
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

	original := SearchRoots
	SearchRoots = roots
	t.Cleanup(func() { SearchRoots = original })

	return roots
}

// writeFeatureFixture writes a minimal .feature file under dir.
func writeFeatureFixture(t *testing.T, dir, name string, enabled bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	content := "[Feature]\nEnabled=" + map[bool]string{true: "true", false: "false"}[enabled] + "\n"
	path := filepath.Join(dir, name+".feature")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// writeSysextTransferFixture writes a minimal sysext-shaped .transfer file
// (url-file source, regular-file target) under dir, tagged with sourceTag so
// tests can tell which copy of a colliding name won a merge.
func writeSysextTransferFixture(t *testing.T, dir, name, sourceTag string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	content := `[Transfer]
Features=` + name + `

[Source]
Type=url-file
Path=https://example.com/` + sourceTag + `/` + name + `
MatchPattern=` + name + `_@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=` + name + `_@v.raw
CurrentSymlink=` + name + `.raw
`
	path := filepath.Join(dir, name+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// writePartitionTransferFixture writes an OS A/B partition transfer, the
// shape native (non-sysext) images ship in the legacy default directory.
func writePartitionTransferFixture(t *testing.T, dir, name string) {
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
	path := filepath.Join(dir, name+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// writeUKITransferFixture writes the UKI regular-file transfer under
// /EFI/Linux (PathRelativeTo=boot), the other non-sysext shape found in the
// legacy default directory on native images.
func writeUKITransferFixture(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	content := `[Transfer]
Verify=yes

[Source]
Type=url-file
Path=https://example.com/os
MatchPattern=cayo_@v.efi

[Target]
Type=regular-file
Path=/EFI/Linux
PathRelativeTo=boot
MatchPattern=cayo_@v.efi
`
	path := filepath.Join(dir, name+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestComponentSearchPaths(t *testing.T) {
	roots := withTestSearchRoots(t)

	t.Run("default component", func(t *testing.T) {
		got := ComponentSearchPaths("")
		want := []string{
			filepath.Join(roots[0], "sysupdate.d"),
			filepath.Join(roots[1], "sysupdate.d"),
			filepath.Join(roots[2], "sysupdate.d"),
			filepath.Join(roots[3], "sysupdate.d"),
		}
		if !slices.Equal(got, want) {
			t.Errorf("ComponentSearchPaths(\"\") = %v, want %v", got, want)
		}
	})

	t.Run("named component", func(t *testing.T) {
		got := ComponentSearchPaths("docker")
		want := []string{
			filepath.Join(roots[0], "sysupdate.docker.d"),
			filepath.Join(roots[1], "sysupdate.docker.d"),
			filepath.Join(roots[2], "sysupdate.docker.d"),
			filepath.Join(roots[3], "sysupdate.docker.d"),
		}
		if !slices.Equal(got, want) {
			t.Errorf("ComponentSearchPaths(\"docker\") = %v, want %v", got, want)
		}
	})
}

func TestEtcComponentDir(t *testing.T) {
	if got, want := EtcComponentDir(""), filepath.Join("/etc", "sysupdate.d"); got != want {
		t.Errorf("EtcComponentDir(\"\") = %q, want %q", got, want)
	}
	if got, want := EtcComponentDir("docker"), filepath.Join("/etc", "sysupdate.docker.d"); got != want {
		t.Errorf("EtcComponentDir(\"docker\") = %q, want %q", got, want)
	}
}

func TestParseComponentDirName(t *testing.T) {
	tests := []struct {
		dirName  string
		wantName string
		wantOK   bool
	}{
		{"sysupdate.docker.d", "docker", true},
		{"sysupdate.claude-desktop.d", "claude-desktop", true},
		{"sysupdate.1password_cli.d", "1password_cli", true},
		{"sysupdate.d", "", false},         // legacy default, not a named component
		{"sysupdate..d", "", false},        // empty name
		{"sysupdate.foo.bar.d", "", false}, // dotted name, invalid charset
		{"sysupdate.foo!bar.d", "", false}, // invalid charset
		{"notsysupdate.foo.d", "", false},  // wrong prefix
		{"sysupdate.foo.dir", "", false},   // wrong suffix
		{"sysupdate.foo", "", false},       // missing .d suffix
		{"other-directory", "", false},     // unrelated directory
		{"sysupdate.FOO_bar-1.d", "FOO_bar-1", true},
	}

	for _, tc := range tests {
		t.Run(tc.dirName, func(t *testing.T) {
			name, ok := parseComponentDirName(tc.dirName)
			if ok != tc.wantOK || name != tc.wantName {
				t.Errorf("parseComponentDirName(%q) = (%q, %v), want (%q, %v)",
					tc.dirName, name, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestComponentOfPath(t *testing.T) {
	tests := []struct {
		path     string
		wantName string
		wantOK   bool
	}{
		{"/usr/lib/sysupdate.docker.d/docker.feature", "docker", true},
		{"/etc/sysupdate.docker.d/docker.feature.d/00-updex.conf", "", false}, // parent is the drop-in dir, not the component dir
		{"/usr/lib/sysupdate.d/docker.feature", "", false},                    // legacy default
		{"/tmp/custom-dir/docker.feature", "", false},                         // --definitions override dir
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			name, ok := ComponentOfPath(tc.path)
			if ok != tc.wantOK || name != tc.wantName {
				t.Errorf("ComponentOfPath(%q) = (%q, %v), want (%q, %v)",
					tc.path, name, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestDiscoverComponents(t *testing.T) {
	roots := withTestSearchRoots(t)
	etc, _, usrLocalLib, usrLib := roots[0], roots[1], roots[2], roots[3]

	// docker: only in /usr/lib (lowest precedence root actually present)
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)
	// incus: present in both /etc (override) and /usr/lib (base)
	writeFeatureFixture(t, filepath.Join(etc, "sysupdate.incus.d"), "incus", false)
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus", true)
	// nix: in /usr/local/lib only
	writeFeatureFixture(t, filepath.Join(usrLocalLib, "sysupdate.nix.d"), "nix", true)

	// Invalid entries that must be ignored.
	if err := os.MkdirAll(filepath.Join(usrLib, "sysupdate.foo.bar.d"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(usrLib, "sysupdate.d"), 0755); err != nil {
		t.Fatal(err) // legacy default, not a named component
	}
	if err := os.WriteFile(filepath.Join(usrLib, "sysupdate.notadir.d"), []byte("x"), 0644); err != nil {
		t.Fatal(err) // a file, not a directory
	}

	components, err := DiscoverComponents()
	if err != nil {
		t.Fatalf("DiscoverComponents() error = %v", err)
	}

	if len(components) != 3 {
		names := make([]string, len(components))
		for i, c := range components {
			names[i] = c.Name
		}
		t.Fatalf("expected 3 components, got %d: %v", len(components), names)
	}

	// Sorted by name.
	wantNames := []string{"docker", "incus", "nix"}
	for i, want := range wantNames {
		if components[i].Name != want {
			t.Errorf("components[%d].Name = %q, want %q", i, components[i].Name, want)
		}
	}

	// docker: only usr/lib exists.
	docker := components[0]
	if len(docker.SearchPaths) != 1 || docker.SearchPaths[0] != filepath.Join(usrLib, "sysupdate.docker.d") {
		t.Errorf("docker.SearchPaths = %v, want [%s]", docker.SearchPaths, filepath.Join(usrLib, "sysupdate.docker.d"))
	}

	// incus: etc (highest priority) then usr/lib, in that order.
	incus := components[1]
	wantIncus := []string{filepath.Join(etc, "sysupdate.incus.d"), filepath.Join(usrLib, "sysupdate.incus.d")}
	if !slices.Equal(incus.SearchPaths, wantIncus) {
		t.Errorf("incus.SearchPaths = %v, want %v", incus.SearchPaths, wantIncus)
	}
}

func TestDiscoverComponents_NoRootsExist(t *testing.T) {
	root := t.TempDir()
	original := SearchRoots
	SearchRoots = []string{filepath.Join(root, "does-not-exist")}
	t.Cleanup(func() { SearchRoots = original })

	components, err := DiscoverComponents()
	if err != nil {
		t.Fatalf("DiscoverComponents() error = %v", err)
	}
	if len(components) != 0 {
		t.Errorf("expected no components, got %v", components)
	}
}

func TestIsSysextTransfer(t *testing.T) {
	tests := []struct {
		name string
		t    *Transfer
		want bool
	}{
		{
			name: "regular sysext transfer",
			t:    &Transfer{Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "regular-file"}},
			want: true,
		},
		{
			name: "implicit regular-file target (Type unset)",
			t:    &Transfer{Source: SourceSection{Type: "url-file"}, Target: TargetSection{}},
			want: true,
		},
		{
			name: "partition target",
			t:    &Transfer{Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "partition"}},
			want: false,
		},
		{
			name: "regular-file target relative to boot (UKI)",
			t:    &Transfer{Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "regular-file", PathRelativeTo: "boot"}},
			want: false,
		},
		{
			name: "non-url-file source",
			t:    &Transfer{Source: SourceSection{Type: "url-tar"}, Target: TargetSection{Type: "regular-file"}},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSysextTransfer(tc.t); got != tc.want {
				t.Errorf("IsSysextTransfer() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterSysextTransfers(t *testing.T) {
	transfers := []*Transfer{
		{Component: "docker", Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "regular-file"}},
		{Component: "10-root-verity", Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "partition"}},
		{Component: "90-uki", Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "regular-file", PathRelativeTo: "boot"}},
		{Component: "incus", Source: SourceSection{Type: "url-file"}, Target: TargetSection{Type: "regular-file"}},
	}

	filtered := FilterSysextTransfers(transfers)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 sysext transfers, got %d", len(filtered))
	}
	if filtered[0].Component != "docker" || filtered[1].Component != "incus" {
		t.Errorf("unexpected filtered transfers: %v, %v", filtered[0].Component, filtered[1].Component)
	}
}

func TestLoadAllFeatures_UnionAcrossComponents(t *testing.T) {
	roots := withTestSearchRoots(t)
	etc, _, _, usrLib := roots[0], roots[1], roots[2], roots[3]

	// Legacy default directory: "shared" feature not shadowed by any component.
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.d"), "shared", true)

	// A component with its own feature.
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus", false)

	features, warnings, err := LoadAllFeatures("")
	if err != nil {
		t.Fatalf("LoadAllFeatures() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no collision warnings, got %v", warnings)
	}
	if len(features) != 2 {
		names := make([]string, len(features))
		for i, f := range features {
			names[i] = f.Name
		}
		t.Fatalf("expected 2 features, got %d: %v", len(features), names)
	}
	if features[0].Name != "incus" || features[1].Name != "shared" {
		t.Errorf("unexpected feature names: %q, %q", features[0].Name, features[1].Name)
	}

	// /etc override on the component wins over the component's own /usr/lib file.
	writeFeatureFixture(t, filepath.Join(etc, "sysupdate.incus.d"), "incus", true)
	features, _, err = LoadAllFeatures("")
	if err != nil {
		t.Fatalf("LoadAllFeatures() error = %v", err)
	}
	idx := slices.IndexFunc(features, func(f *Feature) bool { return f.Name == "incus" })
	if idx < 0 {
		t.Fatalf("incus feature missing")
	}
	if !features[idx].Enabled {
		t.Errorf("expected /etc override (Enabled=true) to win, got Enabled=%v", features[idx].Enabled)
	}
}

func TestLoadAllFeatures_ComponentWinsCollisionWithDefault(t *testing.T) {
	roots := withTestSearchRoots(t)
	_, _, _, usrLib := roots[0], roots[1], roots[2], roots[3]

	// Same feature name defined both in the legacy default dir and in a
	// same-named component; the component must win, with a warning logged.
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.d"), "docker", false)
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)

	features, warnings, err := LoadAllFeatures("")
	if err != nil {
		t.Fatalf("LoadAllFeatures() error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("expected 1 feature after collision resolution, got %d", len(features))
	}
	if !features[0].Enabled {
		t.Errorf("expected the component's feature (Enabled=true) to win over the default directory's, got Enabled=%v", features[0].Enabled)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 collision warning, got %d: %v", len(warnings), warnings)
	}
	if !containsAll(warnings[0], "docker", "default directory", `component "docker"`) {
		t.Errorf("warning %q missing expected details", warnings[0])
	}
}

func TestLoadAllFeatures_CustomPathBypassesDiscovery(t *testing.T) {
	roots := withTestSearchRoots(t)
	_, _, _, usrLib := roots[0], roots[1], roots[2], roots[3]

	// A component exists in the standard hierarchy...
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", true)

	// ...but an explicit --definitions directory must be used verbatim, with
	// no discovery and no warnings.
	customDir := t.TempDir()
	writeFeatureFixture(t, customDir, "custom", true)

	features, warnings, err := LoadAllFeatures(customDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures() error = %v", err)
	}
	if warnings != nil {
		t.Errorf("expected no warnings with customPath, got %v", warnings)
	}
	if len(features) != 1 || features[0].Name != "custom" {
		t.Fatalf("expected only the custom-dir feature, got %+v", features)
	}
}

func TestLoadAllTransfers_UnionSkipsNonSysextAndReportsCollisions(t *testing.T) {
	roots := withTestSearchRoots(t)
	_, _, _, usrLib := roots[0], roots[1], roots[2], roots[3]

	defaultDir := filepath.Join(usrLib, "sysupdate.d")
	// Native-image legacy directory: one sysext transfer plus the two
	// non-sysext OS transfer shapes that must be silently skipped.
	writeSysextTransferFixture(t, defaultDir, "docker", "default")
	writePartitionTransferFixture(t, defaultDir, "10-root-verity")
	writeUKITransferFixture(t, defaultDir, "90-uki")

	// A migrated component ships its own copy of docker (collides with the
	// legacy default and must win) plus a brand-new incus transfer.
	writeSysextTransferFixture(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", "component")
	writeSysextTransferFixture(t, filepath.Join(usrLib, "sysupdate.incus.d"), "incus", "component")

	transfers, warnings, err := LoadAllTransfers("")
	if err != nil {
		t.Fatalf("LoadAllTransfers() error = %v", err)
	}

	if len(transfers) != 2 {
		names := make([]string, len(transfers))
		for i, tr := range transfers {
			names[i] = tr.Component
		}
		t.Fatalf("expected 2 sysext transfers (partition/UKI skipped), got %d: %v", len(transfers), names)
	}
	if transfers[0].Component != "docker" || transfers[1].Component != "incus" {
		t.Errorf("unexpected transfer set: %q, %q", transfers[0].Component, transfers[1].Component)
	}

	// The component's copy of docker must have won.
	if got, want := transfers[0].Source.Path, "https://example.com/component/docker"; got != want {
		t.Errorf("docker Source.Path = %q, want %q (component copy should win)", got, want)
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 collision warning, got %d: %v", len(warnings), warnings)
	}
	if !containsAll(warnings[0], "docker", "default directory", `component "docker"`) {
		t.Errorf("warning %q missing expected details", warnings[0])
	}
}

// TestLoadComponentFeatures_DropInOverrideScoped proves that a component's
// own highest-priority-root drop-in (the temp-root stand-in for /etc, what
// updex.Client.writeFeatureDropIn writes to via config.EtcComponentDir)
// overrides its lower-priority-root base feature file, and that a drop-in
// placed under a DIFFERENT component's directory has no effect — scoping is
// respected on read, not just on write.
func TestLoadComponentFeatures_DropInOverrideScoped(t *testing.T) {
	roots := withTestSearchRoots(t)
	etcStandIn, _, _, usrLib := roots[0], roots[1], roots[2], roots[3]

	// Base feature file for the "docker" component, disabled by default.
	writeFeatureFixture(t, filepath.Join(usrLib, "sysupdate.docker.d"), "docker", false)

	// A drop-in override under a DIFFERENT (legacy default) scope must be
	// ignored when loading the "docker" component.
	misplacedDropInDir := filepath.Join(usrLib, "sysupdate.d", "docker.feature.d")
	if err := os.MkdirAll(misplacedDropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(misplacedDropInDir, "00-updex.conf"), []byte("[Feature]\nEnabled=true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	features, err := LoadComponentFeatures("docker")
	if err != nil {
		t.Fatalf("LoadComponentFeatures(\"docker\") error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	if features[0].Enabled {
		t.Fatalf("expected the misplaced default-scope drop-in to be ignored, got Enabled=true")
	}

	// Now write the drop-in at the CORRECT component-scoped path under the
	// highest-priority root (the /etc stand-in).
	correctDropInDir := filepath.Join(etcStandIn, "sysupdate.docker.d", "docker.feature.d")
	if err := os.MkdirAll(correctDropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(correctDropInDir, "00-updex.conf"), []byte("[Feature]\nEnabled=true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	features, err = LoadComponentFeatures("docker")
	if err != nil {
		t.Fatalf("LoadComponentFeatures(\"docker\") error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}
	if !features[0].Enabled {
		t.Errorf("expected the component-scoped drop-in to win, got Enabled=false")
	}
	if features[0].FilePath != filepath.Join(usrLib, "sysupdate.docker.d", "docker.feature") {
		t.Errorf("FilePath = %q, unexpected source", features[0].FilePath)
	}
}

func TestLoadAllTransfers_CustomPathFiltersNonSysext(t *testing.T) {
	customDir := t.TempDir()
	writeSysextTransferFixture(t, customDir, "docker", "custom")
	writePartitionTransferFixture(t, customDir, "10-root-verity")

	transfers, warnings, err := LoadAllTransfers(customDir)
	if err != nil {
		t.Fatalf("LoadAllTransfers() error = %v", err)
	}
	if warnings != nil {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(transfers) != 1 || transfers[0].Component != "docker" {
		t.Fatalf("expected only the sysext transfer, got %+v", transfers)
	}
}

// containsAll reports whether s contains every substring in subs.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
