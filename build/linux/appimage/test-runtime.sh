#!/usr/bin/env bash
set -euo pipefail

# Test the actual packaged artifact on distributions with different helper paths.
# Usage: bash test-runtime.sh path/to/cascade-ARCH.AppImage [fedora:44|ubuntu:24.04]
image=$(realpath "${1:?AppImage path required}")
script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
test_image="cascade-appimage-smoke-$$"
cleanup() {
    rm -rf "$work"
    docker image rm "$test_image" >/dev/null 2>&1 || true
}
trap cleanup EXIT

distro=${2:-fedora:44}
# A clean base image has no WebKit runtime: fail with installation guidance,
# before launching the GUI. This also prevents silently bundling it again.
if docker run --rm -v "$image:/input/cascade.AppImage:ro" "$distro" \
    /input/cascade.AppImage --appimage-extract-and-run >"$work/missing.log" 2>&1; then
    echo "Expected a missing-runtime error on $distro" >&2
    exit 1
fi
if ! grep -q 'Cascade requires the system GTK 4 and WebKitGTK 6.0 runtime' "$work/missing.log"; then
    cat "$work/missing.log" >&2
    exit 1
fi
grep -q 'sudo dnf install gtk4 webkitgtk6.0' "$work/missing.log"
grep -q 'sudo apt install libgtk-4-1 libwebkitgtk-6.0-4' "$work/missing.log"
echo "PASS: missing-runtime guidance on $distro"

docker build --build-arg "DISTRO=$distro" -f "$script_dir/Dockerfile.smoke" \
    -t "$test_image" "$script_dir"
# Docker's default seccomp/user-namespace restrictions prevent WebKit's
# nested bubblewrap sandbox. Disable it ONLY for this disposable GUI test;
# AppRun must leave the production sandbox enabled. No IRC servers are used.
docker run --rm -v "$image:/input/cascade.AppImage:ro" \
    -e WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1 -e GSK_RENDERER=cairo \
    "$test_image"
