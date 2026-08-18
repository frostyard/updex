#!/bin/sh
# Exercise scripts/check-coverage.sh against fixture coverage profiles.
#
# Usage: scripts/test-coverage-check.sh
#
# Builds two synthetic Go coverage profiles in a temporary directory -- one
# whose total statement coverage is above the 80.0% floor and one below it --
# and asserts that check-coverage.sh exits 0 for the first and 1 for the
# second. Exits 0 when both assertions hold.
set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHECK="$SCRIPT_DIR/check-coverage.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# `go tool cover -func` reads absolute file paths from the profile directly, so
# a plain Go source file outside any module is enough to anchor the blocks.
SRC="$TMP/fixture.go"
cat > "$SRC" <<'GO'
package fixture

func covered() int {
	a := 1
	b := 2
	c := 3
	d := 4
	e := 5
	f := 6
	g := 7
	h := 8
	i := 9
	return a + b + c + d + e + f + g + h + i
}
GO

# Ten statements inside covered(): lines 4-13.
# Above the floor: 9 of 10 statements executed (90.0%).
cat > "$TMP/above.out" <<EOF_PROFILE
mode: atomic
$SRC:3.20,13.2 9 1
$SRC:13.2,14.2 1 0
EOF_PROFILE

# Below the floor: 5 of 10 statements executed (50.0%).
cat > "$TMP/below.out" <<EOF_PROFILE
mode: atomic
$SRC:3.20,8.2 5 1
$SRC:8.2,14.2 5 0
EOF_PROFILE

failures=0

echo "--- expect exit 0: coverage above floor"
if "$CHECK" "$TMP/above.out" 80.0; then
    echo "PASS: above-floor profile accepted"
else
    echo "FAIL: above-floor profile rejected (exit $?)"
    failures=$((failures + 1))
fi

echo "--- expect exit 1: coverage below floor"
if "$CHECK" "$TMP/below.out" 80.0; then
    echo "FAIL: below-floor profile accepted"
    failures=$((failures + 1))
else
    status=$?
    if [ "$status" -eq 1 ]; then
        echo "PASS: below-floor profile rejected with exit 1"
    else
        echo "FAIL: below-floor profile rejected with unexpected exit $status"
        failures=$((failures + 1))
    fi
fi

echo "--- expect exit 0: COVERAGE_MIN env override lowers the floor"
if COVERAGE_MIN=40 "$CHECK" "$TMP/below.out"; then
    echo "PASS: COVERAGE_MIN override honoured"
else
    echo "FAIL: COVERAGE_MIN override ignored (exit $?)"
    failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
    echo "test-coverage-check: $failures assertion(s) failed" >&2
    exit 1
fi
echo "test-coverage-check: all assertions passed"
