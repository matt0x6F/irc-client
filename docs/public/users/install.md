# Install & first run

!!! info "Prebuilt downloads are macOS-only for now"
    Official release builds on the
    [Releases page](https://github.com/matt0x6F/irc-client/releases) currently
    cover macOS only. Windows and Linux are fully supported by the codebase, but
    aren't auto-published yet, so build them from source (below). Native
    installers for those platforms are planned.

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

    No prebuilt installer is published yet — build from source (see below).
    On Windows the build produces a native executable and can be packaged into
    an NSIS installer.

=== "Linux"

    No prebuilt package is published yet, so build from source (see below). The
    Linux build can be packaged as an AppImage, `.deb`, or `.rpm`.

## Building from source

This is the current path for Windows and Linux, and works on macOS too.

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

## irc:// and ircs:// link handling

Cascade registers itself as the system handler for `irc://` and `ircs://` URIs
so that clicking IRC links in a browser or another app opens Cascade directly.
Registration happens automatically at install/first-run time, per platform:

- **macOS:** declared in the app bundle's `Info.plist`; no manual step needed.
- **Windows:** written to the registry by the NSIS installer.
- **Linux:** provided by the `.desktop` file installed alongside the binary.

See [Opening irc:// links](connecting.md#opening-irc-links) for what Cascade
does after it receives the link.
