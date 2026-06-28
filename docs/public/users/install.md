# Install & first run

Prebuilt downloads are published for macOS, Windows, and Linux on the
[Releases page](https://github.com/matt0x6F/irc-client/releases). Windows and
Linux ship both Intel/AMD (`amd64`) and ARM (`arm64`) builds. Stable releases are
cut from tags; the newest pre-release tracks `main`, and the in-app updater can
follow either channel.

=== "macOS"

    Download the latest `Cascade-<version>-universal.dmg` from the
    [Releases page](https://github.com/matt0x6F/irc-client/releases). It's a
    universal binary that runs natively on both Apple Silicon and Intel Macs.

    1. Open the DMG and drag **Cascade** to your Applications folder.
    2. The build isn't code-signed, so the first launch needs one extra step to
       get past Gatekeeper:
        - **Right-click** `Cascade.app` → **Open** → **Open** in the dialog, or
        - run `xattr -dr com.apple.quarantine /Applications/Cascade.app` in
          Terminal.

    You only need to do this once; afterwards it launches normally.

=== "Windows"

    Download `cascade-<arch>-installer.exe` for your architecture (`amd64` for
    most PCs, `arm64` for ARM machines like Surface Pro X) and run it. It's an
    NSIS installer that drops Cascade into your Start menu.

    The build isn't code-signed, so SmartScreen may warn on first run — click
    **More info** → **Run anyway** to continue.

=== "Linux"

    Three packages are published per architecture (`amd64` and `arm64`); pick the
    one that matches your distro:

    - **AppImage** — `chmod +x` it and run directly, no install needed.
    - **`.deb`** — `sudo apt install ./Cascade-*.deb` (Debian/Ubuntu).
    - **`.rpm`** — `sudo dnf install ./Cascade-*.rpm` (Fedora/RHEL/openSUSE).

## Building from source

Prebuilt downloads above are the easy path; build from source if you want to
hack on Cascade or target a platform we don't publish. Works on macOS, Windows,
and Linux.

**Prerequisites**

- Go 1.25+ (required by Wails v3)
- Node.js 20 (see `.nvmrc`; with `nvm`, run `nvm use`)
- [Task](https://taskfile.dev): `go install github.com/go-task/task/v3/cmd/task@latest`
- Wails v3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

**Build**

```bash
git clone https://github.com/matt0x6F/irc-client
cd irc-client
task build      # builds for your current platform; output in bin/
```

To produce a distributable package instead of a bare binary:

=== "Windows"

    ```bash
    task package           # native build + installer
    ```

=== "Linux"

    ```bash
    wails3 task linux:package   # AppImage (.deb / .rpm tasks also available)
    ```

=== "macOS"

    ```bash
    task dmg-universal     # universal .dmg in bin/
    ```

!!! tip "Hot-reload dev build"
    To run Cascade from source with live reload while hacking on it, use
    `task dev` (or `wails3 dev`).
