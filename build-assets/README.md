# Cascade app icon sources

`appicon.svg` is the canonical, editable source for the Cascade app icon.
It uses only SVG geometry and gradients, so it has no font or editor-specific
dependencies.

The generated assets are committed alongside it:

- `appicon.png` and `../build/appicon.png` are 1024 px raster masters.
- `../build/darwin/icons.icns` and `../build/windows/icon.ico` are generated
  from `../build/appicon.png` with `task generate:icons`.
- `../frontend/src/assets/brand/cascade-mark.svg` is the full-bleed sidebar
  variant of the same artwork.
