package updex

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

var (
	catalogRepo        string
	catalogRemoveForce bool
	catalogNoCache     bool
)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Browse and install sysexts from configured catalogs",
		Long: `Browse, install, and remove sysexts published by catalogs such as
fedora-sysexts (https://fedora-sysexts.github.io/).

Catalogs are configured with <name>.catalog INI files searched across:
  - /etc/updex/catalogs.d/
  - /run/updex/catalogs.d/
  - /usr/local/lib/updex/catalogs.d/
  - /usr/lib/updex/catalogs.d/

For example, for the fedora-sysexts "fedora" repo:

  # /etc/updex/catalogs.d/fedora.catalog
  [Catalog]
  SiteURL=https://extensions.fcos.fr/fedora
  ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/

'catalog add' fetches the catalog's published transfer definition and
writes standard .transfer/.feature files into the catalog's own
systemd-sysupdate component directory (/etc/sysupdate.catalog-<name>.d/
by default; set Component= in the .catalog file to override). From then
on the sysext is managed by the normal 'updex features' commands until
'catalog remove' deletes those files again.

Sysexts are referenced as NAME or REPO/NAME (e.g. fedora/zoxide); the
--repo flag is equivalent to the REPO/ prefix. A bare NAME works whenever
it is unambiguous across the configured catalogs.`,
		Example: `  # List everything available from all configured catalogs
  updex catalog list

  # Search for a sysext
  updex catalog search zoxide

  # Install and enable a sysext (downloads immediately)
  sudo updex catalog add fedora/zoxide

  # Remove it again, deleting images and generated files
  sudo updex catalog remove zoxide`,
	}

	cmd.PersistentFlags().StringVar(&catalogRepo, "repo", "", "Limit the operation to a single configured catalog repo")

	cmd.AddCommand(newCatalogListCmd())
	cmd.AddCommand(newCatalogSearchCmd())
	cmd.AddCommand(newCatalogAddCmd())
	cmd.AddCommand(newCatalogRemoveCmd())

	return cmd
}

func newCatalogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sysexts available from configured catalogs",
		Long: `List the sysexts published by every configured catalog repo (or a
single one with --repo), marking those already added and enabled.

Listings are cached locally (default 60 minutes, ~/.cache/updex/) and
revalidated with the catalog afterwards; use --no-cache to force a live
query.

OUTPUT COLUMNS:
  REPO       - Catalog repo publishing the sysext
  NAME       - Sysext name
  INSTALLED  - yes if 'catalog add' has been run for it
  ENABLED    - yes if its feature is currently enabled`,
		Example: `  # List everything
  updex catalog list

  # List one repo in JSON format
  updex catalog list --repo fedora --json

  # Bypass the local cache
  updex catalog list --no-cache`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogList(cmd, "")
		},
	}

	cmd.Flags().BoolVar(&catalogNoCache, "no-cache", false, "Bypass the local listing cache and query the catalog directly")

	return cmd
}

func newCatalogSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search TERM",
		Short: "Search configured catalogs for a sysext",
		Long: `Search the configured catalog repos for sysexts whose name contains TERM.

Uses the same local listing cache as 'catalog list'; --no-cache forces a
live query.`,
		Example: `  # Find anything zoxide-related
  updex catalog search zoxide

  # Search a single repo
  updex catalog search vpn --repo community`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalogList(cmd, args[0])
		},
	}

	cmd.Flags().BoolVar(&catalogNoCache, "no-cache", false, "Bypass the local listing cache and query the catalog directly")

	return cmd
}

func newCatalogAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add [REPO/]NAME",
		Short: "Install a sysext from a catalog",
		Long: `Install a sysext from a configured catalog: fetch its published transfer
definition, write .transfer/.feature files into the catalog's component
directory, enable the feature, and download the image immediately.

Afterwards the sysext is managed by the standard 'updex features'
commands (list, enable, disable, update, check) like any other feature.

Use --dry-run (global flag) to preview changes without modifying the
filesystem.

Requires root privileges.`,
		Example: `  # Add from a specific repo
  sudo updex catalog add fedora/zoxide

  # Bare name works when unambiguous
  sudo updex catalog add zoxide

  # Preview
  updex catalog add zoxide --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: runCatalogAdd,
	}
}

func newCatalogRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [REPO/]NAME",
		Short: "Remove a catalog-added sysext",
		Long: `Remove a sysext previously installed with 'catalog add': disable its
feature, unmerge, delete the downloaded images and /var/lib/extensions
link, and delete the generated .transfer/.feature files.

Like 'features disable --now', removing a currently merged extension
requires --force and a reboot to take effect.

Use --dry-run (global flag) to preview changes without modifying the
filesystem.

Requires root privileges.`,
		Example: `  # Remove a catalog sysext
  sudo updex catalog remove zoxide

  # Force removal while the extension is merged
  sudo updex catalog remove fedora/zoxide --force`,
		Args: cobra.ExactArgs(1),
		RunE: runCatalogRemove,
	}

	cmd.Flags().BoolVar(&catalogRemoveForce, "force", false, "Allow removal of merged extensions (requires reboot)")

	return cmd
}

// splitCatalogArg resolves the [REPO/]NAME argument form against the --repo
// flag. The prefix and the flag may be combined only when they agree.
func splitCatalogArg(arg, repoFlag string) (repo, name string, err error) {
	prefix, rest, found := strings.Cut(arg, "/")
	if !found {
		return repoFlag, arg, nil
	}
	if prefix == "" || rest == "" || strings.Contains(rest, "/") {
		return "", "", fmt.Errorf("invalid sysext reference %q (expected [REPO/]NAME)", arg)
	}
	if repoFlag != "" && repoFlag != prefix {
		return "", "", fmt.Errorf("conflicting repos: %q vs --repo %q", prefix, repoFlag)
	}
	return prefix, rest, nil
}

func runCatalogList(cmd *cobra.Command, search string) error {
	client := newClient()

	entries, err := client.CatalogList(cmd.Context(), updex.CatalogListOptions{
		Repo:    catalogRepo,
		Search:  search,
		NoCache: catalogNoCache,
	})
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err = clix.OutputJSON(entries)
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No sysexts found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "REPO\tNAME\tINSTALLED\tENABLED")
	for _, e := range entries {
		installed, enabled := "no", "no"
		if e.Installed {
			installed = "yes"
		}
		if e.Enabled {
			enabled = "yes"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Name, installed, enabled)
	}
	_ = w.Flush()

	return nil
}

func runCatalogAdd(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	repo, name, err := splitCatalogArg(args[0], catalogRepo)
	if err != nil {
		return err
	}

	client := newClient()

	result, err := client.CatalogAdd(cmd.Context(), name, updex.CatalogAddOptions{
		Repo:      repo,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
	})

	if clix.JSONOutput {
		if result != nil {
			_, _ = clix.OutputJSON(result)
		}
		return err
	}
	if err != nil {
		return err
	}

	if result.DryRun {
		fmt.Printf("[DRY RUN] Would add %s/%s (writing %s and %s), enable it, and download extensions.\n",
			result.Repo, result.Name, result.TransferFile, result.FeatureFile)
		return nil
	}

	fmt.Printf("Added %s/%s and enabled feature '%s'.\n", result.Repo, result.Name, result.Name)
	if result.Enable != nil && len(result.Enable.DownloadedFiles) > 0 {
		fmt.Printf("Downloaded %d extension(s):\n", len(result.Enable.DownloadedFiles))
		for _, f := range result.Enable.DownloadedFiles {
			fmt.Printf("  - %s\n", f)
		}
	}

	return nil
}

func runCatalogRemove(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	repo, name, err := splitCatalogArg(args[0], catalogRepo)
	if err != nil {
		return err
	}

	client := newClient()

	result, err := client.CatalogRemove(cmd.Context(), name, updex.CatalogRemoveOptions{
		Repo:      repo,
		Force:     catalogRemoveForce,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
	})

	if clix.JSONOutput {
		if result != nil {
			_, _ = clix.OutputJSON(result)
		}
		return err
	}
	if err != nil {
		return err
	}

	if result.DryRun {
		fmt.Printf("[DRY RUN] Would remove %s/%s, its extension files, and generated definitions.\n",
			result.Repo, result.Name)
		return nil
	}

	fmt.Printf("Removed %s/%s.\n", result.Repo, result.Name)
	if len(result.RemovedFiles) > 0 {
		fmt.Printf("Deleted %d definition file(s):\n", len(result.RemovedFiles))
		for _, f := range result.RemovedFiles {
			fmt.Printf("  - %s\n", f)
		}
	}
	if catalogRemoveForce {
		fmt.Printf("Warning: Reboot required for changes to take effect.\n")
	}

	return nil
}
