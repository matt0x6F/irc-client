#!/bin/sh

set -eu

input=${1:-appicon.png}
output=${2:-darwin/icons.icns}
iconset=$(mktemp -d "${TMPDIR:-/tmp}/cascade-icon.XXXXXX").iconset

cleanup() {
  rm -rf "$iconset"
}
trap cleanup EXIT INT TERM

mkdir -p "$iconset"

for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$input" --out "$iconset/icon_${size}x${size}.png" >/dev/null
  double_size=$((size * 2))
  sips -z "$double_size" "$double_size" "$input" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done

iconutil -c icns "$iconset" -o "$output"
