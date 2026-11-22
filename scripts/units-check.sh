#!/bin/sh
# Verify the packaged systemd units without root, including warning failures
# (updex#536; core baseline row B19).
set -eu

if ! command -v systemd-analyze >/dev/null 2>&1; then
    echo 'units-check: systemd-analyze is not installed; install systemd (Debian/Ubuntu: apt-get install systemd)' >&2
    exit 1
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -f "$1" ] || [ ! -x "$1" ]; then
    echo 'usage: scripts/units-check.sh BINARY [UNITS_DIR]' >&2
    exit 2
fi
binary=$1
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if [ "$#" -eq 2 ]; then
    units=$2
else
    units=$tmp/units
    ./scripts/units.sh "$units"
fi
# Include hidden entries when enforcing the exact fixture/package file set.
count=0
for path in "$units"/* "$units"/.[!.]* "$units"/..?*; do
    if [ ! -e "$path" ] && [ ! -L "$path" ]; then continue; fi
    case ${path##*/} in
        updex-update.service|updex-update.timer|updex-suite-switch.service)
            if [ -f "$path" ]; then
                count=$((count + 1))
                continue
            fi
            ;;
    esac
    echo "units-check: unexpected unit entry: $path" >&2
    exit 1
done
if [ "$count" -ne 3 ]; then
    echo 'units-check: expected exactly updex-update.service, updex-update.timer, updex-suite-switch.service' >&2
    exit 1
fi
dest=$tmp/root/usr/lib/systemd/system
mkdir -p "$dest" "$tmp/root/usr/bin"
# The units keep systemd's default dependencies (sysinit.target, basic.target,
# shutdown.target, system.slice, ...). systemd-analyze verify --root resolves
# them inside the root, and some systemd releases fail with "Unit sysinit.target
# not found" when the root has none, so the host's targets and slices go in too
# (not its services: their absolute links point outside the root).
host_units=/usr/lib/systemd/system
if [ ! -f "$host_units/sysinit.target" ]; then
    echo "units-check: $host_units/sysinit.target is missing; install systemd (Debian/Ubuntu: apt-get install systemd)" >&2
    exit 1
fi
for path in "$host_units"/*.target "$host_units"/*.slice; do
    if [ -e "$path" ] || [ -L "$path" ]; then cp -P "$path" "$dest/"; fi
done
for name in updex-update.service updex-update.timer updex-suite-switch.service; do
    cp "$units/$name" "$dest/$name"
done
install -m 0755 "$binary" "$tmp/root/usr/bin/updex"
status=0
output=$(systemd-analyze verify --root="$tmp/root" \
    "$dest/updex-update.service" "$dest/updex-update.timer" \
    "$dest/updex-suite-switch.service" 2>&1) || status=$?
printf '%s\n' "$output"
if [ "$status" -ne 0 ]; then
    echo "units-check: systemd-analyze verify failed with exit code $status" >&2
    exit 1
fi
if printf '%s\n' "$output" | grep -q 'updex-'; then
    echo 'units-check: systemd-analyze verify reported a problem with an updex unit' >&2
    exit 1
fi
echo 'units-check: ok (3 units)'
