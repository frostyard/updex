package catalog

import (
	"errors"
	"os"
	"path/filepath"
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
