package updex

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/catalog"
	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/manifest"
)

// signaturePolicyCatalog describes the HTTPS catalog served by
// newSignaturePolicyServer for one sysext.
type signaturePolicyCatalog struct {
	// verifyLine is the catalog conf's [Transfer] Verify line; empty omits it.
	verifyLine string
	// signed serves a valid SHA256SUMS.gpg; otherwise it is a 404.
	signed bool
	// wrongHash publishes a SHA256SUMS hash that does not match the image.
	wrongHash bool
	// omitImage publishes a SHA256SUMS that does not list the image.
	omitImage bool
	// downgrade names a file under /<name>/ whose request redirects to a
	// cleartext HTTP server serving the same content.
	downgrade string
}

const signaturePolicySysext = "git-lfs"

// newSignaturePolicyServer serves spec over TLS like extensions.fcos.fr and
// returns the server; the client's transport must trust it.
func newSignaturePolicyServer(t *testing.T, spec signaturePolicyCatalog) *httptest.Server {
	t.Helper()
	const name = signaturePolicySysext
	rawName := name + "-1.0.0.raw"
	rawContent := []byte("fake sysext image for " + name)

	hash := hashContent(rawContent)
	if spec.wrongHash {
		hash = hashContent([]byte("something else"))
	}
	listed := rawName
	if spec.omitImage {
		listed = name + "-0.9.0-other.img"
	}
	manifestContent := fmt.Appendf(nil, "%s  %s\n", hash, listed)
	var manifestSig []byte
	if spec.signed {
		manifestSig = testutil.SignManifest(t, manifestContent)
	}

	var server *httptest.Server
	content := func(path string) ([]byte, bool) {
		switch path {
		case "/" + name + "/" + name + ".conf":
			conf := "[Transfer]\n"
			if spec.verifyLine != "" {
				conf += spec.verifyLine + "\n"
			}
			conf += fmt.Sprintf(`
[Source]
Type=url-file
Path=%s/%s/
MatchPattern=%s-@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d/
MatchPattern=%s-@v.raw
`, server.URL, name, name, name)
			return []byte(conf), true
		case "/" + name + "/SHA256SUMS":
			return manifestContent, true
		case "/" + name + "/SHA256SUMS.gpg":
			return manifestSig, spec.signed
		case "/" + name + "/" + rawName:
			return rawContent, true
		}
		return nil, false
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := content(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(plain.Close)

	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if spec.downgrade != "" && r.URL.Path == "/"+name+"/"+spec.downgrade {
			http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
			return
		}
		data, ok := content(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

type signaturePolicyEnv struct {
	server         *httptest.Server
	definitionRoot string
	catalogRoot    string
	targetDir      string
	linkDir        string
}

func newSignaturePolicyEnv(t *testing.T, spec signaturePolicyCatalog) *signaturePolicyEnv {
	t.Helper()
	// SignManifest points manifest.KeyringPaths at the test keyring only for
	// signed catalogs; keep unsigned cases from consulting the host keyring.
	if !spec.signed {
		original := manifest.KeyringPaths
		manifest.KeyringPaths = []string{filepath.Join(t.TempDir(), "missing.gpg")}
		t.Cleanup(func() { manifest.KeyringPaths = original })
	}
	return &signaturePolicyEnv{
		server:         newSignaturePolicyServer(t, spec),
		definitionRoot: t.TempDir(),
		catalogRoot:    t.TempDir(),
		targetDir:      t.TempDir(),
		linkDir:        t.TempDir(),
	}
}

// writeRepo writes fedora.catalog for the env's server with extra lines.
func (e *signaturePolicyEnv) writeRepo(t *testing.T, extra string) {
	t.Helper()
	writeCatalogFileContent(t, e.catalogRoot, "fedora", "[Catalog]\nSiteURL="+e.server.URL+"\n"+extra)
}

func (e *signaturePolicyEnv) client(forceVerify bool) *Client {
	client := NewClient(ClientConfig{
		Verify: forceVerify,
		Paths: RuntimePaths{
			DefinitionRoots:    []string{e.definitionRoot},
			CatalogConfigRoots: []string{e.catalogRoot},
			CatalogCacheDir:    DisableCatalogCache,
			CatalogTargetPath:  e.targetDir,
			SysextLinkDir:      e.linkDir,
			RunExtensionsDir:   filepath.Join(e.definitionRoot, "run-extensions"),
		},
		SysextRunner: &catalogPathRunner{},
	})
	client.httpClient.Transport = e.server.Client().Transport
	return client
}

func (e *signaturePolicyEnv) componentDir() string {
	return filepath.Join(e.definitionRoot, "sysupdate.catalog-fedora.d")
}

func (e *signaturePolicyEnv) imagePath() string {
	return filepath.Join(e.targetDir, signaturePolicySysext+"-1.0.0.raw")
}

// checkError runs CheckFeatures on client and returns the sysext's
// per-transfer error.
func checkError(t *testing.T, client *Client) string {
	t.Helper()
	results, _ := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	for _, r := range results {
		for _, cr := range r.Results {
			if cr.Component == signaturePolicySysext {
				return cr.Error
			}
		}
	}
	t.Fatalf("no check result for %s: %+v", signaturePolicySysext, results)
	return ""
}

func TestCatalogAdd_SignaturePolicy(t *testing.T) {
	tests := []struct {
		name          string
		spec          signaturePolicyCatalog
		allowUnsigned bool
		forceVerify   bool
		// wantErr lists substrings of the add error; empty means success.
		wantErr      []string
		wantNotFound bool   // error wraps manifest.ErrSignatureNotFound
		wantVerify   string // rendered Verify line on success ("" = none)
	}{
		{
			name:         "unsigned catalog without opt-in fails with guidance",
			spec:         signaturePolicyCatalog{verifyLine: "Verify=false"},
			wantErr:      []string{`catalog "fedora" must publish a signed SHA256SUMS`, "AllowUnsigned=yes in fedora.catalog"},
			wantNotFound: true,
		},
		{
			name: "signed catalog without opt-in is verified",
			spec: signaturePolicyCatalog{verifyLine: "Verify=false", signed: true},
		},
		{
			name:          "opt-in honors catalog Verify=false",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false"},
			allowUnsigned: true,
			wantVerify:    "Verify=no",
		},
		{
			name:          "opt-in with catalog Verify=yes still requires a signature",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=yes"},
			allowUnsigned: true,
			wantErr:       []string{"the generated transfer for git-lfs requires a signed SHA256SUMS"},
			wantNotFound:  true,
		},
		{
			name:          "opt-in with no catalog Verify defaults to required",
			spec:          signaturePolicyCatalog{},
			allowUnsigned: true,
			wantErr:       []string{"the generated transfer for git-lfs requires a signed SHA256SUMS"},
			wantNotFound:  true,
		},
		{
			name:          "opt-in with a signed catalog verifies it",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=true", signed: true},
			allowUnsigned: true,
			wantVerify:    "Verify=yes",
		},
		{
			name:          "forced verification beats opt-in",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false"},
			allowUnsigned: true,
			forceVerify:   true,
			wantErr:       []string{"forced by client configuration"},
			wantNotFound:  true,
		},
		{
			name:          "opt-in still rejects a checksum mismatch",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false", wrongHash: true},
			allowUnsigned: true,
			wantErr:       []string{"hash mismatch"},
		},
		{
			name:          "opt-in still requires the image in SHA256SUMS",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false", omitImage: true},
			allowUnsigned: true,
			wantErr:       []string{"no versions available"},
		},
		{
			name:          "opt-in still refuses a manifest redirect downgrade",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false", downgrade: "SHA256SUMS"},
			allowUnsigned: true,
			wantErr:       []string{"redirect downgrade"},
		},
		{
			name:          "opt-in still refuses an image redirect downgrade",
			spec:          signaturePolicyCatalog{verifyLine: "Verify=false", downgrade: signaturePolicySysext + "-1.0.0.raw"},
			allowUnsigned: true,
			wantErr:       []string{"redirect downgrade"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newSignaturePolicyEnv(t, tt.spec)
			extra := ""
			if tt.allowUnsigned {
				extra = "AllowUnsigned=yes\n"
			}
			env.writeRepo(t, extra)

			_, err := env.client(tt.forceVerify).CatalogAdd(t.Context(), signaturePolicySysext, CatalogAddOptions{})

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("CatalogAdd() error = %v", err)
				}
				if _, err := os.Stat(env.imagePath()); err != nil {
					t.Errorf("image not installed: %v", err)
				}
				data, err := os.ReadFile(filepath.Join(env.componentDir(), signaturePolicySysext+".transfer"))
				if err != nil {
					t.Fatal(err)
				}
				gotVerify := ""
				for line := range strings.Lines(string(data)) {
					if strings.HasPrefix(line, "Verify") {
						gotVerify = strings.TrimSpace(line)
					}
				}
				if gotVerify != tt.wantVerify {
					t.Errorf("rendered Verify = %q, want %q\n%s", gotVerify, tt.wantVerify, data)
				}
				return
			}

			if err == nil {
				t.Fatal("CatalogAdd() error = nil, want failure")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("CatalogAdd() error = %q, want substring %q", err, want)
				}
			}
			if got := errors.Is(err, manifest.ErrSignatureNotFound); got != tt.wantNotFound {
				t.Errorf("errors.Is(err, ErrSignatureNotFound) = %v, want %v (err: %v)", got, tt.wantNotFound, err)
			}
			if _, err := os.Stat(env.imagePath()); !os.IsNotExist(err) {
				t.Errorf("failed add left an image behind: %v", err)
			}
			if _, err := os.Stat(env.componentDir()); !os.IsNotExist(err) {
				t.Errorf("failed add left generated definitions behind: %v", err)
			}
		})
	}
}

// TestCatalogSignaturePolicy_RevokingOptInRequiresSignature verifies the
// policy is re-read on every use by one long-lived client (as the daemon or
// an SDK consumer would hold): removing AllowUnsigned, or breaking the
// catalog configuration, makes the next check require a signature without
// re-adding or reconstructing the client.
func TestCatalogSignaturePolicy_RevokingOptInRequiresSignature(t *testing.T) {
	env := newSignaturePolicyEnv(t, signaturePolicyCatalog{verifyLine: "Verify=false"})
	env.writeRepo(t, "AllowUnsigned=yes\n")
	client := env.client(false)
	if _, err := client.CatalogAdd(t.Context(), signaturePolicySysext, CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd() error = %v", err)
	}
	if got := checkError(t, client); got != "" {
		t.Fatalf("check with opt-in error = %q, want none", got)
	}

	steps := []struct {
		name         string
		extra        string
		wantRequired bool
	}{
		{"opt-in removed", "", true},
		{"opt-in restored", "AllowUnsigned=yes\n", false},
		{"malformed configuration fails closed", "AllowUnsigned=maybe\n", true},
	}
	for _, step := range steps {
		env.writeRepo(t, step.extra)
		got := checkError(t, client)
		if !step.wantRequired {
			if got != "" {
				t.Errorf("%s: check error = %q, want none", step.name, got)
			}
			continue
		}
		if !strings.Contains(got, "AllowUnsigned=yes") || !strings.Contains(got, "signature not found") {
			t.Errorf("%s: check error = %q, want signature requirement", step.name, got)
		}
	}
}

// TestCatalogSignaturePolicy_MigratesV201Transfer verifies a transfer
// rendered before AllowUnsigned existed (catalog Verify dropped) keeps
// requiring a signature after the opt-in, and works once re-added.
func TestCatalogSignaturePolicy_MigratesV201Transfer(t *testing.T) {
	env := newSignaturePolicyEnv(t, signaturePolicyCatalog{verifyLine: "Verify=false"})
	env.writeRepo(t, "AllowUnsigned=yes\n")

	conf, err := catalog.FetchConf(t.Context(), env.client(false).httpClient, catalog.Repo{Name: "fedora", SiteURL: env.server.URL}, signaturePolicySysext)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := catalog.RenderTransferTo(conf, catalog.Repo{Name: "fedora", SiteURL: env.server.URL}, signaturePolicySysext, env.targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "Verify") {
		t.Fatalf("legacy render unexpectedly carries Verify:\n%s", legacy)
	}
	if err := os.MkdirAll(env.componentDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.componentDir(), signaturePolicySysext+".transfer"), legacy, 0644); err != nil {
		t.Fatal(err)
	}
	writeGeneratedFeature(t, env.componentDir(), "fedora", signaturePolicySysext)
	writeEnableDropIn(t, env.componentDir(), signaturePolicySysext, true)

	client := env.client(false)
	got := checkError(t, client)
	for _, want := range []string{"the generated transfer for git-lfs requires a signed SHA256SUMS", "re-run 'updex catalog add fedora/git-lfs'"} {
		if !strings.Contains(got, want) {
			t.Errorf("check of legacy transfer error = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, "declares Verify=yes") {
		t.Errorf("legacy transfer error claims the catalog declares Verify=yes: %q", got)
	}

	if _, err := client.CatalogAdd(t.Context(), signaturePolicySysext, CatalogAddOptions{}); err != nil {
		t.Fatalf("re-add error = %v", err)
	}
	if got := checkError(t, client); got != "" {
		t.Errorf("check after re-add error = %q, want none", got)
	}
}

func TestVerifyPolicy(t *testing.T) {
	const handWrittenBody = "\n[Source]\nType=url-file\nPath=https://example.com/x/\nMatchPattern=x-@v.raw\n\n[Target]\nType=regular-file\nPath=/var/lib/extensions.d/\nMatchPattern=x-@v.raw\n"
	repoAllowed := catalog.Repo{Name: "allowed", SiteURL: "https://example.com/allowed", AllowUnsigned: true}
	repoDenied := catalog.Repo{Name: "denied", SiteURL: "https://example.com/denied"}
	repoGone := catalog.Repo{Name: "gone", SiteURL: "https://example.com/gone"}

	tests := []struct {
		name       string
		file       string // .transfer content
		force      bool
		catalogs   string // "none" omits every .catalog file; "malformed" adds an invalid one
		wantVerify bool
		wantReason verifyReason
	}{
		{"hand-written Verify=no", "[Transfer]\nVerify=no\n" + handWrittenBody, false, "", false, verifyByTransfer},
		{"hand-written default", "[Transfer]\n" + handWrittenBody, false, "", true, verifyByTransfer},
		{"hand-written Verify=no forced", "[Transfer]\nVerify=no\n" + handWrittenBody, true, "", true, verifyForced},
		{"catalog opt-in Verify=no", renderForTest(t, repoAllowed), false, "", false, verifyByCatalog},
		{"catalog opt-in forced", renderForTest(t, repoAllowed), true, "", true, verifyForced},
		{"hand-edited Verify=no without opt-in", strings.Replace(renderForTest(t, repoDenied), "Features=x\n", "Features=x\nVerify=no\n", 1), false, "", true, verifyCatalogNotAllowed},
		{"owning catalog no longer configured", strings.Replace(renderForTest(t, repoGone), "Features=x\n", "Features=x\nVerify=no\n", 1), false, "", true, verifyCatalogNotAllowed},
		{"no catalogs configured at all", renderForTest(t, repoAllowed), false, "none", true, verifyCatalogNotAllowed},
		{"malformed catalog configuration fails closed", renderForTest(t, repoAllowed), false, "malformed", true, verifyCatalogNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalogRoot := t.TempDir()
			if tt.catalogs != "none" {
				writeCatalogFileContent(t, catalogRoot, "allowed", "[Catalog]\nSiteURL=https://example.com/allowed\nAllowUnsigned=yes\n")
				writeCatalogFileContent(t, catalogRoot, "denied", "[Catalog]\nSiteURL=https://example.com/denied\n")
			}
			if tt.catalogs == "malformed" {
				writeCatalogFileContent(t, catalogRoot, "broken", "[Catalog]\nAllowUnsigned=yes\n")
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "x.transfer")
			if err := os.WriteFile(path, []byte(tt.file), 0644); err != nil {
				t.Fatal(err)
			}
			transfers, err := config.LoadTransfers(dir)
			if err != nil || len(transfers) != 1 {
				t.Fatalf("LoadTransfers() = %v, %v", transfers, err)
			}
			client := NewClient(ClientConfig{Verify: tt.force, Paths: RuntimePaths{CatalogConfigRoots: []string{catalogRoot}}})

			got := client.verifyPolicy(transfers[0])
			if got.required != tt.wantVerify || got.reason != tt.wantReason {
				t.Errorf("verifyPolicy() = %+v, want required=%v reason=%v", got, tt.wantVerify, tt.wantReason)
			}
		})
	}
}

// renderForTest renders a Verify=no catalog conf for sysext "x" of repo.
func renderForTest(t *testing.T, repo catalog.Repo) string {
	t.Helper()
	conf := "[Transfer]\nVerify=no\n\n[Source]\nType=url-file\nPath=" + repo.SiteURL + "/x/\nMatchPattern=x-@v.raw\n\n[Target]\nType=regular-file\nPath=/var/lib/extensions.d/\nMatchPattern=x-@v.raw\n"
	out, err := catalog.RenderTransferTo([]byte(conf), repo, "x", "/var/lib/extensions.d")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
