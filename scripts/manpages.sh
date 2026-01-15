#!/bin/sh
set -e
rm -rf manpages
mkdir manpages
go run ./updex man | gzip -c -9 >manpages/updex.1.gz
go run ./instex man | gzip -c -9 >manpages/instex.1.gz