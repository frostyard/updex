package updex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Discover finds available extensions from a remote repository.
func (c *Client) Discover(ctx context.Context, url string) (*DiscoverResult, error) {
	c.helper.BeginAction("Discover extensions")
	defer c.helper.EndAction()

	baseURL := strings.TrimRight(url, "/")

	c.helper.BeginTask("Fetching index")

	// Fetch the index file
	indexURL := baseURL + "/ext/index"
	extensions, err := c.fetchIndex(indexURL)
	if err != nil {
		c.helper.EndTask()
		return nil, fmt.Errorf("failed to fetch index from %s: %w", indexURL, err)
	}

	c.helper.Info(fmt.Sprintf("Found %d extension(s)", len(extensions)))
	c.helper.EndTask()

	if len(extensions) == 0 {
		return &DiscoverResult{
			URL:        baseURL,
			Extensions: []ExtensionInfo{},
		}, nil
	}

	// Fetch versions for each extension
	var results []ExtensionInfo
	for _, ext := range extensions {
		c.helper.BeginTask(fmt.Sprintf("Fetching versions for %s", ext))

		extURL := baseURL + "/ext/" + ext
		versions, err := c.fetchVersionsFromManifest(extURL)
		info := ExtensionInfo{
			Name:     ext,
			Versions: versions,
		}
		if err != nil {
			info.Error = err.Error()
			c.helper.Warning(info.Error)
		} else {
			c.helper.Info(fmt.Sprintf("Found %d version(s)", len(versions)))
		}
		results = append(results, info)

		c.helper.EndTask()
	}

	return &DiscoverResult{
		URL:        baseURL,
		Extensions: results,
	}, nil
}

// fetchIndex downloads and parses the index file.
func (c *Client) fetchIndex(url string) ([]string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var extensions []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			extensions = append(extensions, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	return extensions, nil
}

// fetchVersionsFromManifest downloads SHA256SUMS and extracts versions from filenames.
func (c *Client) fetchVersionsFromManifest(baseURL string) ([]string, error) {
	manifestURL := strings.TrimRight(baseURL, "/") + "/SHA256SUMS"

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse SHA256SUMS and extract versions from filenames
	versionSet := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		filename := parts[1]
		filename = strings.TrimPrefix(filename, "*")

		// Extract version from filename
		version := extractVersionFromFilename(filename)
		if version != "" {
			versionSet[version] = true
		}
	}

	// Convert to sorted slice (newest first)
	var versions []string
	for v := range versionSet {
		versions = append(versions, v)
	}
	sortVersionsDescending(versions)

	return versions, nil
}

// extractVersionFromFilename extracts the version from a filename.
// Expected format: EXTENSION_VERSION_ARCH.raw{.compression}
// Example: vscode_1.108.0_amd64.raw.xz -> 1.108.0
func extractVersionFromFilename(filename string) string {
	// Remove compression and .raw extensions
	name := filename
	for _, ext := range []string{".xz", ".gz", ".zst", ".zstd"} {
		name = strings.TrimSuffix(name, ext)
	}
	name = strings.TrimSuffix(name, ".raw")

	// Split by underscore: EXTENSION_VERSION_ARCH
	parts := strings.Split(name, "_")
	if len(parts) < 3 {
		return ""
	}

	// Version is the second segment
	return parts[1]
}

// sortVersionsDescending sorts versions in descending order (newest first).
func sortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		v1 := strings.TrimPrefix(versions[i], "v")
		v2 := strings.TrimPrefix(versions[j], "v")
		return v1 > v2
	})
}
