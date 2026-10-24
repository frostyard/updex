// Package imagecompat decides whether a downloaded sysext image may be
// linked on a host that runs an older base image (ADR-0016, "Minimum image
// version").
//
// It has three parts:
//
//   - ReadExtensionRelease reads the extension-release file from inside a
//     staged sysext image without mounting it. The image is untrusted input:
//     mounting it (mount, systemd-dissect, a loop device) would hand it to
//     the kernel's filesystem code as root before updex decides whether to
//     link it, and the auto-update service runs with RestrictNamespaces=yes.
//     The reader parses the GPT and an uncompressed EROFS filesystem in
//     pure Go.
//     go-erofs refuses a compressed filesystem, so such an image is read
//     with the erofs-utils "dump.erofs" program instead, through the
//     injectable ErofsDumpRunner; a host without erofs-utils >= 1.8.5 fails
//     closed (ErrErofsToolUnavailable), so the check is never skipped.
//   - NextBootImageVersion returns the IMAGE_VERSION of the image that boots
//     next: the rollback deployment when bootc queues one, else the staged
//     deployment, else the booted os-release.
//   - CheckMinImageVersion compares the UPDEX_MIN_IMAGE_VERSION key of the
//     extension-release file with that version.
//
// The package does not import the CLI or the updex SDK packages.
package imagecompat
