#!/bin/sh
# Exercise units-check with generated units and invalid fixtures (updex#536).
set -eu
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
./scripts/units.sh "$tmp/units"
# Verification checks executability, not the binary's behavior.
cp /bin/true "$tmp/updex"
./scripts/units-check.sh "$tmp/updex" "$tmp/units"
cp "$tmp/units/updex-update.service" "$tmp/good.service"
printf '\n[Service]\nBogusKey=1\n' >> "$tmp/units/updex-update.service"
if ./scripts/units-check.sh "$tmp/updex" "$tmp/units" > "$tmp/output" 2>&1; then
    echo 'test-units-check: unknown key passed' >&2
    exit 1
fi
cat "$tmp/output"
grep -q 'units-check: systemd-analyze verify reported a problem with an updex unit' "$tmp/output"
cp "$tmp/good.service" "$tmp/units/updex-update.service"
rm "$tmp/units/updex-update.timer"
if ./scripts/units-check.sh "$tmp/updex" "$tmp/units" > "$tmp/output" 2>&1; then
    echo 'test-units-check: missing unit passed' >&2
    exit 1
fi
cat "$tmp/output"
grep -q 'units-check: expected exactly' "$tmp/output"
echo 'test-units-check: ok (good units, unknown key, missing unit)'
