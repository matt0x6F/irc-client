#!/usr/bin/env python3
"""Reject an AppImage that requires a newer host glibc than its target base."""

import argparse
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


def required_versions(path: Path):
    with path.open("rb") as source:
        elf_magic = source.read(4)
    if elf_magic != b"\x7fELF":
        return []
    result = subprocess.run(
        ["objdump", "-T", str(path)], capture_output=True, text=True
    )
    if result.returncode:
        if "not a dynamic object" in result.stderr:
            return []
        raise RuntimeError(f"objdump failed for {path}: {result.stderr.strip()}")
    return [
        (int(major), int(minor))
        for line in result.stdout.splitlines()
        if "*UND*" in line
        for major, minor in re.findall(r"GLIBC_(\d+)\.(\d+)", line)
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("appimage", type=Path)
    parser.add_argument("--max-version", required=True)
    args = parser.parse_args()
    limit = tuple(int(part) for part in args.max_version.split("."))
    image = args.appimage.resolve(strict=True)

    with tempfile.TemporaryDirectory() as directory:
        executable = Path(directory) / "artifact.AppImage"
        shutil.copy2(image, executable)
        executable.chmod(executable.stat().st_mode | 0o111)
        subprocess.run(
            [str(executable), "--appimage-extract"],
            cwd=directory,
            stdout=subprocess.DEVNULL,
            check=True,
        )
        payload = Path(directory) / "squashfs-root"
        if not payload.is_dir():
            parser.error("AppImage extraction produced no squashfs-root")
        requirements = []
        for path in [image, *payload.rglob("*")]:
            if path.is_file() and not path.is_symlink():
                versions = required_versions(path)
                if versions:
                    name = path.relative_to(payload) if path != image else Path("AppImage runtime")
                    requirements.append((max(versions), name))

    if not requirements:
        parser.error("No glibc symbol requirements found; check the artifact")
    highest = max(version for version, _ in requirements)
    print(f"Highest required glibc: {highest[0]}.{highest[1]}")
    if highest > limit:
        for version, path in requirements:
            if version > limit:
                print(f"  {path}: GLIBC_{version[0]}.{version[1]}")
        parser.exit(1, f"AppImage exceeds glibc {args.max_version}\n")


if __name__ == "__main__":
    main()
