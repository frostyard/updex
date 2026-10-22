package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigRoots points ConfigRoots at temp directories for the test.
func withConfigRoots(t *testing.T, roots ...string) {
	t.Helper()
	orig := ConfigRoots
	ConfigRoots = roots
	t.Cleanup(func() { ConfigRoots = orig })
}

func writeCatalogFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".catalog"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const fedoraCatalog = `[Catalog]
SiteURL=https://extensions.example.com/fedora/
ListURL=https://api.example.com/repos/fedora/contents/
`

func TestLoadReposNoCatalogs(t *testing.T) {
	withConfigRoots(t, t.TempDir())

	_, err := LoadRepos()
	if !errors.Is(err, ErrNoCatalogs) {
		t.Fatalf("expected ErrNoCatalogs, got %v", err)
	}
}

func TestLoadReposDefaults(t *testing.T) {
	root := t.TempDir()
	withConfigRoots(t, root)
	writeCatalogFile(t, root, "fedora", fedoraCatalog)

	repos, err := LoadRepos()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	r := repos[0]
	if r.Name != "fedora" {
		t.Errorf("Name = %q, want fedora", r.Name)
	}
	if r.SiteURL != "https://extensions.example.com/fedora" {
		t.Errorf("SiteURL = %q, want trailing slash trimmed", r.SiteURL)
	}
	if r.ListURL != "https://api.example.com/repos/fedora/contents/" {
		t.Errorf("ListURL = %q", r.ListURL)
	}
	if r.Component != "catalog-fedora" {
		t.Errorf("Component = %q, want catalog-fedora", r.Component)
	}
}

func TestLoadReposComponentOverride(t *testing.T) {
	root := t.TempDir()
	withConfigRoots(t, root)
	writeCatalogFile(t, root, "community", `[Catalog]
SiteURL=https://extensions.example.com/community
Component=fedora-sysexts-community
`)

	repos, err := LoadRepos()
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Component != "fedora-sysexts-community" {
		t.Errorf("Component = %q, want fedora-sysexts-community", repos[0].Component)
	}
	if repos[0].ListURL != "" {
		t.Errorf("ListURL = %q, want empty", repos[0].ListURL)
	}
}

func TestLoadReposPrecedence(t *testing.T) {
	etc, lib := t.TempDir(), t.TempDir()
	withConfigRoots(t, etc, lib)
	writeCatalogFile(t, etc, "fedora", `[Catalog]
SiteURL=https://etc.example.com
`)
	writeCatalogFile(t, lib, "fedora", `[Catalog]
SiteURL=https://lib.example.com
`)
	writeCatalogFile(t, lib, "community", `[Catalog]
SiteURL=https://lib.example.com/community
`)

	repos, err := LoadRepos()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	// Sorted by name: community, fedora.
	if repos[0].Name != "community" || repos[1].Name != "fedora" {
		t.Fatalf("unexpected order: %s, %s", repos[0].Name, repos[1].Name)
	}
	if repos[1].SiteURL != "https://etc.example.com" {
		t.Errorf("earlier root should win, got SiteURL %q", repos[1].SiteURL)
	}
}

func TestLoadReposValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"missing-siteurl", "[Catalog]\nListURL=https://api.example.com\n"},
		{"missing-section", "SiteURL=https://example.com\n"},
		{"bad-component", "[Catalog]\nSiteURL=https://example.com\nComponent=has.dots\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			withConfigRoots(t, root)
			writeCatalogFile(t, root, "repo", tt.content)

			if _, err := LoadRepos(); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseRepoHTTPSPolicy(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantInsecure bool
		wantErr      string
	}{
		{
			name:    "https accepted",
			content: "[Catalog]\nSiteURL=https://extensions.example.com\n",
		},
		{
			name:    "http rejected",
			content: "[Catalog]\nSiteURL=http://extensions.example.com\n",
			wantErr: "AllowInsecure",
		},
		{
			name:         "http explicitly allowed",
			content:      "[Catalog]\nSiteURL=http://extensions.example.com\nAllowInsecure=yes\n",
			wantInsecure: true,
		},
		{
			name:    "insecure list rejected",
			content: "[Catalog]\nSiteURL=https://extensions.example.com\nListURL=http://api.example.com\n",
			wantErr: "AllowInsecure",
		},
		{
			name:         "insecure list explicitly allowed",
			content:      "[Catalog]\nSiteURL=https://extensions.example.com\nListURL=http://api.example.com\nAllowInsecure=yes\n",
			wantInsecure: true,
		},
		{
			name:    "relative site rejected",
			content: "[Catalog]\nSiteURL=extensions.example.com\n",
			wantErr: "absolute URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "test.catalog")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			repo, err := parseRepoFile("test", path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("parseRepoFile() error = nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("parseRepoFile() error = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRepoFile() error = %v", err)
			}
			if repo.AllowInsecure != tt.wantInsecure {
				t.Errorf("AllowInsecure = %v, want %v", repo.AllowInsecure, tt.wantInsecure)
			}
		})
	}
}

func TestParseRepoAllowUnsigned(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
		wantErr string
	}{
		{name: "default denies", content: "[Catalog]\nSiteURL=https://extensions.example.com\n"},
		{name: "yes allows", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=yes\n", want: true},
		{name: "no denies", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=no\n"},
		{name: "invalid rejected", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=maybe\n", wantErr: "invalid AllowUnsigned value"},
		{name: "does not permit http", content: "[Catalog]\nSiteURL=http://extensions.example.com\nAllowUnsigned=yes\n", wantErr: "AllowInsecure"},
		{name: "true allows", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=true\n", want: true},
		{name: "1 allows", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=1\n", want: true},
		{name: "on allows", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=on\n", want: true},
		{name: "false denies", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=false\n"},
		{name: "empty rejected", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=\n", wantErr: "invalid AllowUnsigned value"},
		{name: "last duplicate wins (no)", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=yes\nAllowUnsigned=no\n"},
		{name: "last duplicate wins (yes)", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowUnsigned=no\nAllowUnsigned=yes\n", want: true},
		{name: "outside [Catalog] ignored", content: "[Catalog]\nSiteURL=https://extensions.example.com\n\n[Transfer]\nAllowUnsigned=yes\n"},
		{name: "refused with AllowInsecure", content: "[Catalog]\nSiteURL=https://extensions.example.com\nAllowInsecure=yes\nAllowUnsigned=yes\n", wantErr: "cannot be combined with AllowInsecure=yes"},
		{name: "AllowInsecure without AllowUnsigned accepted", content: "[Catalog]\nSiteURL=http://extensions.example.com\nAllowInsecure=yes\nAllowUnsigned=no\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.catalog")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			repo, err := parseRepoFile("test", path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseRepoFile() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRepoFile() error = %v", err)
			}
			if repo.AllowUnsigned != tt.want {
				t.Errorf("AllowUnsigned = %v, want %v", repo.AllowUnsigned, tt.want)
			}
		})
	}
}

func TestLoadReposInvalidName(t *testing.T) {
	root := t.TempDir()
	withConfigRoots(t, root)
	writeCatalogFile(t, root, "bad.name", fedoraCatalog)

	if _, err := LoadRepos(); err == nil {
		t.Fatal("expected error for dotted catalog name, got nil")
	}
}

func TestRepoByName(t *testing.T) {
	repos := []Repo{{Name: "community"}, {Name: "fedora"}}

	if r, ok := RepoByName(repos, "fedora"); !ok || r.Name != "fedora" {
		t.Errorf("RepoByName(fedora) = %v, %v", r, ok)
	}
	if _, ok := RepoByName(repos, "missing"); ok {
		t.Error("RepoByName(missing) should not be found")
	}
}

// TestLoadRepoFrom verifies the single-repo loader follows LoadReposFrom's
// first-root-wins precedence and is unaffected by other repos' files.
func TestLoadRepoFrom(t *testing.T) {
	etc, lib := t.TempDir(), t.TempDir()
	writeCatalogFile(t, etc, "fedora", "[Catalog]\nSiteURL=https://etc.example.com\nAllowUnsigned=yes\n")
	writeCatalogFile(t, lib, "fedora", "[Catalog]\nSiteURL=https://lib.example.com\n")
	writeCatalogFile(t, etc, "broken", "[Catalog]\nAllowUnsigned=yes\n")
	writeCatalogFile(t, lib, "libonly", "[Catalog]\nSiteURL=https://libonly.example.com\n")
	if err := os.Mkdir(filepath.Join(etc, "libonly.catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	roots := []string{etc, lib}

	repo, err := LoadRepoFrom(roots, "fedora")
	if err != nil || repo.SiteURL != "https://etc.example.com" || !repo.AllowUnsigned {
		t.Errorf("LoadRepoFrom(fedora) = %+v, %v; want the /etc file", repo, err)
	}
	if repo, err := LoadRepoFrom(roots, "libonly"); err != nil || repo.SiteURL != "https://libonly.example.com" {
		t.Errorf("LoadRepoFrom(libonly) = %+v, %v; want the lib file past a directory", repo, err)
	}
	if _, err := LoadRepoFrom(roots, "broken"); err == nil || !strings.Contains(err.Error(), "SiteURL is required") {
		t.Errorf("LoadRepoFrom(broken) error = %v, want parse error", err)
	}
	if _, err := LoadRepoFrom(roots, "missing"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("LoadRepoFrom(missing) error = %v, want ErrRepoNotFound", err)
	}
	if _, err := LoadRepoFrom(roots, "../fedora"); err == nil || errors.Is(err, ErrRepoNotFound) {
		t.Errorf("LoadRepoFrom(../fedora) error = %v, want invalid name", err)
	}
}

// TestLoadRepoFromDanglingOverride verifies a dangling higher-priority
// symlink is selected, as LoadReposFrom selects it, so an unloadable /etc
// override cannot be skipped in favor of a lower root's opt-in.
func TestLoadRepoFromDanglingOverride(t *testing.T) {
	etc, lib := t.TempDir(), t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.catalog"), filepath.Join(etc, "fedora.catalog")); err != nil {
		t.Fatal(err)
	}
	writeCatalogFile(t, lib, "fedora", "[Catalog]\nSiteURL=https://lib.example.com\nAllowUnsigned=yes\n")
	roots := []string{etc, lib}

	if repo, err := LoadRepoFrom(roots, "fedora"); err == nil || errors.Is(err, ErrRepoNotFound) {
		t.Errorf("LoadRepoFrom() = %+v, %v; want a load error for the dangling override", repo, err)
	}
	if _, err := LoadReposFrom(roots); err == nil {
		t.Error("LoadReposFrom() error = nil; the two loaders must agree on the dangling override")
	}
}
