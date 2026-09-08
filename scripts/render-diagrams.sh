#!/bin/sh
set -eu

for source in docs/diagrams/*.mmd; do
  target=${source%.mmd}.svg
  npx --yes @mermaid-js/mermaid-cli@11.9.0 -i "$source" -o "$target" -b transparent
done
