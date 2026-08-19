package updex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/manifest"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/version"
)

// Features returns all configured features with their status.
//
// opts is variadic for backward compatibility: only opts[0] is used, if
// provided; additional elements are ignored. Callers with no options may
// omit it entirely.
func (c *Client) Features(ctx context.Context, opts ...FeaturesOptions) ([]FeatureInfo, error) {
	var opt FeaturesOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	c.msg("Loading configurations")

	features, transfers, err := c.loadDomain(opt.Component)
	if err != nil {
		return nil, err
	}

	if len(features) == 0 {
		c.msg("No features configured")
		return []FeatureInfo{}, nil
	}

	var featureInfos []FeatureInfo

	// Resolved once: os-release does not change between features.
	imageName := config.ImageNameFrom(c.paths.osReleasePaths)

	for _, f := range features {
		// Get transfers associated with this feature
		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		var transferNames []string
		for _, t := range featureTransfers {
			transferNames = append(transferNames, t.Component)
		}

		origin, originName := featureOrigin(f.FilePath, imageName, c.paths.definitionRoots, c.config.Definitions != "")

		info := FeatureInfo{
			Name:          f.Name,
			Description:   f.Description,
			Documentation: f.Documentation,
			Enabled:       f.Enabled,
			Masked:        f.Masked,
			Source:        f.FilePath,
			Origin:        origin,
			OriginName:    originName,
			Transfers:     transferNames,
		}
		featureInfos = append(featureInfos, info)
	}

	c.msg("Found %d feature(s)", len(featureInfos))

	return featureInfos, nil
}

// featureOrigin classifies where a .feature file came from: a
// catalog-generated file names its catalog in the marker header (see
// catalog.GeneratedFileRepo), and everything else is identified by the
// search root it lives under. imageName is passed in rather than resolved
// here so a listing reads os-release once. definitionRoots are the roots
// captured at client construction — never the package-level SearchRoots.
//
// definitionsOverride reports whether the whole domain came from a
// -C/--definitions directory. That directory may itself sit under a
// search root (e.g. -C /etc/my-defs), where path containment alone would
// wrongly claim the file as local:etc or even image — the file was not
// discovered through the search paths at all, so the root it happens to
// live under says nothing about its provenance. The marker still wins,
// because it is a fact recorded in the file rather than an inference
// from its location.
func featureOrigin(path, imageName string, definitionRoots []string, definitionsOverride bool) (origin, originName string) {
	if repo, ok := catalog.GeneratedFileRepo(path); ok {
		return FeatureOriginCatalog, repo
	}
	if definitionsOverride {
		return FeatureOriginUnknown, ""
	}

	// Index order matches the definition roots order: /etc, /run,
	// /usr/local/lib, /usr/lib.
	switch idx, ok := config.SearchRootIndexIn(path, definitionRoots); {
	case !ok:
		return FeatureOriginUnknown, ""
	case idx == 0:
		return FeatureOriginLocal, "etc"
	case idx == 1:
		return FeatureOriginLocal, "run"
	case idx == 2:
		return FeatureOriginLocal, "usr"
	default:
		return FeatureOriginImage, imageName
	}
}

// updexDropInName is the feature drop-in file updex owns. Everything else
// in a <feature>.feature.d directory belongs to the administrator and must
// be left alone (see CatalogRemove).
const updexDropInName = "00-updex.conf"

// lookupFeature returns the feature matching name from an already-loaded
// feature set. It returns an error if the feature is not found or is
// masked. The action parameter (e.g. "enabled", "disabled") is used in the
// masked error message.
func lookupFeature(features []*config.Feature, name, action string) (*config.Feature, error) {
	for _, f := range features {
		if f.Name == name {
			if f.Masked {
				return nil, fmt.Errorf("feature '%s' is masked and cannot be %s", name, action)
			}
			return f, nil
		}
	}

	return nil, fmt.Errorf("feature '%s' not found", name)
}

// writeFeatureDropIn creates a drop-in configuration file that sets a
// feature's enabled state. The drop-in is written under the same
// systemd-sysupdate component scope the feature file itself was discovered
// under (see config.ComponentOfPath): component-scoped features get
// /etc/sysupdate.<name>.d/..., legacy default and --definitions-loaded
// features keep the legacy /etc/sysupdate.d/... path. In dry-run mode it
// only logs what would happen and returns the path without writing
// anything.
func (c *Client) writeFeatureDropIn(f *config.Feature, enabled bool, dryRun bool) (string, error) {
	component, _ := config.ComponentOfPath(f.FilePath) // "" for the legacy default or a --definitions override
	dropInDir := filepath.Join(config.EtcComponentDirIn(component, c.paths.definitionRoots), f.Name+".feature.d")
	dropInFile := filepath.Join(dropInDir, updexDropInName)

	if dryRun {
		c.msg("Would create drop-in: %s", dropInFile)
		return dropInFile, nil
	}

	// ADR-0005: this is a root write under a managed definition directory,
	// so both the drop-in directory and the drop-in itself are checked with
	// Lstat (never Stat) and refused when anything non-regular is present —
	// a symlink at the directory path, dangling or live, would otherwise be
	// followed by MkdirAll and the write below it would land wherever the
	// link points; a symlink at the file path is managedFileExists' case.
	// A stat failure other than absence is an error too, so nothing
	// unreadable is ever misread as absent. The write itself then goes
	// through a temp file plus rename so it never follows a link planted
	// between check and write.
	switch info, err := os.Lstat(dropInDir); {
	case err == nil && info.IsDir():
		// present and real: nothing to create
	case err == nil:
		return "", fmt.Errorf("drop-in directory %s exists and is not a directory (mode %s); remove it manually", dropInDir, info.Mode().Type())
	case os.IsNotExist(err):
		if err := os.MkdirAll(dropInDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create drop-in directory: %w", err)
		}
	default:
		return "", fmt.Errorf("failed to check drop-in directory: %w", err)
	}
	if _, err := managedFileExists(dropInFile); err != nil {
		return "", fmt.Errorf("failed to check drop-in file: %w", err)
	}

	content := fmt.Sprintf("[Feature]\nEnabled=%v\n", enabled)
	if err := writeManagedFile(dropInFile, content); err != nil {
		return "", fmt.Errorf("failed to write drop-in file: %w", err)
	}

	c.msg("Created drop-in: %s", dropInFile)
	return dropInFile, nil
}

// EnableFeature enables a feature by creating a drop-in configuration file.
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
	c.msg("Enabling %s", name)

	result := &FeatureActionResult{
		Feature: name,
		Action:  "enable",
		DryRun:  opts.DryRun,
	}

	features, transfers, err := c.loadDomain(opts.Component)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// Verify the feature exists and is not masked
	f, err := lookupFeature(features, name, "enabled")
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// Create drop-in directory and file
	dropInFile, err := c.writeFeatureDropIn(f, true, opts.DryRun)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}
	if !opts.DryRun {
		result.DropIn = dropInFile
	}

	// Handle --now flag: download extensions immediately
	if opts.Now {
		c.msg("Downloading extensions")

		featureTransfers := config.GetTransfersForFeature(transfers, name)

		if len(featureTransfers) == 0 {
			c.msg("No transfers associated with this feature")
		} else {
			for _, transfer := range featureTransfers {
				c.msg("Processing %s", transfer.Component)

				if opts.DryRun {
					c.msg("Would download: %s", transfer.Component)
					result.DownloadedFiles = append(result.DownloadedFiles, transfer.Component+" (would download)")
				} else {
					// Use installTransfer which handles all the download logic
					version, _, downloaded, err := c.installTransfer(ctx, transfer, installTransferOptions{
						NoRefresh: true, // refresh is batched at the end
					})
					if err != nil {
						err = fmt.Errorf("failed to download %s: %w", transfer.Component, err)
						result.Error = err.Error()
						c.warn("%s", result.Error)
						return result, err
					}
					if downloaded {
						result.DownloadedFiles = append(result.DownloadedFiles, fmt.Sprintf("%s@%s", transfer.Component, version))
						c.msg("Downloaded %s version %s", transfer.Component, version)
					} else {
						c.msg("Version %s already installed and current for %s", version, transfer.Component)
					}
				}
			}

			// Refresh if we downloaded (unless --no-refresh or --dry-run)
			if !opts.NoRefresh && !opts.DryRun {
				c.msg("Refreshing sysext")
				if err := c.runner.Refresh(); err != nil {
					// The drop-in is written and the images are staged and
					// linked; only activation failed. Report it rather than
					// claiming success (see FeatureActionResult.RefreshError).
					err = fmt.Errorf("sysext refresh failed: %w", err)
					c.warn("%s", err)
					result.RefreshError = err.Error()
					result.Error = err.Error()
					result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled and %d extension(s) downloaded, but systemd-sysext refresh failed; run 'systemd-sysext refresh' (or reboot) to activate them", name, len(result.DownloadedFiles))
					return result, err
				}
			}
		}
	}

	result.Success = true

	// Set appropriate NextActionMessage
	if opts.DryRun {
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would enable feature '%s'", name)
		if opts.Now {
			result.NextActionMessage += " and download extensions"
		}
	} else if opts.Now && len(result.DownloadedFiles) > 0 {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled and %d extension(s) downloaded", name, len(result.DownloadedFiles))
	} else {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled. Run 'updex features update' to download extensions.", name)
	}

	return result, nil
}

// DisableFeature disables a feature by creating a drop-in configuration file.
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error) {
	c.msg("Disabling %s", name)

	result := &FeatureActionResult{
		Feature: name,
		Action:  "disable",
		DryRun:  opts.DryRun,
	}

	features, transfers, err := c.loadDomain(opts.Component)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// Verify the feature exists and is not masked
	f, err := lookupFeature(features, name, "disabled")
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// The transfers `--now` may remove: members of this feature that no
	// remaining enabled feature still activates once it is disabled. A
	// transfer another enabled feature also lists (Features=alpha beta) keeps
	// its staged image and sysext link; the same set drives the merge-state
	// check and RemovedFiles, so nothing still active is ever inspected or
	// deleted for this disable.
	featureTransfers := config.TransfersReleasedByDisabling(transfers, features, name)
	var retainedTransfers []string
	for _, t := range transfers {
		if !slices.Contains(t.Transfer.Features, name) && !slices.Contains(t.Transfer.RequisiteFeatures, name) {
			continue
		}
		if !slices.ContainsFunc(featureTransfers, func(released *config.Transfer) bool { return released == t }) {
			retainedTransfers = append(retainedTransfers, t.Component)
		}
	}
	retainedNote := ""
	if len(retainedTransfers) > 0 {
		retainedNote = fmt.Sprintf(" Kept %s: still activated by another enabled feature.", strings.Join(retainedTransfers, ", "))
	}

	willRemoveFiles := opts.Now

	// Check merge state BEFORE any destructive operations
	if willRemoveFiles && len(featureTransfers) > 0 {
		var mergedExtensions []string
		for _, t := range featureTransfers {
			activeVersion, err := sysext.GetActiveVersionIn(
				t,
				c.paths.sysextLinkDir,
				c.paths.runExtensionsDir,
			)
			if err != nil {
				c.warn("could not check merge state for %s: %v", t.Component, err)
				continue
			}
			if activeVersion != "" {
				mergedExtensions = append(mergedExtensions, fmt.Sprintf("%s (version %s)", t.Component, activeVersion))
			}
		}

		if len(mergedExtensions) > 0 && !opts.Force {
			var errMsg string
			if len(mergedExtensions) == 1 {
				errMsg = fmt.Sprintf("Extension %s is active. Removing requires --force and a reboot to take effect.", mergedExtensions[0])
			} else {
				errMsg = fmt.Sprintf("Extensions are active: %v. Removing requires --force and a reboot to take effect.", mergedExtensions)
			}
			result.Error = errMsg
			c.warn("%s", errMsg)
			return result, errors.New(errMsg)
		}

		if len(mergedExtensions) > 0 && opts.Force {
			c.warn("Extensions are currently active. Changes will take effect after reboot.")
		}
	}

	// Create drop-in directory and file
	dropInFile, err := c.writeFeatureDropIn(f, false, opts.DryRun)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}
	if !opts.DryRun {
		result.DropIn = dropInFile
	}

	// Handle --now (or --remove for backward compat): remove files and unmerge
	if willRemoveFiles && len(featureTransfers) > 0 {
		// If --now is specified, unmerge first (unless dry-run)
		if opts.Now && !opts.DryRun {
			c.msg("Unmerging extensions")
			if err := c.runner.Unmerge(); err != nil {
				err = fmt.Errorf("failed to unmerge: %w", err)
				result.Error = err.Error()
				c.warn("%s", result.Error)
				return result, err
			}
			result.Unmerged = true
		} else if opts.Now && opts.DryRun {
			c.msg("Would unmerge extensions")
		}

		// Remove files for each transfer
		c.msg("Removing files")
		var allRemoved []string
		for _, t := range featureTransfers {
			if opts.DryRun {
				c.msg("Would remove files for: %s", t.Component)
				allRemoved = append(allRemoved, t.Component+" (would remove)")
			} else {
				// Remove the symlink from /var/lib/extensions
				if err := sysext.UnlinkFromSysextAt(t, c.paths.sysextLinkDir); err != nil {
					c.warn("failed to unlink %s: %v", t.Component, err)
				}

				// Remove all versions
				removed, err := sysext.RemoveAllVersionsAt(t, c.paths.sysextLinkDir)
				if err != nil {
					err = fmt.Errorf("failed to remove files for %s: %w", t.Component, err)
					result.Error = err.Error()
					c.warn("%s", result.Error)
					return result, err
				}
				allRemoved = append(allRemoved, removed...)
			}
		}
		result.RemovedFiles = allRemoved
		if !opts.DryRun {
			c.msg("Removed %d file(s)", len(allRemoved))
		}

		// Refresh if we unmerged (unless --no-refresh or --dry-run)
		if opts.Now && !opts.NoRefresh && !opts.DryRun {
			c.msg("Refreshing sysext")
			if err := c.runner.Refresh(); err != nil {
				// Unmerge already detached every extension on the host and
				// the files are gone; a failed refresh leaves the remaining
				// extensions unmerged. That must never read as success.
				err = fmt.Errorf("sysext refresh failed: %w", err)
				c.warn("%s", err)
				result.RefreshError = err.Error()
				result.Error = err.Error()
				result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled and %d extension file(s) removed, but systemd-sysext refresh failed after unmerge: all extensions are currently unmerged; run 'systemd-sysext refresh' (or reboot) to re-merge the remaining extensions", name, len(result.RemovedFiles))
				return result, err
			}
		}
	}

	result.Success = true

	// Set the next action message based on what was done
	if opts.DryRun {
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would disable feature '%s'", name)
		if willRemoveFiles {
			result.NextActionMessage += " and remove extension files"
		}
	} else if willRemoveFiles && opts.Force {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled and files removed. Reboot required for changes to take effect.%s", name, retainedNote)
	} else if willRemoveFiles {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled and %d extension file(s) removed.%s", name, len(result.RemovedFiles), retainedNote)
	} else {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled. Run 'updex features update' to apply changes.", name)
	}

	return result, nil
}

// UpdateFeatures downloads and installs new versions for all enabled features.
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error) {
	features, transfers, err := c.loadDomain(opts.Component)
	if err != nil {
		return nil, err
	}

	// Initialize as a non-nil slice so empty results serialize as JSON `[]`
	// rather than `null`. Consumers (pilothouse, snosi scripts) that parse
	// the CLI's `--json` output depend on an array being present.
	allResults := make([]UpdateFeaturesResult, 0)
	var hasErrors bool

	// Cache manifests by source URL to avoid redundant HTTP requests
	// when multiple transfers share the same source. getAvailableVersions
	// refetches (with verification) when a Verify=true transfer meets an
	// unverified entry; the verified manifest then replaces it here.
	manifestCache := make(map[string]*manifest.Manifest)

	for _, f := range features {
		if !f.Enabled || f.Masked {
			continue
		}

		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		if len(featureTransfers) == 0 {
			continue
		}

		featureResult := UpdateFeaturesResult{
			Feature: f.Name,
			// Non-nil so a feature with no per-transfer result serializes
			// its `results` as `[]` rather than `null`.
			Results: make([]UpdateResult, 0),
		}

		for _, transfer := range featureTransfers {
			c.msg("Processing %s/%s", f.Name, transfer.Component)

			result := UpdateResult{
				Component: transfer.Component,
				DryRun:    opts.DryRun,
			}

			v, m, downloaded, err := c.installTransfer(ctx, transfer, installTransferOptions{
				DryRun:         opts.DryRun,
				NoVacuum:       opts.NoVacuum,
				NoRefresh:      true, // refresh is batched at the end
				CachedManifest: manifestCache[transfer.Source.Path],
			})
			if m != nil {
				manifestCache[transfer.Source.Path] = m
			}
			if err != nil {
				result.Error = err.Error()
				c.warn("%s", result.Error)
				featureResult.Results = append(featureResult.Results, result)
				hasErrors = true
				continue
			}

			result.Version = v
			if downloaded {
				result.Downloaded = true
				if opts.DryRun {
					result.NextActionMessage = "Would download and install version " + v
					if !opts.NoVacuum {
						removed, _, err := sysext.PlanVacuumAfterInstallAt(transfer, v, c.paths.sysextLinkDir)
						if err != nil {
							c.warn("failed to plan vacuum for %s: %v", transfer.Component, err)
						}
						result.RemovedVersions = removed
						if len(removed) > 0 {
							result.NextActionMessage += fmt.Sprintf("; would remove old versions: %v", removed)
						}
					}
					c.msg("Would install version %s", v)
				} else {
					result.Installed = true
					result.NextActionMessage = "Reboot required to activate changes"
					c.msg("Installed version %s", v)
				}
			} else {
				result.Installed = true
				c.msg("Version %s already installed and current", v)
			}

			featureResult.Results = append(featureResult.Results, result)
		}

		allResults = append(allResults, featureResult)
	}

	var refreshErr error
	if opts.DryRun {
		c.msg("Dry run: skipping sysext refresh")
	} else if !opts.NoRefresh {
		if err := c.runner.Refresh(); err != nil {
			// Installs and links are on disk but not activated. Results stay
			// populated (Installed=true is accurate); the error tells the
			// caller activation did not happen and the CLI exits non-zero.
			refreshErr = fmt.Errorf("sysext refresh failed: %w", err)
			c.warn("%s", refreshErr)
		}
	} else {
		c.msg("Skipping sysext refresh (--no-refresh)")
	}

	if hasErrors {
		return allResults, errors.Join(fmt.Errorf("one or more components failed to update"), refreshErr)
	}
	return allResults, refreshErr
}

// CheckFeatures checks if newer versions are available for all enabled features.
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error) {
	features, transfers, err := c.loadDomain(opts.Component)
	if err != nil {
		return nil, err
	}

	// Initialize as a non-nil slice so empty results serialize as JSON `[]`
	// rather than `null` (see UpdateFeatures for the same rationale).
	allResults := make([]CheckFeaturesResult, 0)

	// Cache manifests by source URL to avoid redundant HTTP requests
	// when multiple transfers share the same source. getAvailableVersions
	// refetches (with verification) when a Verify=true transfer meets an
	// unverified entry; the verified manifest then replaces it here.
	manifestCache := make(map[string]*manifest.Manifest)
	var hasErrors bool

	for _, f := range features {
		if !f.Enabled || f.Masked {
			continue
		}

		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		if len(featureTransfers) == 0 {
			continue
		}

		featureResult := CheckFeaturesResult{
			Feature: f.Name,
			// Non-nil so a feature with no per-transfer result serializes
			// its `results` as `[]` rather than `null`.
			Results: make([]CheckResult, 0),
		}

		for _, transfer := range featureTransfers {
			c.msg("Checking %s/%s", f.Name, transfer.Component)

			available, m, _, err := c.getAvailableVersions(ctx, transfer, manifestCache[transfer.Source.Path])
			if m != nil {
				manifestCache[transfer.Source.Path] = m
			}
			if err != nil {
				// A component that cannot be checked is reported as such
				// rather than dropped: consumers must be able to tell
				// "could not check" from "no update" (mirrors UpdateFeatures).
				err = fmt.Errorf("failed to get available versions: %w", err)
				c.warn("%s", err)
				featureResult.Results = append(featureResult.Results, CheckResult{
					Component: transfer.Component,
					Error:     err.Error(),
				})
				hasErrors = true
				continue
			}

			if len(available) == 0 {
				continue
			}

			version.Sort(available)
			newest := available[0]

			installed, current, err := sysext.GetInstalledVersionsAt(transfer, c.paths.sysextLinkDir)
			if err != nil {
				// Without the installed set the comparison would report a
				// spurious "update available"; report the failure instead.
				err = fmt.Errorf("failed to get installed versions: %w", err)
				c.warn("%s", err)
				featureResult.Results = append(featureResult.Results, CheckResult{
					Component:     transfer.Component,
					NewestVersion: newest,
					Error:         err.Error(),
				})
				hasErrors = true
				continue
			}

			result := CheckResult{
				Component:      transfer.Component,
				CurrentVersion: current,
				NewestVersion:  newest,
			}

			if len(installed) == 0 {
				result.UpdateAvailable = true
				c.msg("New version available: %s", newest)
			} else if version.Compare(newest, current) > 0 {
				result.UpdateAvailable = true
				c.msg("Update available: %s → %s", current, newest)
			} else {
				c.msg("Up to date: %s", current)
			}

			featureResult.Results = append(featureResult.Results, result)
		}

		allResults = append(allResults, featureResult)
	}

	if hasErrors {
		return allResults, fmt.Errorf("one or more components failed to check")
	}
	return allResults, nil
}
