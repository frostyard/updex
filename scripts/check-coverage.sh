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
# line, prints the observed and required percentages, and exits 1 when the
# observed value is below the floor. Any other failure (missing profile,
# unparsable output) also exits non-zero.
set -eu

PROFILE="${1:-coverage.out}"
MIN="${2:-${COVERAGE_MIN:-80.0}}"

if [ ! -f "$PROFILE" ]; then
    echo "check-coverage: profile not found: $PROFILE" >&2
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

echo "check-coverage: observed total statement coverage ${OBSERVED}% (required >= ${MIN}%)"

if awk -v observed="$OBSERVED" -v required="$MIN" 'BEGIN { exit !(observed + 0 < required + 0) }'; then
    echo "check-coverage: FAIL: total coverage ${OBSERVED}% is below the required floor of ${MIN}%" >&2
    exit 1
fi

echo "check-coverage: OK"
