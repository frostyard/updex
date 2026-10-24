#!/bin/sh
# Writes updex-update.timer and updex-update.service, the units that the
# frostyard-updex package installs under /usr/lib/systemd/system. GoReleaser
# runs it as a before-hook with no argument (output goes to ./units, which is
# gitignored like completions/ and manpages/); the units contract test passes
# a temporary directory.
set -e
out="${1:-units}"
rm -rf "$out"
mkdir -p "$out"
go run ./cmd/updex-cli units --output-dir "$out"
