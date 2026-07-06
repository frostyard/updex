# Legacy sysext symlink cleanup order

When removing support for writing `Target.CurrentSymlink`, keep current-version detection before legacy staging-symlink cleanup in the update path. `sysext.GetInstalledVersions` can still use a legacy `CurrentSymlink` to distinguish "newest version is staged but not current" from "already current"; deleting that symlink first makes the newest staged file look current and can skip the required `/var/lib/extensions/<component>.<ext>` relink.

The safe order in `installTransfer` is:

1. Fetch available versions and select the newest candidate.
2. Call `sysext.GetInstalledVersions` while any legacy `CurrentSymlink` still exists.
3. Remove the legacy staging symlink if the transfer configured one.
4. Return early only if the selected version was already both installed and current.
