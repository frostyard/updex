#!/bin/sh
# Exercise scripts/check-coverage.sh against fixture coverage profiles.
#
# Usage: scripts/test-coverage-check.sh
#
# Builds synthetic Go coverage profiles and exercises both the absolute floor
# and the repository-local .coverage-baseline ratchet. Exits 0 when every
# exact-status assertion holds.
set -eu

SCRIPT_DIR="$(CDPATH= cd "$(dirname "$0")" && pwd)"
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

assert_status() {
    expected="$1"
    description="$2"
    shift 2

    echo "--- expect exit $expected: $description"
    set +e
    "$@"
    status=$?
    set -e
    if [ "$status" -eq "$expected" ]; then
        echo "PASS: $description (exit $status)"
    else
        echo "FAIL: $description (exit $status, want $expected)"
        failures=$((failures + 1))
    fi
}

# Copy the checker into isolated repository-shaped roots so each case controls
# whether a baseline exists without touching the real repository baseline.
make_check_root() {
    root="$1"
    mkdir -p "$root/scripts"
    cp "$CHECK" "$root/scripts/check-coverage.sh"
}

NO_BASELINE_ROOT="$TMP/no-baseline"
make_check_root "$NO_BASELINE_ROOT"
assert_status 0 "no baseline keeps the legacy absolute-floor success" \
    "$NO_BASELINE_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0
assert_status 1 "no baseline keeps the legacy absolute-floor failure" \
    "$NO_BASELINE_ROOT/scripts/check-coverage.sh" "$TMP/below.out" 80.0
assert_status 0 "COVERAGE_MIN override works without a baseline" \
    env COVERAGE_MIN=40 "$NO_BASELINE_ROOT/scripts/check-coverage.sh" "$TMP/below.out"

ABOVE_ROOT="$TMP/above-baseline"
make_check_root "$ABOVE_ROOT"
printf '%s\n' 85.0 > "$ABOVE_ROOT/.coverage-baseline"
assert_status 0 "coverage above the baseline is accepted" \
    "$ABOVE_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0

WITHIN_ROOT="$TMP/within-tolerance"
make_check_root "$WITHIN_ROOT"
printf '%s\n' 90.5 > "$WITHIN_ROOT/.coverage-baseline"
assert_status 0 "coverage within 0.5 points below the baseline is accepted" \
    "$WITHIN_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0

RATCHET_ROOT="$TMP/ratchet-failure"
make_check_root "$RATCHET_ROOT"
printf '%s\n' 91.0 > "$RATCHET_ROOT/.coverage-baseline"
assert_status 1 "90.0% is above 80.0% but more than 0.5 points below the baseline" \
    "$RATCHET_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0
assert_status 0 "COVERAGE_TOLERANCE overrides the default tolerance" \
    env COVERAGE_TOLERANCE=1.0 "$RATCHET_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0

LOW_BASELINE_ROOT="$TMP/low-baseline"
make_check_root "$LOW_BASELINE_ROOT"
printf '%s\n' 40.0 > "$LOW_BASELINE_ROOT/.coverage-baseline"
assert_status 1 "the 80.0% absolute floor wins over a low baseline" \
    "$LOW_BASELINE_ROOT/scripts/check-coverage.sh" "$TMP/below.out" 80.0

INVALID_ROOT="$TMP/invalid-baseline"
make_check_root "$INVALID_ROOT"
printf '%s\n' invalid > "$INVALID_ROOT/.coverage-baseline"
assert_status 2 "an unparsable baseline is a usage error" \
    "$INVALID_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0

MULTILINE_ROOT="$TMP/multiline-baseline"
make_check_root "$MULTILINE_ROOT"
printf '%s\n' 90.0 91.0 > "$MULTILINE_ROOT/.coverage-baseline"
assert_status 2 "a multi-line baseline is a usage error" \
    "$MULTILINE_ROOT/scripts/check-coverage.sh" "$TMP/above.out" 80.0

if [ "$failures" -ne 0 ]; then
    echo "test-coverage-check: $failures assertion(s) failed" >&2
    exit 1
fi
echo "test-coverage-check: all assertions passed"
