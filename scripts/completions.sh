#!/bin/sh
set -e
rm -rf completions
mkdir completions
go build -o build/updex ./cmd/updex-cli
for sh in bash zsh fish; do
  ./build/updex completion "$sh" >"completions/updex.$sh"
done