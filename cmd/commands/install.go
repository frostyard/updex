package commands

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/download"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
	"github.com/spf13/cobra"
)

// NewInstallCmd creates the install command
func NewInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install URL",
		Short: "Install an extension from a remote repository",
		Long: `Install an extension from a remote repository.

Downloads the transfer file from the repository and places it in /etc/sysupdate.d/,
then downloads and installs the extension.

Requires --component flag to specify which extension to install.

Example:
  instex install https://repo.frostyard.org --component vscode`,
		Args: cobra.ExactArgs(1),
		RunE: runInstall,
	}
}

// InstallResult represents the result of an install operation
type InstallResult struct {
	Component    string `json:"component"`
	TransferFile string `json:"transfer_file"`
	Version      string `json:"version,omitempty"`
	Installed    bool   `json:"installed"`
	Error        string `json:"error,omitempty"`
}

func runInstall(cmd *cobra.Command, args []string) error {
	if common.Component == "" {
		return fmt.Errorf("--component flag is required")
	}

	baseURL := strings.TrimRight(args[0], "/")

	result := InstallResult{
		Component: common.Component,
	}

	// Fetch the index file to validate the extension exists
	indexURL := baseURL + "/ext/index"
	extensions, err := fetchIndex(indexURL)
	if err != nil {
		result.Error = fmt.Sprintf("failed to fetch index: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		}
		return fmt.Errorf("failed to fetch index from %s: %w", indexURL, err)
	}

	// Check if the extension is in the index
	found := false
	for _, ext := range extensions {
		if ext == common.Component {
			found = true
			break
		}
	}

	if !found {
		result.Error = fmt.Sprintf("extension %q not found in repository index", common.Component)
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			fmt.Fprintf(os.Stderr, "Available extensions: %s\n", strings.Join(extensions, ", "))
		}
		return fmt.Errorf("extension %q not found in repository", common.Component)
	}

	// Download the transfer file
	transferURL := fmt.Sprintf("%s/ext/%s/%s.transfer", baseURL, common.Component, common.Component)
	transferPath := filepath.Join("/etc/sysupdate.d", common.Component+".transfer")

	if !common.JSONOutput {
		fmt.Printf("Downloading transfer file for %s...\n", common.Component)
	}

	err = downloadTransferFile(transferURL, transferPath)
	if err != nil {
		result.Error = fmt.Sprintf("failed to download transfer file: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		}
		return err
	}

	result.TransferFile = transferPath

	if !common.JSONOutput {
		fmt.Printf("Installed transfer file: %s\n", transferPath)
	}

	// Now trigger the update logic for this component
	if !common.JSONOutput {
		fmt.Printf("Installing %s...\n", common.Component)
	}

	// Load the newly installed transfer config
	transfer, err := loadSingleTransfer(transferPath, common.Component)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load transfer config: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		}
		return err
	}

	// Run the update logic
	installedVersion, err := installTransfer(transfer)
	if err != nil {
		result.Error = fmt.Sprintf("failed to install: %v", err)
		if common.JSONOutput {
			common.OutputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		}
		return err
	}

	result.Version = installedVersion
	result.Installed = true

	if common.JSONOutput {
		common.OutputJSON(result)
	} else {
		fmt.Printf("Successfully installed %s version %s\n", common.Component, installedVersion)
	}

	return nil
}

// fetchIndex downloads and parses the index file
func fetchIndex(url string) ([]string, error) {
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

// downloadTransferFile downloads a transfer file to the specified path
func downloadTransferFile(url, destPath string) error {
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

	// Create the file
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", destPath, err)
	}
	defer func() { _ = f.Close() }()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// loadSingleTransfer loads a single transfer file
func loadSingleTransfer(filePath, componentName string) (*config.Transfer, error) {
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

// installTransfer performs the update/install logic for a single transfer
func installTransfer(transfer *config.Transfer) (string, error) {
	// Get available versions
	m, err := manifest.Fetch(transfer.Source.Path, common.Verify || transfer.Transfer.Verify)
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

	// Find the file for this version (prefer first pattern that matches)
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
			fmt.Fprintf(os.Stderr, "Warning: failed to update symlink: %v\n", err)
		}
	}

	// Link to /var/lib/extensions for systemd-sysext
	if err := sysext.LinkToSysext(transfer); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to link to sysext: %v\n", err)
	}

	// Refresh systemd-sysext to pick up the new extension (unless --no-refresh)
	if !common.NoRefresh {
		if err := sysext.Refresh(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: sysext refresh failed: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Note: skipping sysext refresh (--no-refresh). Run 'systemd-sysext refresh' manually.\n")
	}

	// Run vacuum
	if err := sysext.Vacuum(transfer); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: vacuum failed: %v\n", err)
	}

	return versionToInstall, nil
}
