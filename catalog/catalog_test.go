package catalog

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/config"
	"gopkg.in/ini.v1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

	repo := Repo{
		Name:          "fedora",
		SiteURL:       server.URL,
		ListURL:       server.URL,
		AllowInsecure: true,
	}
	names, err := List(t.Context(), server.Client(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"btop", "zoxide"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("List = %v, want %v", names, want)
	}
}

func TestListGitHubTokenNotSentToUntrustedOrigin(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	repo := Repo{
		Name:          "fedora",
		SiteURL:       server.URL,
		ListURL:       server.URL,
		AllowInsecure: true,
	}
	if _, err := List(t.Context(), server.Client(), repo); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Error("untrusted catalog origin received authorization")
	}
}

func TestListGitHubTokenSentToTrustedOrigin(t *testing.T) {
	const token = "test-token"
	t.Setenv("GITHUB_TOKEN", token)

	var gotAuthorization string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuthorization = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[]`)),
			Request:    req,
		}, nil
	})}

	repo := Repo{
		Name:      "fedora",
		SiteURL:   "https://extensions.example.com",
		ListURL:   "https://api.github.com/repos/example/catalog/contents/",
		Component: "catalog-fedora",
	}
	if _, err := List(t.Context(), client, repo); err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer "+token {
		t.Error("trusted GitHub API request did not receive authorization")
	}
}

func TestListGitHubTokenStrippedOnCrossOriginRedirect(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var redirectedAuthorization string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Port() == "" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://api.github.com:444/list"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}
		redirectedAuthorization = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[]`)),
			Request:    req,
		}, nil
	})}

	repo := Repo{
		Name:      "fedora",
		SiteURL:   "https://extensions.example.com",
		ListURL:   "https://api.github.com/repos/example/catalog/contents/",
		Component: "catalog-fedora",
	}
	if _, err := List(t.Context(), client, repo); err != nil {
		t.Fatal(err)
	}
	if redirectedAuthorization != "" {
		t.Error("cross-origin redirect received authorization")
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

	repo := Repo{
		Name:          "fedora",
		SiteURL:       server.URL,
		ListURL:       server.URL,
		AllowInsecure: true,
	}
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

	repo := Repo{Name: "fedora", SiteURL: server.URL, AllowInsecure: true}

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

	// Traversal-shaped names are rejected before any request is made.
	if _, err := FetchConf(t.Context(), server.Client(), repo, "../etc/passwd"); err == nil {
		t.Fatal("expected error for traversal name")
	}
}

func TestValidateSysextName(t *testing.T) {
	for _, valid := range []string{"zoxide", "1password-cli", "nvidia-driver-cuda-580.95.05", "kubernetes-cri-o-1.32", "WALinuxAgent", "fuse2", "git_email+x"} {
		if err := ValidateSysextName(valid); err != nil {
			t.Errorf("ValidateSysextName(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "..", ".hidden", "-dash", "a/b", "../x", "a b", "a\x00b"} {
		if err := ValidateSysextName(invalid); err == nil {
			t.Errorf("ValidateSysextName(%q) = nil, want error", invalid)
		}
	}
}

var testRepo = Repo{Name: "fedora", SiteURL: "https://extensions.example.com/fedora"}

func TestRenderTransfer(t *testing.T) {
	out, err := RenderTransfer([]byte(zoxideConf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !IsGenerated(out) {
		t.Errorf("output missing GeneratedMarker header:\n%s", got)
	}
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
		t.Errorf("source path not canonicalized:\n%s", got)
	}
	if !strings.Contains(got, "Path=/var/lib/extensions.d\nMatchPattern=zoxide-@v-%w-%a.raw\nMode=0644\n") {
		t.Errorf("target path and mode not canonicalized:\n%s", got)
	}
}

func TestRenderTransferStripsCatalogVerifyFalse(t *testing.T) {
	out, err := RenderTransfer([]byte(zoxideConf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Verify") {
		t.Errorf("catalog-supplied Verify key not stripped, letting config/transfer.go's true default apply:\n%s", out)
	}
}

// TestRenderTransferStripsAlternateVerifySpellings guards against catalogs
// that spell the "Verify" key using gopkg.in/ini.v1-accepted syntax that a
// naive "Key=value" split would miss: the ':' delimiter and
// backtick/double-quote-quoted key names.
func TestRenderTransferStripsAlternateVerifySpellings(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"colon delimiter", "Verify:no"},
		{"colon delimiter with spaces", "Verify : no"},
		{"double-quoted key with equals", `"Verify"=no`},
		{"backtick-quoted key with equals", "`Verify`=no"},
		{"double-quoted key with colon", `"Verify":no`},
		{"triple-quoted key with equals", `"""Verify"""=no`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := strings.Replace(zoxideConf, "Verify=false", tt.line, 1)
			out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "Verify") {
				t.Errorf("catalog-supplied %q not stripped:\n%s", tt.line, out)
			}
		})
	}
}

// TestRenderTransferAlternateVerifySpellingDoesNotDisableVerification proves
// the stripped output actually leaves GPG verification on end-to-end: a
// hostile catalog conf spelling "Verify" in a way a naive parser would miss
// must not survive into config.LoadTransfers with Verify == false.
func TestRenderTransferAlternateVerifySpellingDoesNotDisableVerification(t *testing.T) {
	conf := strings.Replace(zoxideConf, "Verify=false", `"""Verify"""=no`+"\n"+`"""Features"""=evil`, 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Verify") {
		t.Errorf("catalog-supplied triple-quoted Verify not stripped:\n%s", out)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zoxide.transfer"), out, 0o644); err != nil {
		t.Fatal(err)
	}

	transfers, err := config.LoadTransfers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected exactly one transfer, got %d", len(transfers))
	}
	if !transfers[0].Transfer.Verify {
		t.Errorf("hostile catalog Verify spelling disabled verification: %+v", transfers[0].Transfer)
	}
	if got := transfers[0].Transfer.Features; len(got) != 1 || got[0] != "zoxide" {
		t.Errorf("hostile catalog Features spelling overrode the injected Features line: got %v, want [zoxide]", got)
	}
}

// TestRenderTransferPreservesMultilineValues proves the keyless-line guard
// added for alternate Verify spellings does not eat the body of a legitimate
// gopkg.in/ini.v1 multiline value. Continuation lines carry no key/value
// delimiter, so a blanket keyless drop deletes them and leaves the rendered
// .transfer holding an unterminated value.
func TestRenderTransferPreservesMultilineValues(t *testing.T) {
	tests := []struct {
		name string
		// block replaces the catalog conf's "Verify=false" line.
		block string
		// want are substrings the rendered output must still contain.
		want []string
	}{
		{
			name:  "triple-quoted multiline",
			block: "ProtectVersion=\"\"\"\n1.2\n\"\"\"",
			want:  []string{"ProtectVersion=\"\"\"", "\n1.2\n", "\"\"\""},
		},
		{
			name:  "backtick multiline",
			block: "ProtectVersion=`\n1.2\n`",
			want:  []string{"ProtectVersion=`", "\n1.2\n", "`"},
		},
		{
			name:  "multiline body that looks like a section header",
			block: "ProtectVersion=\"\"\"\n[Source]\n\"\"\"",
			want:  []string{"ProtectVersion=\"\"\"", "\n[Source]\n", "\"\"\""},
		},
		{
			name:  "multiline body that looks like a bare word",
			block: "ProtectVersion=\"\"\"\nnot a key at all\n\"\"\"",
			want:  []string{"\nnot a key at all\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := strings.Replace(zoxideConf, "Verify=false", tt.block, 1)
			out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(out), want) {
					t.Errorf("rendered .transfer dropped %q from a multiline value:\n%s", want, out)
				}
			}

			// The rendered file must still parse, and the multiline value must
			// survive with the value ini.v1 read from the catalog conf.
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "zoxide.transfer"), out, 0o644); err != nil {
				t.Fatal(err)
			}
			transfers, err := config.LoadTransfers(dir)
			if err != nil {
				t.Fatalf("rendered .transfer no longer parses: %v\n%s", err, out)
			}
			if len(transfers) != 1 {
				t.Fatalf("expected exactly one transfer, got %d", len(transfers))
			}
			if !transfers[0].Transfer.Verify {
				t.Errorf("multiline passthrough disabled verification: %+v", transfers[0].Transfer)
			}
			if got := transfers[0].Transfer.Features; len(got) != 1 || got[0] != "zoxide" {
				t.Errorf("Features = %v, want [zoxide]", got)
			}
		})
	}
}

// TestRenderTransferStripsMultilineVerifyAndFeatures is the non-vacuity guard
// for TestRenderTransferPreservesMultilineValues: passing continuation lines
// through must not become a way to smuggle a Verify or Features override in.
// When the key that opens the multiline is one this renderer strips, the whole
// block goes, not just its first line.
func TestRenderTransferStripsMultilineVerifyAndFeatures(t *testing.T) {
	tests := []struct {
		name  string
		block string
	}{
		{"multiline Verify", "Verify=\"\"\"\nno\n\"\"\""},
		{"multiline Features", "Features=\"\"\"\nevil\n\"\"\""},
		{"backtick multiline Verify", "Verify=`\nno\n`"},
		{"triple-quoted key opening a multiline Verify", "\"\"\"Verify\"\"\"=\"\"\"\nno\n\"\"\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := strings.Replace(zoxideConf, "Verify=false", tt.block, 1)
			out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
			if err != nil {
				t.Fatal(err)
			}
			// Neither the opening line nor any body line may survive: a
			// leftover body line would be stray text in the rendered file.
			for _, gone := range []string{"Verify", "no\n", "evil"} {
				if strings.Contains(string(out), gone) {
					t.Errorf("stripped multiline block leaked %q:\n%s", gone, out)
				}
			}

			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "zoxide.transfer"), out, 0o644); err != nil {
				t.Fatal(err)
			}
			transfers, err := config.LoadTransfers(dir)
			if err != nil {
				t.Fatalf("rendered .transfer no longer parses: %v\n%s", err, out)
			}
			if len(transfers) != 1 {
				t.Fatalf("expected exactly one transfer, got %d", len(transfers))
			}
			if !transfers[0].Transfer.Verify {
				t.Errorf("multiline catalog override disabled verification: %+v", transfers[0].Transfer)
			}
			if got := transfers[0].Transfer.Features; len(got) != 1 || got[0] != "zoxide" {
				t.Errorf("multiline catalog override changed Features: got %v, want [zoxide]", got)
			}
		})
	}
}

func TestRenderTransferContinuationCannotSmuggleVerify(t *testing.T) {
	block := "Dummy=value\\\n[Other]\nVerify=no"
	conf := strings.Replace(zoxideConf, "Verify=false", block, 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Dummy=value\\\n[Other]\n") {
		t.Fatalf("legitimate continuation block was not preserved byte-for-byte:\n%s", out)
	}

	transfer := loadRenderedTransfer(t, out)
	if !transfer.Transfer.Verify {
		t.Errorf("continuation-carried section header smuggled Verify=no: %+v", transfer.Transfer)
	}
}

func TestRenderTransferContinuationCannotSmuggleFeatures(t *testing.T) {
	block := "Dummy=value\\\n[Other]\nFeatures=evil"
	conf := strings.Replace(zoxideConf, "Verify=false", block, 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}

	transfer := loadRenderedTransfer(t, out)
	if got := transfer.Transfer.Features; len(got) != 1 || got[0] != "zoxide" {
		t.Errorf("continuation-carried section header smuggled Features override: got %v, want [zoxide]", got)
	}
}

func TestRenderTransferStripsContinuationBodies(t *testing.T) {
	tests := []struct {
		name string
		conf string
		gone []string
	}{
		{
			name: "stripped Verify cannot inject Path",
			conf: strings.Replace(zoxideConf, "Verify=false", "Verify=x\\\nPath=/etc/evil", 1),
			gone: []string{"Verify=x", "Path=/etc/evil"},
		},
		{
			name: "stripped Features cannot inject a requisite",
			conf: strings.Replace(zoxideConf, "Verify=false", "Features=evil\\\nRequisiteFeatures=bad", 1),
			gone: []string{"Features=evil", "RequisiteFeatures=bad"},
		},
		{
			name: "rewritten section drops the whole continuation",
			conf: strings.Replace(zoxideConf, "MatchPattern=zoxide-@v-%w-%a.raw\n\n[Target]", "MatchPattern=zoxide-@v-%w-%a.raw\nSourceNote=value\\\n[Other]\nLeaked=value\n\n[Target]", 1),
			gone: []string{"SourceNote", "[Other]", "Leaked=value"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := RenderTransfer([]byte(tt.conf), testRepo, "zoxide")
			if err != nil {
				t.Fatal(err)
			}
			for _, gone := range tt.gone {
				if strings.Contains(string(out), gone) {
					t.Errorf("stripped continuation block leaked %q:\n%s", gone, out)
				}
			}
			_ = loadRenderedTransfer(t, out)
		})
	}
}

func TestRenderTransferPreservesContinuationValue(t *testing.T) {
	block := "ProtectVersion=1.2\\\n3"
	conf := strings.Replace(zoxideConf, "Verify=false", block, 1)
	wantConfig, err := ini.Load([]byte(conf))
	if err != nil {
		t.Fatal(err)
	}
	want := wantConfig.Section("Transfer").Key("ProtectVersion").String()

	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), block) {
		t.Fatalf("legitimate continuation block was not preserved byte-for-byte:\n%s", out)
	}
	renderedConfig, err := ini.Load(out)
	if err != nil {
		t.Fatalf("rendered .transfer no longer parses: %v\n%s", err, out)
	}
	if got := renderedConfig.Section("Transfer").Key("ProtectVersion").String(); got != want {
		t.Errorf("rendered continuation value = %q, want ini.v1 catalog value %q", got, want)
	}
}

func loadRenderedTransfer(t *testing.T, out []byte) *config.Transfer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zoxide.transfer"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	transfers, err := config.LoadTransfers(dir)
	if err != nil {
		t.Fatalf("rendered .transfer no longer parses: %v\n%s", err, out)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected exactly one transfer, got %d", len(transfers))
	}
	return transfers[0]
}

func TestRenderTransferToRewritesProductionTarget(t *testing.T) {
	out, err := RenderTransferTo([]byte(zoxideConf), testRepo, "zoxide", "/isolated/extensions.d")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Path=/isolated/extensions.d\n") {
		t.Fatalf("custom target not rendered:\n%s", out)
	}
}

func TestRenderTransferNoTransferSection(t *testing.T) {
	conf := `[Source]
Type=url-file
Path=https://extensions.example.com/fedora/foo/
MatchPattern=foo-@v.raw

[Target]
Type=regular-file
MatchPattern=foo-@v.raw
`
	out, err := RenderTransfer([]byte(conf), testRepo, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "\n[Transfer]\nFeatures=foo\n") {
		t.Errorf("missing appended [Transfer] section:\n%s", out)
	}
}

func TestRenderTransferReplacesExistingFeatures(t *testing.T) {
	conf := strings.Replace(zoxideConf, "Verify=false", "Verify=false\nFeatures=other", 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
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
	if _, err := RenderTransfer([]byte("[Transfer]\nVerify=false\n"), testRepo, "foo"); err == nil {
		t.Fatal("expected error for conf without [Source]/[Target]")
	}
	if _, err := RenderTransfer([]byte("not an ini \x00 file [["), testRepo, "foo"); err == nil {
		t.Fatal("expected error for unparseable conf")
	}
}

func TestRenderTransferRejectsUnsafeCatalogMetadata(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "alternate source", old: "Path=https://extensions.example.com/fedora/zoxide/", new: "Path=https://attacker.example/zoxide/"},
		{name: "source traversal pattern", old: "MatchPattern=zoxide-@v-%w-%a.raw", new: "MatchPattern=../zoxide-@v.raw"},
		{name: "target traversal pattern", old: "MatchPattern=zoxide-@v-%w-%a.raw\nCurrentSymlink", new: "MatchPattern=../authorized_keys-@v\nCurrentSymlink"},
		{name: "quoted pattern", old: "MatchPattern=zoxide-@v-%w-%a.raw", new: `MatchPattern="zoxide @v.raw"`},
		{name: "arbitrary target", old: "Path=/var/lib/extensions.d/", new: "Path=/root/.ssh"},
		{name: "relative target", old: "Path=/var/lib/extensions.d/", new: "Path=/var/lib/extensions.d/\nPathRelativeTo=boot"},
		{name: "non-file source", old: "Type=url-file", new: "Type=url-tar"},
		{name: "non-file target", old: "Type=regular-file", new: "Type=directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := strings.Replace(zoxideConf, tt.old, tt.new, 1)
			if _, err := RenderTransfer([]byte(conf), testRepo, "zoxide"); err == nil {
				t.Fatal("expected unsafe catalog metadata to be rejected")
			}
		})
	}
}

func TestRenderTransferNormalizesSensitiveTargetKeys(t *testing.T) {
	conf := strings.Replace(zoxideConf, "CurrentSymlink=/var/lib/extensions/zoxide.raw", "CurrentSymlink=/var/lib/extensions/zoxide.raw\nMode=0777", 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "0777") {
		t.Errorf("catalog-controlled mode preserved:\n%s", got)
	}
	if strings.Count(got, "Mode=0644") != 1 {
		t.Errorf("expected one canonical mode:\n%s", got)
	}
}

func TestRenderTransferCanonicalizesCommentedSections(t *testing.T) {
	conf := strings.Replace(zoxideConf, "[Target]", "[Target] # upstream comment", 1)
	conf = strings.Replace(conf, "CurrentSymlink=/var/lib/extensions/zoxide.raw", "CurrentSymlink=../../../etc/localtime\nMode=0777", 1)
	out, err := RenderTransfer([]byte(conf), testRepo, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "CurrentSymlink") || strings.Contains(got, "0777") {
		t.Errorf("commented target section retained sensitive metadata:\n%s", got)
	}
	if !strings.Contains(got, "Path=/var/lib/extensions.d\nMatchPattern=zoxide-@v-%w-%a.raw\nMode=0644") {
		t.Errorf("commented target section was not canonicalized:\n%s", got)
	}
}

func TestRenderFeature(t *testing.T) {
	got := string(RenderFeature(testRepo, "zoxide"))

	want := `# Generated by updex catalog (repo: fedora); managed by 'updex catalog'
[Feature]
Description=zoxide sysext
Documentation=https://extensions.example.com/fedora/zoxide/
Enabled=false
`
	if got != want {
		t.Errorf("RenderFeature =\n%s\nwant\n%s", got, want)
	}
	if !IsGenerated([]byte(got)) {
		t.Error("RenderFeature output not recognized by IsGenerated")
	}
}

func TestGeneratedRepo(t *testing.T) {
	repo, ok := GeneratedRepo(RenderFeature(testRepo, "zoxide"))
	if !ok || repo != "fedora" {
		t.Errorf("GeneratedRepo = (%q, %v), want (fedora, true)", repo, ok)
	}

	conf := strings.Replace(zoxideConf, testRepo.SiteURL, "https://x", 1)
	out, err := RenderTransfer([]byte(conf), Repo{Name: "community", SiteURL: "https://x"}, "zoxide")
	if err != nil {
		t.Fatal(err)
	}
	if repo, ok := GeneratedRepo(out); !ok || repo != "community" {
		t.Errorf("GeneratedRepo(transfer) = (%q, %v), want (community, true)", repo, ok)
	}

	if _, ok := GeneratedRepo([]byte("[Feature]\nEnabled=true\n")); ok {
		t.Error("hand-written content reported as generated")
	}
}

func TestGeneratedFileRepo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zoxide.feature")
	if err := os.WriteFile(path, RenderFeature(testRepo, "zoxide"), 0644); err != nil {
		t.Fatal(err)
	}

	if repo, ok := GeneratedFileRepo(path); !ok || repo != "fedora" {
		t.Errorf("GeneratedFileRepo = (%q, %v), want (fedora, true)", repo, ok)
	}
	if _, ok := GeneratedFileRepo(filepath.Join(dir, "missing")); ok {
		t.Error("missing file reported as generated")
	}
}

func TestIsGeneratedFile(t *testing.T) {
	dir := t.TempDir()

	generated := filepath.Join(dir, "gen.feature")
	if err := os.WriteFile(generated, RenderFeature(testRepo, "zoxide"), 0644); err != nil {
		t.Fatal(err)
	}
	handmade := filepath.Join(dir, "hand.feature")
	if err := os.WriteFile(handmade, []byte("[Feature]\nEnabled=true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !IsGeneratedFile(generated) {
		t.Error("generated file not recognized")
	}
	if IsGeneratedFile(handmade) {
		t.Error("hand-written file wrongly recognized as generated")
	}
	if IsGeneratedFile(filepath.Join(dir, "missing.feature")) {
		t.Error("missing file wrongly recognized as generated")
	}
}
