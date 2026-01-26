#!/bin/bash
# Test shell completion script generation for updex
# Usage: ./scripts/test-completions.sh [path-to-updex-binary]

set -e

UPDEX="${1:-./bin/updex}"

if [ ! -x "$UPDEX" ]; then
    echo "Error: updex binary not found at $UPDEX"
    echo "Build first: go build -o ./bin/updex ./cmd/updex-cli/"
    exit 1
fi

echo "Testing shell completions with: $UPDEX"
echo

# Test bash completion
echo "=== Bash Completion ==="
echo -n "Generating... "
$UPDEX completion bash > /tmp/updex_completion.bash
echo "OK"

echo -n "Validating syntax... "
bash -n /tmp/updex_completion.bash
echo "OK"

echo -n "Checking for _updex function... "
grep -q "_updex" /tmp/updex_completion.bash
echo "OK"

echo -n "Checking for subcommands... "
grep -q "update" /tmp/updex_completion.bash && \
grep -q "install" /tmp/updex_completion.bash && \
grep -q "remove" /tmp/updex_completion.bash && \
grep -q "list" /tmp/updex_completion.bash
echo "OK"

echo

# Test zsh completion
echo "=== Zsh Completion ==="
echo -n "Generating... "
$UPDEX completion zsh > /tmp/_updex
echo "OK"

echo -n "Checking for compdef... "
grep -q "compdef" /tmp/_updex
echo "OK"

echo -n "Checking for _updex function... "
grep -q "_updex" /tmp/_updex
echo "OK"

echo

# Test fish completion
echo "=== Fish Completion ==="
echo -n "Generating... "
$UPDEX completion fish > /tmp/updex.fish
echo "OK"

echo -n "Checking for complete command... "
grep -q "complete" /tmp/updex.fish
echo "OK"

echo -n "Checking for subcommands... "
grep -q "update" /tmp/updex.fish && \
grep -q "install" /tmp/updex.fish && \
grep -q "remove" /tmp/updex.fish
echo "OK"

echo
echo "All completion tests passed!"
