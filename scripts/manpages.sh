#!/bin/sh
set -e
rm -rf manpages
mkdir manpages
go run ./cmd/updex-cli man | gzip -c -9 >manpages/updex.1.gz