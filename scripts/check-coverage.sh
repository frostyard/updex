#!/bin/sh
# Enforce a total statement-coverage floor on a Go coverage profile.
#
# Usage: scripts/check-coverage.sh [profile] [minimum-percent]
#
#   profile          coverage profile written by `go test -coverprofile`
#                    (default: coverage.out)
#   minimum-percent  required total statement coverage
#                    (default: $COVERAGE_MIN, or 80.0 when unset)
#
# The script runs `go tool cover -func=<profile>`, reads the final `total:`
# line, and exits 1 when the observed value is below the floor. When the
# repository root contains .coverage-baseline, the effective floor is the
# greater of the absolute minimum and the baseline minus
# $COVERAGE_TOLERANCE (default 0.5). Any other failure (missing profile,
# unparsable output) exits 2.
set -eu

PROFILE="${1:-coverage.out}"
MIN="${2:-${COVERAGE_MIN:-80.0}}"
SCRIPT_DIR="$(CDPATH= cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
BASELINE_FILE="$REPO_ROOT/.coverage-baseline"

if [ ! -f "$PROFILE" ]; then
    echo "check-coverage: profile not found: $PROFILE" >&2
    exit 2
fi

if [ -L "$BASELINE_FILE" ] ||
    { [ -e "$BASELINE_FILE" ] &&
        { [ ! -f "$BASELINE_FILE" ] || [ ! -r "$BASELINE_FILE" ]; }; }
then
    echo "check-coverage: baseline is not a readable regular file: $BASELINE_FILE" >&2
    exit 2
fi

case "$MIN" in
    ''|*[!0-9.]*|.|*.*.*)
        echo "check-coverage: invalid minimum percentage: '$MIN'" >&2
        exit 2
        ;;
esac

TOTAL_LINE="$(go tool cover -func="$PROFILE" | awk '$1 == "total:" { line = $0 } END { print line }')"
if [ -z "$TOTAL_LINE" ]; then
    echo "check-coverage: no 'total:' line in 'go tool cover -func=$PROFILE' output" >&2
    exit 2
fi

OBSERVED="$(printf '%s\n' "$TOTAL_LINE" | awk '{ sub(/%$/, "", $NF); print $NF }')"
case "$OBSERVED" in
    ''|*[!0-9.]*)
        echo "check-coverage: could not parse coverage from: $TOTAL_LINE" >&2
        exit 2
        ;;
esac

if [ ! -e "$BASELINE_FILE" ]; then
    echo "check-coverage: observed total statement coverage ${OBSERVED}% (required >= ${MIN}%)"

    # Compute the comparison through command substitution so an awk failure
    # aborts under `set -e` instead of falling through to OK.
    BELOW="$(awk -v observed="$OBSERVED" -v required="$MIN" 'BEGIN { print (observed + 0 < required + 0) ? "yes" : "no" }')"
    case "$BELOW" in
        yes)
            echo "check-coverage: FAIL: total coverage ${OBSERVED}% is below the required floor of ${MIN}%" >&2
            exit 1
            ;;
        no) ;;
        *)
            echo "check-coverage: could not compare ${OBSERVED} with ${MIN}" >&2
            exit 2
            ;;
    esac

    echo "check-coverage: OK"
    exit 0
fi

BASELINE_LINES="$(awk 'END { print NR }' "$BASELINE_FILE")"
BASELINE="$(sed -n '1p' "$BASELINE_FILE")"
BASELINE_VALID=no
case "$BASELINE_LINES:$BASELINE" in
    1:''|1:*[!0-9.]*|1:.|1:*.*.*) ;;
    1:*)
        BASELINE_VALID=yes
        ;;
esac
if [ "$BASELINE_VALID" != yes ]; then
    echo "check-coverage: invalid baseline percentage in $BASELINE_FILE" >&2
    exit 2
fi

TOLERANCE="${COVERAGE_TOLERANCE:-0.5}"
case "$TOLERANCE" in
    ''|*[!0-9.]*|.|*.*.*)
        echo "check-coverage: invalid coverage tolerance: '$TOLERANCE'" >&2
        exit 2
        ;;
esac

RATCHET_FLOOR="$(awk -v baseline="$BASELINE" -v tolerance="$TOLERANCE" 'BEGIN { printf "%.10g\n", baseline - tolerance }')"
EFFECTIVE_FLOOR="$(awk -v minimum="$MIN" -v ratchet="$RATCHET_FLOOR" 'BEGIN { printf "%.10g\n", (minimum > ratchet) ? minimum : ratchet }')"

echo "check-coverage: observed total statement coverage ${OBSERVED}%"
echo "check-coverage: baseline ${BASELINE}%, tolerance ${TOLERANCE} percentage points, effective floor ${EFFECTIVE_FLOOR}%"

BELOW_MINIMUM="$(awk -v observed="$OBSERVED" -v minimum="$MIN" 'BEGIN { print (observed + 0 < minimum + 0) ? "yes" : "no" }')"
if [ "$BELOW_MINIMUM" = yes ]; then
    echo "check-coverage: FAIL: total coverage ${OBSERVED}% violates the absolute minimum bound of ${MIN}%" >&2
    exit 1
fi

BELOW_EFFECTIVE="$(awk -v observed="$OBSERVED" -v effective="$EFFECTIVE_FLOOR" 'BEGIN { print (observed + 0 < effective + 0) ? "yes" : "no" }')"
if [ "$BELOW_EFFECTIVE" = yes ]; then
    echo "check-coverage: FAIL: total coverage ${OBSERVED}% violates the baseline tolerance bound of ${BASELINE}% - ${TOLERANCE} points" >&2
    exit 1
fi

echo "check-coverage: OK"
