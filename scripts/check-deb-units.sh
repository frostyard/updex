#!/bin/sh
# Fails unless the given .deb lists both update units under
# /usr/lib/systemd/system as regular files with mode 0644. The snapshot
# workflow runs it on the amd64 package that GoReleaser builds.
#   usage: scripts/check-deb-units.sh PACKAGE.deb
set -eu

if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "usage: $0 PACKAGE.deb (a single, existing file)" >&2
  exit 2
fi

listing="$(dpkg-deb -c "$1")"
status=0
for unit in updex-update.timer updex-update.service; do
  if printf '%s\n' "$listing" | grep -Eq "^-rw-r--r--[[:space:]].*[[:space:]]\./usr/lib/systemd/system/$unit\$"; then
    echo "ok: $unit is in $1"
  else
    echo "error: $1 does not list /usr/lib/systemd/system/$unit as a 0644 file" >&2
    status=1
  fi
done
exit "$status"
