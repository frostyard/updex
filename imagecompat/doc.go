// Package imagecompat decides whether a downloaded sysext image may be
// linked on a host that runs an older base image (ADR-0016, "Minimum image
// version").
//
// It has three parts:
//
//   - ReadExtensionRelease reads the extension-release file from inside a
//     staged sysext image without mounting it. The auto-update service runs
//     with RestrictNamespaces=yes and SystemCallFilter=@system-service, so
//     it cannot mount, call systemd-dissect, or set up a loop device. The
//     reader parses the GPT and the EROFS filesystem in pure Go instead.
//   - NextBootImageVersion returns the IMAGE_VERSION of the image that boots
//     next: the staged bootc deployment, else the booted os-release.
//   - CheckMinImageVersion compares the UPDEX_MIN_IMAGE_VERSION key of the
//     extension-release file with that version.
//
// The package does not import the CLI or the updex SDK packages.
package imagecompat
