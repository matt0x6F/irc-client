#!/usr/bin/env bash
set -euo pipefail

# Run inside Dockerfile.jammy with the repository mounted at /src.
: "${GOARCH:?Set GOARCH to amd64 or arm64}"
case "${GOARCH}:$(uname -m)" in
    amd64:x86_64|arm64:aarch64) ;;
    *) echo "GOARCH does not match the builder architecture" >&2; exit 1 ;;
esac

go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.108
wails3 task linux:create:appimage ARCH="$GOARCH"

arch=$(uname -m)
appdir="build/linux/appimage/build/cascade-$arch.AppDir"
image="bin/cascade-$arch.AppImage"
grep -Fxq 'Name=Cascade Chat' "$appdir/cascade.desktop"
bash build/linux/appimage/postprocess.sh "$image" "$appdir"
python3 build/linux/appimage/check_glibc.py "$image" --max-version 2.35
