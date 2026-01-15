package commands

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/frostyard/updex/cmd/common"
	"github.com/spf13/cobra"
)

// NewDiscoverCmd creates the discover command
func NewDiscoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discover URL",
		Short: "Discover available extensions from a remote repository",
		Long: `Discover available extensions from a remote repository.

Downloads the index file from {URL}/ext/index to get a list of available
extensions, then fetches SHA256SUMS for each extension to list available versions.

Example:
  instex discover https://example.com/sysext`,
		Args: cobra.ExactArgs(1),
		RunE: runDiscover,
	}
}

// ExtensionInfo represents discovered extension information
type ExtensionInfo struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Error    string   `json:"error,omitempty"`
}

// DiscoverResult represents the complete discovery result
type DiscoverResult struct {
	URL        string          `json:"url"`
	Extensions []ExtensionInfo `json:"extensions"`
}

func runDiscover(cmd *cobra.Command, args []string) error {
	baseURL := strings.TrimRight(args[0], "/")

	// Fetch the index file
	indexURL := baseURL + "/ext/index"
	extensions, err := fetchIndex(indexURL)
	if err != nil {
		return fmt.Errorf("failed to fetch index from %s: %w", indexURL, err)
	}

	if len(extensions) == 0 {
		if common.JSONOutput {
			result := DiscoverResult{
				URL:        baseURL,
				Extensions: []ExtensionInfo{},
			}
			common.OutputJSON(result)
			return nil
		}
		fmt.Println("No extensions found in repository.")
		return nil
	}

	// Fetch versions for each extension
	var results []ExtensionInfo
	for _, ext := range extensions {
		extURL := baseURL + "/ext/" + ext
		versions, err := fetchVersionsFromManifest(extURL)
		info := ExtensionInfo{
			Name:     ext,
			Versions: versions,
		}
		if err != nil {
			info.Error = err.Error()
		}
		results = append(results, info)
	}

	// Output results
	if common.JSONOutput {
		result := DiscoverResult{
			URL:        baseURL,
			Extensions: results,
		}
		common.OutputJSON(result)
		return nil
	}

	// Tabular output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "EXTENSION\tVERSIONS\n")
	for _, ext := range results {
		if ext.Error != "" {
			_, _ = fmt.Fprintf(w, "%s\t(error: %s)\n", ext.Name, ext.Error)
		} else if len(ext.Versions) == 0 {
			_, _ = fmt.Fprintf(w, "%s\t(no versions)\n", ext.Name)
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", ext.Name, strings.Join(ext.Versions, ", "))
		}
	}
	_ = w.Flush()

	return nil
}

// fetchVersionsFromManifest downloads SHA256SUMS and extracts versions from filenames
func fetchVersionsFromManifest(baseURL string) ([]string, error) {
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
	// Format: <hash>  <filename> or <hash> *<filename>
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
		// Common patterns: name_VERSION.raw, name_VERSION.raw.xz, etc.
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

// extractVersionFromFilename extracts the version from a filename
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

// sortVersionsDescending sorts versions in descending order (newest first)
func sortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		// Try semantic version comparison
		v1 := strings.TrimPrefix(versions[i], "v")
		v2 := strings.TrimPrefix(versions[j], "v")

		// Simple numeric comparison for common cases
		// Fall back to string comparison if not parseable
		return v1 > v2
	})
}
