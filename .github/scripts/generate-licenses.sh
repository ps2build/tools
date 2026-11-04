#!/bin/sh
# Regenerates licenses/tools/ from the compiled-in Go dependency graph.
# Run from the repo root. Requires: go install github.com/google/go-licenses@v1.6.0
set -e

if ! command -v go-licenses >/dev/null 2>&1; then
  echo "go-licenses not found; install it with:" >&2
  echo "  go install github.com/google/go-licenses@v1.6.0" >&2
  exit 1
fi

OUT="licenses/tools"
rm -rf "$OUT"
mkdir -p "$OUT"
cp LICENSE "$OUT/LICENSE"

# The dependency graph differs per GOOS, so the tree is the union of linux and windows.
tmp_linux="$(mktemp -d)"
tmp_windows="$(mktemp -d)"
trap 'rm -rf "$tmp_linux" "$tmp_windows"' EXIT
GOOS=linux GOARCH=amd64 go-licenses save ./cmd/... --save_path="$tmp_linux" --force
GOOS=windows GOARCH=amd64 go-licenses save ./cmd/... --save_path="$tmp_windows" --force
rm -rf "$tmp_linux/github.com/ps2build" "$tmp_windows/github.com/ps2build"
mkdir -p "$OUT/third-party"
cp -r "$tmp_linux"/. "$OUT/third-party/"
chmod -R u+w "$OUT/third-party"
cp -r "$tmp_windows"/. "$OUT/third-party/"
chmod -R u+w "$OUT/third-party"
