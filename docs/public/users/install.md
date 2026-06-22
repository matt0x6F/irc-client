# Install & first run

## macOS

Download the latest `Cascade-<version>-universal.dmg` from the
[Releases page](https://github.com/matt0x6F/irc-client/releases). The build is a
universal binary, running natively on both Apple Silicon and Intel Macs.

1. Open the DMG and drag **Cascade** to your Applications folder.
2. The build is **not code-signed**, so the first launch needs one extra step to
   get past Gatekeeper:
   - **Right-click** `Cascade.app` → **Open** → **Open** in the dialog, or
   - run `xattr -dr com.apple.quarantine /Applications/cascade.app` in Terminal.

You only need to do this once; afterwards it launches normally.

## Windows

> _Documentation pending._ Download the Windows build from the
> [Releases page](https://github.com/matt0x6F/irc-client/releases).

## Linux

> _Documentation pending._ Download the Linux build from the
> [Releases page](https://github.com/matt0x6F/irc-client/releases).
