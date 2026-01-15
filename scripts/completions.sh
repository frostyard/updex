#!/bin/sh
set -e
rm -rf completions
mkdir completions
go build -o build/updex ./updex
for sh in bash zsh fish; do
  ./build/updex completion "$sh" >"completions/updex.$sh"
done

go build -o build/instex ./instex
for sh in bash zsh fish; do
  ./build/instex completion "$sh" >"completions/instex.$sh"
done