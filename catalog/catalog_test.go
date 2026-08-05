package catalog

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// zoxideConf mirrors a real fedora-sysexts catalog .conf.
const zoxideConf = `[Transfer]
Verify=false

[Source]
Type=url-file
Path=https://extensions.example.com/fedora/zoxide/
MatchPattern=zoxide-@v-%w-%a.raw

[Target]
InstancesMax=2
Type=regular-file
Path=/var/lib/extensions.d/
MatchPattern=zoxide-@v-%w-%a.raw
CurrentSymlink=/var/lib/extensions/zoxide.raw
`

func TestList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		_, _ = w.Write([]byte(`[
			{"name": ".github", "type": "dir"},
			{"name": "LICENSES", "type": "dir"},
			{"name": "docs", "type": "dir"},
			{"name": "README.md", "type": "file"},
			{"name": "zoxide", "type": "dir"},
			{"name": "btop", "type": "dir"}
		]`))
	}))
	defer server.Close()

	repo := Repo{Name: "fedora", SiteURL: server.URL, ListURL: server.URL}
	names, err := List(t.Context(), server.Client(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"btop", "zoxide"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("List = %v, want %v", names, want)
	}
}

func TestListGitHubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	repo := Repo{Name: "fedora", SiteURL: server.URL, ListURL: server.URL}
	if _, err := List(t.Context(), server.Client(), repo); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
}

func TestListNoListURL(t *testing.T) {
	repo := Repo{Name: "fedora", SiteURL: "https://example.com"}
	if _, err := List(t.Context(), http.DefaultClient, repo); err == nil {
		t.Fatal("expected error for missing ListURL")
	}
}

func TestListHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	repo := Repo{Name: "fedora", SiteURL: server.URL, ListURL: server.URL}
	if _, err := List(t.Context(), server.Client(), repo); err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func TestFetchConf(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zoxide/zoxide.conf" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(zoxideConf))
	}))
	defer server.Close()

	repo := Repo{Name: "fedora", SiteURL: server.URL}

	data, err := FetchConf(t.Context(), server.Client(), repo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != zoxideConf {
		t.Errorf("unexpected conf content:\n%s", data)
	}

	_, err = FetchConf(t.Context(), server.Client(), repo, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRenderTransfer(t *testing.T) {
	out, err := RenderTransfer([]byte(zoxideConf), "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.Contains(got, "[Transfer]\nFeatures=zoxide\n") {
		t.Errorf("Features line not injected after [Transfer]:\n%s", got)
	}
	if strings.Contains(got, "CurrentSymlink") {
		t.Errorf("CurrentSymlink not dropped:\n%s", got)
	}
	// Specifiers must stay unexpanded so the file survives Fedora upgrades.
	if !strings.Contains(got, "MatchPattern=zoxide-@v-%w-%a.raw") {
		t.Errorf("specifiers not preserved:\n%s", got)
	}
	if !strings.Contains(got, "Path=https://extensions.example.com/fedora/zoxide/") {
		t.Errorf("source path not preserved:\n%s", got)
	}
	if !strings.Contains(got, "InstancesMax=2") {
		t.Errorf("target keys not preserved:\n%s", got)
	}
}

func TestRenderTransferNoTransferSection(t *testing.T) {
	conf := `[Source]
Type=url-file
Path=https://example.com/
MatchPattern=foo-@v.raw

[Target]
Type=regular-file
MatchPattern=foo-@v.raw
`
	out, err := RenderTransfer([]byte(conf), "foo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "\n[Transfer]\nFeatures=foo\n") {
		t.Errorf("missing appended [Transfer] section:\n%s", out)
	}
}

func TestRenderTransferReplacesExistingFeatures(t *testing.T) {
	conf := strings.Replace(zoxideConf, "Verify=false", "Verify=false\nFeatures=other", 1)
	out, err := RenderTransfer([]byte(conf), "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Features=other") {
		t.Errorf("existing Features not replaced:\n%s", out)
	}
	if strings.Count(string(out), "Features=") != 1 {
		t.Errorf("expected exactly one Features line:\n%s", out)
	}
}

func TestRenderTransferInvalid(t *testing.T) {
	if _, err := RenderTransfer([]byte("[Transfer]\nVerify=false\n"), "foo"); err == nil {
		t.Fatal("expected error for conf without [Source]/[Target]")
	}
	if _, err := RenderTransfer([]byte("not an ini \x00 file [["), "foo"); err == nil {
		t.Fatal("expected error for unparseable conf")
	}
}

func TestRenderFeature(t *testing.T) {
	repo := Repo{Name: "fedora", SiteURL: "https://extensions.example.com/fedora"}
	got := string(RenderFeature(repo, "zoxide"))

	want := `[Feature]
Description=zoxide sysext from the fedora catalog
Documentation=https://extensions.example.com/fedora/zoxide/
Enabled=false
`
	if got != want {
		t.Errorf("RenderFeature =\n%s\nwant\n%s", got, want)
	}
}
