package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

// ErrNotFound is returned by FetchConf when the sysext does not exist in
// the queried repo (HTTP 404), letting callers distinguish "not in this
// repo" from transport or server failures.
var ErrNotFound = errors.New("sysext not found in catalog")

// maxFetchSize bounds catalog HTTP response bodies; .conf files and
// contents-API listings are a few KB.
const maxFetchSize = 4 << 20

// nonSysextDirs are top-level repo directories that never hold a sysext.
var nonSysextDirs = []string{"docs", "LICENSES"}

// contentsEntry is the subset of a GitHub contents API entry List needs.
type contentsEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// List enumerates the sysexts available in repo via its ListURL (a GitHub
// contents API endpoint): top-level directories minus dotted names and
// known non-sysext directories, sorted. When the GITHUB_TOKEN environment
// variable is set it is sent as a bearer token to raise the API rate limit.
func List(ctx context.Context, client *http.Client, repo Repo) ([]string, error) {
	if repo.ListURL == "" {
		return nil, fmt.Errorf("catalog %q has no ListURL configured", repo.Name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repo.ListURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list catalog %q: %w", repo.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list catalog %q: %s returned %s", repo.Name, repo.ListURL, resp.Status)
	}

	var entries []contentsEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxFetchSize)).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode catalog %q listing: %w", repo.Name, err)
	}

	var names []string
	for _, e := range entries {
		if e.Type != "dir" || strings.HasPrefix(e.Name, ".") || slices.Contains(nonSysextDirs, e.Name) {
			continue
		}
		names = append(names, e.Name)
	}
	slices.Sort(names)

	return names, nil
}

// FetchConf downloads the catalog-published sysupdate transfer definition
// for a sysext (<SiteURL>/<name>/<name>.conf). A 404 is reported as
// ErrNotFound.
func FetchConf(ctx context.Context, client *http.Client, repo Repo, name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/%s.conf", repo.SiteURL, name, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create conf request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%q in catalog %q: %w", name, repo.Name, ErrNotFound)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("failed to fetch %s: %s", url, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", url, err)
	}

	return data, nil
}

// RenderTransfer turns a catalog-published .conf into the .transfer content
// updex writes to a component directory. It injects Features=<name> so the
// transfer is tied to its generated feature, and drops Target
// CurrentSymlink: updex manages the /var/lib/extensions link itself (see
// updex.installTransfer) and actively removes legacy current symlinks.
// Everything else — including %w/%a specifiers, which must stay unexpanded
// so the file remains valid across Fedora release upgrades — is preserved
// byte-for-byte, so the result diffs against the catalog original by
// exactly those two changes.
func RenderTransfer(conf []byte, name string) ([]byte, error) {
	cfg, err := ini.Load(conf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse catalog conf: %w", err)
	}
	if _, err := cfg.GetSection("Source"); err != nil {
		return nil, fmt.Errorf("catalog conf has no [Source] section")
	}
	if _, err := cfg.GetSection("Target"); err != nil {
		return nil, fmt.Errorf("catalog conf has no [Target] section")
	}

	featuresLine := "Features=" + name
	var out bytes.Buffer
	out.Grow(len(conf) + len(featuresLine) + 1)

	section := ""
	inserted := false
	for line := range strings.Lines(string(conf)) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed[1 : len(trimmed)-1]
			out.WriteString(line)
			if section == "Transfer" && !inserted {
				if !strings.HasSuffix(line, "\n") {
					out.WriteByte('\n')
				}
				out.WriteString(featuresLine + "\n")
				inserted = true
			}
			continue
		}
		switch section {
		case "Target":
			if iniKeyOf(trimmed) == "CurrentSymlink" {
				continue
			}
		case "Transfer":
			if iniKeyOf(trimmed) == "Features" {
				continue // replaced by the injected line
			}
		}
		out.WriteString(line)
	}
	if !inserted {
		if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
			out.WriteByte('\n')
		}
		out.WriteString("\n[Transfer]\n" + featuresLine + "\n")
	}

	return out.Bytes(), nil
}

// iniKeyOf returns the key name of an INI "Key=value" line (whitespace
// around '=' tolerated), or "" for comments, section headers, and blanks.
func iniKeyOf(trimmedLine string) string {
	if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") || strings.HasPrefix(trimmedLine, ";") {
		return ""
	}
	key, _, found := strings.Cut(trimmedLine, "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(key)
}

// RenderFeature builds the .feature content for a catalog sysext. The base
// file is disabled; enabling happens through the standard drop-in written
// by EnableFeature, so later enable/disable cycles work exactly like any
// hand-written feature.
func RenderFeature(repo Repo, name string) []byte {
	return fmt.Appendf(nil,
		"[Feature]\nDescription=%s sysext from the %s catalog\nDocumentation=%s/%s/\nEnabled=false\n",
		name, repo.Name, repo.SiteURL, name)
}
