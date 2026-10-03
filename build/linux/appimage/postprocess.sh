#!/usr/bin/env bash
set -euo pipefail

# Wails copies WebKit's helpers with os.Create (0600) and its stock AppRun
# cannot teach WebKit's compiled helper paths about the AppImage mount point.
image=$(realpath "$1")
appdir=$(realpath "$2")
builddir=$(dirname "$appdir")
arch=$(uname -m)
helpers=$(find "$appdir/usr/lib" -type f -path '*/webkitgtk-6.0/WebKitNetworkProcess' -print -quit)
if [[ -z "$helpers" ]]; then
    echo 'Bundled WebKitNetworkProcess is missing' >&2
    exit 1
fi
helperdir=$(dirname "$helpers")
for name in WebKitNetworkProcess WebKitWebProcess; do
    test -f "$helperdir/$name"
    chmod 755 "$helperdir/$name"
done

gcc -shared -fPIC -O2 -Wall -Wextra \
    -o "$appdir/usr/lib/libcascade-webkit-helper.so" \
    "$(dirname "$0")/webkit_helper_shim.c" \
    $(pkg-config --cflags --libs gio-2.0) -ldl

test ! -e "$appdir/AppRun.original"
mv "$appdir/AppRun" "$appdir/AppRun.original"
cat > "$appdir/AppRun" <<'EOF'
#!/bin/sh
APPDIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
export APPDIR
# LD_PRELOAD splits paths on spaces. Resolve the shim by its basename so an
# AppImage launched from a directory containing spaces still works.
export LD_LIBRARY_PATH="${APPDIR}/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export LD_PRELOAD="libcascade-webkit-helper.so${LD_PRELOAD:+:$LD_PRELOAD}"
exec "${APPDIR}/AppRun.original" "$@"
EOF
chmod 755 "$appdir/AppRun"

# Repackage the already deployed AppDir. Keep Wails' first image intact if
# this step fails; only replace it after linuxdeploy finishes successfully.
output=$(basename "$image" .AppImage)-fixed.AppImage
cd "$builddir"
OUTPUT="$output" APPIMAGE_EXTRACT_AND_RUN=1 NO_STRIP=1 \
    "$builddir/linuxdeploy-$arch.AppImage" --appdir "$appdir" --output appimage
mv -f "$builddir/$output" "$image"
