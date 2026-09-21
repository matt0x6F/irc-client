#!/usr/bin/env bash
set -euo pipefail

# Package the native binary with the host GTK/WebKit stack. WebKit's production
# builds use absolute helper paths and private IPC: bundling Ubuntu libraries
# with Fedora helpers (or vice versa) is not safe. See issue #191.
if [[ $# != 4 ]]; then
    echo "Usage: $0 BINARY ICON DESKTOP_FILE OUTPUT_DIR" >&2
    exit 2
fi
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
binary=$(realpath "$1")
icon=$(realpath "$2")
desktop=$(realpath "$3")
mkdir -p "$4"
output_dir=$(cd -- "$4" && pwd)

case $(uname -m) in
    x86_64)
        arch=x86_64
        tool_sha=ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0
        runtime_sha=2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d
        ;;
    aarch64)
        arch=aarch64
        tool_sha=f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158
        runtime_sha=00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444
        ;;
    *) echo "Unsupported AppImage architecture: $(uname -m)" >&2; exit 1 ;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
appdir="$work/Cascade.AppDir"
mkdir -p "$appdir/usr/bin"
install -m 755 "$binary" "$appdir/usr/bin/cascade"
install -m 755 "$script_dir/AppRun" "$appdir/AppRun"
cp "$icon" "$appdir/cascade.png"
ln -s cascade.png "$appdir/.DirIcon"
cp "$desktop" "$appdir/cascade.desktop"

# Pin both the packager and its runtime; otherwise appimagetool downloads the
# moving continuous runtime even when the tool itself has a fixed version.
curl -fsSL --retry 3 -o "$work/appimagetool" \
    "https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-$arch.AppImage"
curl -fsSL --retry 3 -o "$work/runtime" \
    "https://github.com/AppImage/type2-runtime/releases/download/20251108/runtime-$arch"
printf '%s  %s\n' "$tool_sha" "$work/appimagetool" "$runtime_sha" "$work/runtime" | sha256sum -c -
chmod +x "$work/appimagetool"
ARCH="$arch" "$work/appimagetool" --appimage-extract-and-run \
    --runtime-file "$work/runtime" --no-appstream \
    "$appdir" "$output_dir/cascade-$arch.AppImage"
