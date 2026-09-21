# AppImage packaging precedents for issue #191

Researched 2026-09-20 (America/Los_Angeles). Related: [issue #191](https://github.com/matt0x6F/irc-client/issues/191) and [draft PR #195](https://github.com/matt0x6F/irc-client/pull/195).

## Question and conclusion

Can Cascade retain download-and-run AppImage distribution, reliable Ubuntu/Fedora startup, and its normal WebKit sandbox without disproportionate packaging complexity?

**Conclusion:** Other projects demonstrate several workable distribution strategies, but the inspected examples do not establish a small, proven recipe satisfying all three requirements for Cascade's GTK4/WebKitGTK6 stack. This is a research result, not proof that such a recipe is impossible. The host-runtime implementation in PR #195 remains a fallback pending a packaging decision.

AppImage's own guidance supports the portability requirement: bundle dependencies that cannot reasonably be assumed present on every supported target system. It permits baseline host libraries and warns that indiscriminate bundling can itself break compatibility. Requiring a separately installed web engine changes the user experience materially. [AppImage concepts](https://docs.appimage.org/introduction/concepts.html#do-not-depend-on-system-provided-resources)

## Released application precedents

### OrcaSlicer: host GTK/WebKit, with a diagnosis matching ours

Stable [v2.4.2](https://github.com/OrcaSlicer/OrcaSlicer/releases/tag/v2.4.2) ships AppImages. Its packaging explicitly explains that Ubuntu and Fedora compile different absolute paths into WebKit for helper processes, and deliberately uses host WebKit. Its exclusion policy covers the related GTK, GLib, GStreamer, libsoup, WebKit and JavaScriptCore stack. The launcher checks WebKit4.1 availability and prints installation instructions. [Helper-path rationale](https://github.com/OrcaSlicer/OrcaSlicer/blob/8500fcdccaa10b5099ac20d252af3a7c560046f1/src/dev-utils/platform/unix/build_linux_image.sh.in#L226-L229), [exclusions](https://github.com/OrcaSlicer/OrcaSlicer/blob/8500fcdccaa10b5099ac20d252af3a7c560046f1/scripts/appimage_lib_policy.sh#L9-L16), [dependency diagnostic](https://github.com/OrcaSlicer/OrcaSlicer/blob/8500fcdccaa10b5099ac20d252af3a7c560046f1/src/dev-utils/platform/unix/build_linux_image.sh.in#L339-L348).

**Implication:** Our fallback has a close released precedent, including its limitation. This application uses GTK3/WebKit4.1, so it is not exact validation of Cascade's stack. Conditional bundled-runtime code elsewhere in the script is not evidence that the released packaging bundles WebKit successfully.

### Bambu Studio: separate distribution targets

Stable [v02.08.02.61](https://github.com/bambulab/BambuStudio/releases/tag/v02.08.02.61) publishes distinct Ubuntu22.04 and Ubuntu24.04 AppImages and recommends a community-maintained Flatpak. Its packaging assembles application resources and built libraries; the inspected scripts do not provide a dedicated relocatable WebKit helper stack. The official QA page explains the WebKit4.0 versus 4.1 split between Ubuntu targets. [Assembly script](https://github.com/bambulab/BambuStudio/blob/926a7192574bcb9b3a732e1ec59a46d79cb45466/src/platform/unix/BuildLinuxImage.sh.in#L30-L47), [AppImage script](https://github.com/bambulab/BambuStudio/blob/926a7192574bcb9b3a732e1ec59a46d79cb45466/src/platform/unix/build_appimage.sh.in), [QA](https://github.com/bambulab/BambuStudio/wiki/QA).

**Implication:** An AppImage extension does not necessarily mean universal distribution compatibility. This is evidence of narrower support targets, not a portable WebKit solution to reuse.

### MuseScore Studio: bundled Qt plus selective host-library fallbacks

Stable [v4.7.5](https://github.com/musescore/MuseScore/releases/tag/v4.7.5) ships x86_64 and aarch64 AppImages. Its build runs linuxdeploy and linuxdeploy-plugin-qt to collect libraries and Qt dependencies. It also carries fallback copies of selected libraries, loading them only when the host lacks them. [Deployment calls](https://github.com/musescore/MuseScore/blob/3654226c2e99289916916953a98e585a3d3b315a/buildscripts/ci/linux/tools/make_appimage.sh#L121-L122), [fallback policy](https://github.com/musescore/MuseScore/blob/3654226c2e99289916916953a98e585a3d3b315a/buildscripts/ci/linux/tools/make_appimage.sh#L208-L231).

**Implication:** Bundling a framework while selectively using system integration libraries is an established approach. It does not solve WebKit-specific helper relocation. Separately, Qt WebEngine explicitly supports relocating its helper through `qt.conf` or `QTWEBENGINEPROCESS_PATH`; that production deployment interface is the kind of facility that makes bundling easier. This is framework documentation, not a claim that MuseScore uses Qt WebEngine. [Qt deployment documentation](https://doc.qt.io/qt-6/qtwebengine-deploying.html#deploying-qt-webengine-processes)

### Joplin: Electron runtime and established packaging tooling

Stable [v3.7.18](https://github.com/laurent22/joplin/releases/tag/v3.7.18) ships an AppImage. Its release configuration selects AppImage and pins toolset `1.0.3`, Electron `42.3.0`, and electron-builder `26.15.6`. [Release configuration](https://github.com/laurent22/joplin/blob/ce254820977ea3fb2559638f722960dfc60e1cb1/packages/app-desktop/package.json#L143-L152), [versions](https://github.com/laurent22/joplin/blob/ce254820977ea3fb2559638f722960dfc60e1cb1/packages/app-desktop/package.json#L188-L189).

In that electron-builder version, the legacy/default toolset gets `--no-sandbox`; explicitly selecting the newer toolset avoids that unconditional argument. This does not prove every runtime launch retains its sandbox. Current electron-builder documentation also describes a namespace-dependent sandbox fallback; those current defaults must not be projected backward onto every released application. [Pinned builder logic](https://github.com/electron-userland/electron-builder/blob/026bbdaffbab7aef558e061c46094c6672db33bb/packages/app-builder-lib/src/targets/appimage/AppImageTarget.ts#L33-L34), [current documentation](https://www.electron.build/docs/appimage/).

**Implication:** Applications can ship their rendering runtime with mature tooling, but runtime choice and sandbox policy still matter. Replacing Cascade's framework with Electron would be a substantially broader architectural change, not a packaging-only repair.

## Other upstream implementation evidence

### Lutris: helper relocation with the sandbox disabled

Official upstream development code bundles WebKit4.1 helpers, byte-patches their compiled path to a fixed `/tmp` location, creates a symlink to the bundle, and explicitly disables WebKit's sandbox. [AppRun](https://github.com/lutris/lutris/blob/8d882da59c68c8ff7f8eff43b2772c493ddcc088/utils/appimage/AppRun#L32-L51), [bundle and patch](https://github.com/lutris/lutris/blob/8d882da59c68c8ff7f8eff43b2772c493ddcc088/utils/appimage/build-in-container.sh#L183-L226).

This corrects an earlier provenance concern: the recipe exists in official upstream, as well as the personal fork initially found. However, latest stable [v0.5.22](https://github.com/lutris/lutris/releases/tag/v0.5.22) has a `.deb` release asset and the [official downloads page](https://lutris.net/downloads/) does not offer AppImage. Treat this as upstream development packaging, not established released AppImage practice. Its source comment describing sandbox disabling as standard practice is not independently established by this research.

Useful details include explicitly bundling dynamically loaded TLS modules and preferring per-binary RPATH to a globally inherited `LD_LIBRARY_PATH`. These address dependency discovery and host-subprocess contamination, respectively. [TLS modules](https://github.com/lutris/lutris/blob/8d882da59c68c8ff7f8eff43b2772c493ddcc088/utils/appimage/build-in-container.sh#L143-L163), [RPATH rationale](https://github.com/lutris/lutris/blob/8d882da59c68c8ff7f8eff43b2772c493ddcc088/utils/appimage/AppRun#L64-L75).

### Tauri, Pake and Yaak: bundled WebKit with different sandbox defaults

Released Tauri CLI 2.11.5 copies WebKit4.1 helper processes into its AppDir and uses a GTK plugin that binary-patches `/usr` to `././`. [Helper bundling](https://github.com/tauri-apps/tauri/blob/9452ddee5ebefd9b678a94ff003521379df6c9ae/crates/tauri-bundler/src/bundle/linux/appimage/linuxdeploy.rs#L149-L164), [path patch](https://github.com/tauri-apps/tauri/blob/9452ddee5ebefd9b678a94ff003521379df6c9ae/crates/tauri-bundler/src/bundle/linux/appimage/linuxdeploy-plugin-gtk.sh#L325-L326).

Concrete shipped applications include:

- [Pake V3.17.0](https://github.com/tw93/Pake/releases/tag/V3.17.0): official AppImages built on Ubuntu. Its FAQ also documents helper-path problems when locally building outside Debian's layout. [Build workflow](https://github.com/tw93/Pake/blob/38a27c1303c806eb05cd18078c53b14e77f23ed5/.github/workflows/single-app.yaml#L110-L125), [FAQ](https://github.com/tw93/Pake/blob/38a27c1303c806eb05cd18078c53b14e77f23ed5/docs/faq.md).
- [Yaak v2026.8.0](https://github.com/yaakapp/app/releases/tag/v2026.8.0): AMD64 and ARM64 AppImages, Ubuntu22.04 builders and WebKit4.1 dependencies. Its troubleshooting guide still recommends native packages when AppImage graphics workarounds fail. [Runners](https://github.com/yaakapp/app/blob/bbe8d1eef5291af0b42f3a6a04cea460eccaa9bf/.github/workflows/release-app.yml#L29-L39), [WebKit dependency](https://github.com/yaakapp/app/blob/bbe8d1eef5291af0b42f3a6a04cea460eccaa9bf/.github/workflows/release-app.yml#L102-L105), [troubleshooting](https://yaak.app/docs/troubleshooting/linux-graphics-issues).

The critical distinction is GTK3/WebKit4.1 versus GTK4/WebKit6. WebKit maps GTK4 to its 2022 API, which explicitly enables process sandboxing during context construction; the process-pool default is otherwise false. Wry's request to enable sandboxing remains open, as does its GTK4 migration. Therefore, successful Tauri bundling is not proof that the same relative-path patch preserves Cascade's normal sandbox. [API mapping](https://github.com/WebKit/WebKit/blob/58e02c19b9aa1d72a3acf3ee1b2c725b407962d5/Source/cmake/OptionsGTK.cmake#L189-L196), [pool default](https://github.com/WebKit/WebKit/blob/58e02c19b9aa1d72a3acf3ee1b2c725b407962d5/Source/WebKit/UIProcess/WebProcessPool.h#L950), [new API enabling](https://github.com/WebKit/WebKit/blob/58e02c19b9aa1d72a3acf3ee1b2c725b407962d5/Source/WebKit/UIProcess/API/glib/WebKitWebContext.cpp#L465-L468), [Wry sandbox issue](https://github.com/tauri-apps/wry/issues/935), [GTK4 migration](https://github.com/tauri-apps/wry/pull/1767).

### quick-sharun: experimental relocation with reduced sandbox restrictions

[Tauri PR #12491](https://github.com/tauri-apps/tauri/pull/12491) is open and unmerged at research time. It proposes an experimental quick-sharun bundler. The pinned tooling handles WebKit library names broadly enough to include GTK6, rewrites paths, and installs a bubblewrap wrapper. This makes it relevant to investigate, not a validated Cascade solution. [WebKit handling](https://github.com/pkgforge-dev/Anylinux-AppImages/blob/facb95e825cb082f634d48385f86b05a5c5cab66/useful-tools/quick-sharun.sh#L4801-L4804).

The corresponding sharun 2.3.0 wrapper makes AppDir paths and environment available inside the namespace, but explicitly removes WebKit's `--seccomp` argument and exposes the host `/tmp` through a bind mount. Thus it retains some namespace isolation while changing the normal sandbox substantially. Its apparent launch success cannot establish our sandbox-preservation requirement. [Wrapper rationale](https://github.com/pkgforge-dev/Anylinux-sharun/blob/a91dfd996f8ae67b6d9cc8c7d4872364ce6c50fe/src/bwrap_wrapper.rs#L1-L22), [mounts](https://github.com/pkgforge-dev/Anylinux-sharun/blob/a91dfd996f8ae67b6d9cc8c7d4872364ce6c50fe/src/bwrap_wrapper.rs#L63-L95), [seccomp removal](https://github.com/pkgforge-dev/Anylinux-sharun/blob/a91dfd996f8ae67b6d9cc8c7d4872364ce6c50fe/src/bwrap_wrapper.rs#L191-L201).

## Engineering recommendation (not a verified implementation)

Keep download-and-run as the target. Do not infer that installing host WebKit is unavoidable, or copy a launcher that disables/reduces sandboxing simply because it starts successfully.

The most useful next experiment is a minimal GTK4/WebKit6 application with runtime-resolved absolute helper paths and narrowly scoped read-only access to its bundle inside WebKit's sandbox. Preserve WebKit's seccomp filter and private temporary directory. Determine whether that can be achieved through a small maintained packaging change, or requires rebuilding/patching WebKit; the survey does not establish the implementation cost.

A credible success result needs more than a visible window:

1. Start on clean Fedora and Ubuntu desktops without host WebKit, using the actual mounted AppImage as well as extraction paths containing spaces.
2. Confirm the WebKit library, both helper processes and injected bundle come from the same shipped runtime.
3. Verify sandbox state in the subprocesses, including seccomp and namespace/mount isolation, with no sandbox-disable override or weakened wrapper.
4. Exercise rendering, HTTPS/TLS, shutdown, concurrent instances and host subprocess/plugin execution; repeat on AMD64 and ARM64.

These are proposed acceptance checks, not claims of testing completed during this research. If the bounded experiment requires broad runtime surgery, return with that concrete maintenance cost before selecting host-runtime packaging or another distribution format.

## Evidence limits and PR status

This survey inspected first-party source, release metadata and official documentation. It did not rebuild or execute the third-party applications. Pinned source describes the release's build intent, not a byte-for-byte audit of its downloadable artifact. Links to issues, documentation and open PRs are time-sensitive.

Cascade's earlier local relative-path prototype opened a window with container-only sandbox disabling, but failed when its normal sandbox was enabled because bubblewrap received a relative helper mount path. Those earlier experiments are recorded in [PR #195](https://github.com/matt0x6F/irc-client/pull/195); they are not new validation from this survey. PR #195 remains draft and the host-runtime implementation is not the agreed final resolution.
