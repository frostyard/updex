package updex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/download"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
)

// Install installs an extension from a remote repository.
func (c *Client) Install(ctx context.Context, url string, opts InstallOptions) (*InstallResult, error) {
	c.helper.BeginAction("Install extension")
	defer c.helper.EndAction()

	if opts.Component == "" {
		return nil, fmt.Errorf("component name is required")
	}

	baseURL := strings.TrimRight(url, "/")

	result := &InstallResult{
		Component: opts.Component,
	}

	// Fetch the index file to validate the extension exists
	c.helper.BeginTask("Validating extension")

	indexURL := baseURL + "/ext/index"
	extensions, err := c.fetchIndex(indexURL)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch index: %v", err)
		c.helper.EndTask()
		return result, fmt.Errorf("failed to fetch index from %s: %w", indexURL, err)
	}

	// Check if the extension is in the index
	found := false
	for _, ext := range extensions {
		if ext == opts.Component {
			found = true
			break
		}
	}

	if !found {
		result.Error = fmt.Sprintf("extension %q not found in repository index", opts.Component)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("extension %q not found in repository", opts.Component)
	}

	c.helper.Info("Extension found in repository")
	c.helper.EndTask()

	// Download the transfer file
	c.helper.BeginTask("Downloading transfer file")

	transferURL := fmt.Sprintf("%s/ext/%s/%s.transfer", baseURL, opts.Component, opts.Component)
	transferPath := filepath.Join("/etc/sysupdate.d", opts.Component+".transfer")

	err = c.downloadTransferFile(transferURL, transferPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to download transfer file: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, err
	}

	result.TransferFile = transferPath
	c.helper.Info(fmt.Sprintf("Installed transfer file: %s", transferPath))
	c.helper.EndTask()

	// Now trigger the update logic for this component
	c.helper.BeginTask("Installing extension")

	// Load the newly installed transfer config
	transfer, err := c.loadSingleTransfer(transferPath, opts.Component)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load transfer config: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, err
	}

	// Check if the transfer requires features that are not enabled
	if len(transfer.Transfer.Features) > 0 || len(transfer.Transfer.RequisiteFeatures) > 0 {
		features, err := config.LoadFeatures(c.config.Definitions)
		if err != nil {
			result.Error = fmt.Sprintf("failed to load features: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, err
		}

		filtered := config.FilterTransfersByFeatures([]*config.Transfer{transfer}, features)
		if len(filtered) == 0 {
			result.Error = "transfer requires features that are not enabled"
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("transfer requires features that are not enabled")
		}
	}

	// Run the install logic
	installedVersion, err := c.installTransfer(transfer, opts.NoRefresh)
	if err != nil {
		result.Error = fmt.Sprintf("failed to install: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, err
	}

	result.Version = installedVersion
	result.Installed = true
	result.NextActionMessage = "Reboot required to activate changes"

	c.helper.Info(fmt.Sprintf("Successfully installed version %s", installedVersion))
	c.helper.EndTask()

	return result, nil
}

// downloadTransferFile downloads a transfer file to the specified path.
func (c *Client) downloadTransferFile(url, destPath string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	// Ensure directory exists
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Read the response body
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Preprocess: strip comment-only lines starting with '#' or ';'
	var filteredLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// Keep empty lines and non-comment lines
		if trimmed == "" || (!strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, ";")) {
			filteredLines = append(filteredLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to process content: %w", err)
	}

	filteredContent := strings.Join(filteredLines, "\n") + "\n"

	// Create the file
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", destPath, err)
	}
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(filteredContent)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// loadSingleTransfer loads a single transfer file.
func (c *Client) loadSingleTransfer(filePath, componentName string) (*config.Transfer, error) {
	transfers, err := config.LoadTransfers(filepath.Dir(filePath))
	if err != nil {
		return nil, err
	}

	for _, t := range transfers {
		if t.Component == componentName {
			return t, nil
		}
	}

	return nil, fmt.Errorf("transfer config for %s not found after loading", componentName)
}

// installTransfer performs the update/install logic for a single transfer.
func (c *Client) installTransfer(transfer *config.Transfer, noRefresh bool) (string, error) {
	// Get available versions
	m, err := manifest.Fetch(transfer.Source.Path, c.config.Verify || transfer.Transfer.Verify)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	// Get all patterns
	patterns := transfer.Source.MatchPatterns
	if len(patterns) == 0 && transfer.Source.MatchPattern != "" {
		patterns = []string{transfer.Source.MatchPattern}
	}

	// Find available versions using all patterns
	versionSet := make(map[string]bool)
	for filename := range m.Files {
		if v, _, ok := version.ExtractVersionMulti(filename, patterns); ok {
			versionSet[v] = true
		}
	}

	if len(versionSet) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	available := make([]string, 0, len(versionSet))
	for v := range versionSet {
		available = append(available, v)
	}

	// Sort and get newest
	version.Sort(available)
	versionToInstall := available[0]

	// Check if already installed
	installed, current, _ := sysext.GetInstalledVersions(transfer)
	for _, v := range installed {
		if v == versionToInstall && v == current {
			return versionToInstall, nil // Already installed and current
		}
	}

	// Find the file for this version
	var sourceFile string
	var expectedHash string
	for filename, hash := range m.Files {
		if v, _, ok := version.ExtractVersionMulti(filename, patterns); ok && v == versionToInstall {
			sourceFile = filename
			expectedHash = hash
			break
		}
	}

	if sourceFile == "" {
		return "", fmt.Errorf("no file found for version %s", versionToInstall)
	}

	// Build target path using first target pattern
	targetPatterns := transfer.Target.MatchPatterns
	if len(targetPatterns) == 0 && transfer.Target.MatchPattern != "" {
		targetPatterns = []string{transfer.Target.MatchPattern}
	}

	targetPattern, err := version.ParsePattern(targetPatterns[0])
	if err != nil {
		return "", fmt.Errorf("invalid target pattern: %w", err)
	}

	targetFile := targetPattern.BuildFilename(versionToInstall)
	targetPath := filepath.Join(transfer.Target.Path, targetFile)

	// Download
	downloadURL := transfer.Source.Path + "/" + sourceFile
	err = download.Download(downloadURL, targetPath, expectedHash, transfer.Target.Mode)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Update symlink if configured
	if transfer.Target.CurrentSymlink != "" {
		err = sysext.UpdateSymlink(transfer.Target.Path, transfer.Target.CurrentSymlink, targetFile)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to update symlink: %v", err))
		}
	}

	// Link to /var/lib/extensions for systemd-sysext
	if err := sysext.LinkToSysext(transfer); err != nil {
		c.helper.Warning(fmt.Sprintf("failed to link to sysext: %v", err))
	}

	// Refresh systemd-sysext (unless --no-refresh)
	if !noRefresh {
		if err := sysext.Refresh(); err != nil {
			c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
		}
	} else {
		c.helper.Info("Skipping sysext refresh (--no-refresh)")
	}

	// Run vacuum
	if err := sysext.Vacuum(transfer); err != nil {
		c.helper.Warning(fmt.Sprintf("vacuum failed: %v", err))
	}

	return versionToInstall, nil
}
