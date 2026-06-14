# Apply Cascade Design System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the Cascade Chat design system's new material (self-hosted fonts, full token set, active-pane glow, glass bump, brand wordmark) to the live Wails+React frontend.

**Architecture:** Almost everything routes through `frontend/src/style.css` (the single theme entry point): add `@font-face` rules, token blocks, and a `.cc-active-pane` utility. Three components consume the new utility/tokens: `server-tree.tsx` (glow + wordmark), `App.tsx` and `input-area.tsx` (glass bump), `message-view.tsx` (mention tokens). The Go backend is untouched.

**Tech Stack:** React 19 + TypeScript, Tailwind CSS v4, Vite, Wails. Verification via `tsc --noEmit`, `vitest`, and `vite build`.

**Working directory:** All paths are relative to the worktree root `/Users/matt/Developer/irc/.claude/worktrees/affectionate-maxwell-21af7e`. Run all commands from `frontend/` unless stated.

**Note on TDD:** This is visual/CSS work with no meaningful new unit surface. Each task's verification gate is type-check + existing tests + build (and, where relevant, a grep assertion that the change landed), not new unit tests. The one existing test that matters is `irc-formatted-text.test.tsx` (exercises the IRC palette) — it must stay green.

---

## File Structure

**Modified:**
- `frontend/src/style.css` — all `@font-face`, tokens, and the `.cc-active-pane` utility (primary file).
- `frontend/src/components/server-tree.tsx` — active rows → `.cc-active-pane`; sidebar header → wordmark.
- `frontend/src/App.tsx` — header glass bump.
- `frontend/src/components/input-area.tsx` — composer glass bump.
- `frontend/src/components/message-view.tsx` — mention rows → `--mention-*` tokens.

**Created:**
- `frontend/src/assets/fonts/jetbrains-mono-{400,500,600}.woff2` — self-hosted mono.
- `frontend/src/assets/fonts/nunito-{500,600,700,800}.woff2` — self-hosted Nunito weights.
- `frontend/src/assets/brand/cascade-wordmark.svg`, `frontend/src/assets/brand/cascade-mark.svg` — brand marks.

---

## Task 1: Self-host the fonts

**Files:**
- Create: `frontend/src/assets/fonts/jetbrains-mono-{400,500,600}.woff2`, `frontend/src/assets/fonts/nunito-{500,600,700,800}.woff2`
- Modify: `frontend/src/style.css` (top, after the existing Nunito 400 `@font-face`)

- [ ] **Step 1: Download the woff2 files from Google Fonts (latin subset)**

Run from the worktree root:

```bash
cd frontend/src/assets/fonts
UA="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
for w in 500 600 700 800; do
  url=$(curl -s -H "User-Agent: $UA" "https://fonts.googleapis.com/css2?family=Nunito:wght@$w&display=swap" \
    | awk '/\/\* latin \*\//{f=1} f&&/src:/{print; exit}' | grep -oE "https://[^)]+\.woff2")
  curl -s "$url" -o "nunito-$w.woff2"
done
for w in 400 500 600; do
  url=$(curl -s -H "User-Agent: $UA" "https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@$w&display=swap" \
    | awk '/\/\* latin \*\//{f=1} f&&/src:/{print; exit}' | grep -oE "https://[^)]+\.woff2")
  curl -s "$url" -o "jetbrains-mono-$w.woff2"
done
```

- [ ] **Step 2: Verify all 7 files downloaded and are non-trivial**

Run from `frontend/src/assets/fonts`:

```bash
ls -l nunito-{500,600,700,800}.woff2 jetbrains-mono-{400,500,600}.woff2
# Each must be > 5000 bytes. If any is missing/tiny, the CDN fetch failed.
for f in nunito-500 nunito-600 nunito-700 nunito-800 jetbrains-mono-400 jetbrains-mono-500 jetbrains-mono-600; do
  sz=$(wc -c < "$f.woff2" 2>/dev/null || echo 0)
  [ "$sz" -gt 5000 ] && echo "OK  $f ($sz)" || echo "FAIL $f ($sz)"
done
```

Expected: 7 lines all `OK`. **If any line is `FAIL`**, do NOT proceed to self-host that family — instead skip Steps 3–4 for the failed family, add this single line at the top of `style.css` right after `@import "tailwindcss";`, and note the fallback in the final summary:

```css
@import url("https://fonts.googleapis.com/css2?family=Nunito:wght@500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap");
```

- [ ] **Step 3: Add `@font-face` rules and the `--font-mono` token to `style.css`**

In `frontend/src/style.css`, immediately after the existing Nunito 400 `@font-face` block (which ends at the line `}` before `:root {`), insert:

```css
@font-face {
    font-family: "Nunito";
    font-style: normal;
    font-weight: 500;
    font-display: swap;
    src: url("assets/fonts/nunito-500.woff2") format("woff2");
}
@font-face {
    font-family: "Nunito";
    font-style: normal;
    font-weight: 600;
    font-display: swap;
    src: url("assets/fonts/nunito-600.woff2") format("woff2");
}
@font-face {
    font-family: "Nunito";
    font-style: normal;
    font-weight: 700;
    font-display: swap;
    src: url("assets/fonts/nunito-700.woff2") format("woff2");
}
@font-face {
    font-family: "Nunito";
    font-style: normal;
    font-weight: 800;
    font-display: swap;
    src: url("assets/fonts/nunito-800.woff2") format("woff2");
}
@font-face {
    font-family: "JetBrains Mono";
    font-style: normal;
    font-weight: 400;
    font-display: swap;
    src: url("assets/fonts/jetbrains-mono-400.woff2") format("woff2");
}
@font-face {
    font-family: "JetBrains Mono";
    font-style: normal;
    font-weight: 500;
    font-display: swap;
    src: url("assets/fonts/jetbrains-mono-500.woff2") format("woff2");
}
@font-face {
    font-family: "JetBrains Mono";
    font-style: normal;
    font-weight: 600;
    font-display: swap;
    src: url("assets/fonts/jetbrains-mono-600.woff2") format("woff2");
}
```

Then, inside the existing `:root { ... }` block (add near the bottom, before the closing `}`), add the mono family token so Tailwind's `font-mono` and any `var(--font-mono)` resolve to JetBrains Mono first:

```css
    --font-mono: "JetBrains Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
```

- [ ] **Step 4: Point Tailwind's mono utility at the token**

Tailwind v4 reads theme values from CSS. In `frontend/src/style.css`, the `@import "tailwindcss";` is already present at the top. Add a `@theme` mapping so `font-mono` utilities (used by timestamps and `kbd`) resolve to the token. Add this block immediately after the last `@font-face` rule and before `:root {`:

```css
@theme {
    --font-mono: "JetBrains Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
}
```

(Note: `@theme` is the Tailwind v4 mechanism; the `:root` copy from Step 3 covers any direct `var(--font-mono)` use. Keep both.)

- [ ] **Step 5: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed (no TS errors; Vite build completes and bundles the woff2 assets).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/assets/fonts frontend/src/style.css
git commit -m "Self-host JetBrains Mono + Nunito 500-800, wire --font-mono token"
```

---

## Task 2: Add the missing design tokens

**Files:**
- Modify: `frontend/src/style.css` (`:root` and `.dark` blocks)

- [ ] **Step 1: Add role / presence / IRC-16 / mention / type-scale / glass tokens to `:root`**

In `frontend/src/style.css`, inside the existing `:root { ... }` block, append before its closing `}` (these mirror the design system's `tokens/colors.css`, `typography.css`, `effects.css`):

```css
    /* ---- IRC member roles (channel mode prefixes) ---- */
    --role-owner:  #9333ea;
    --role-admin:  #dc2626;
    --role-op:     #ef4444;
    --role-halfop: #f97316;
    --role-voice:  #3b82f6;

    /* ---- Presence ---- */
    --presence-online:  #22c55e;
    --presence-away:    #f59e0b;
    --presence-offline: #94a3b8;

    /* ---- Mention / highlight ---- */
    --mention-bg: rgba(234, 179, 8, 0.10);
    --mention-border: rgba(234, 179, 8, 0.50);

    /* ---- IRC standard 16-color text palette (mIRC) ---- */
    --irc-0:  #ffffff; --irc-1:  #000000; --irc-2:  #00007f; --irc-3:  #007f00;
    --irc-4:  #ff0000; --irc-5:  #7f0000; --irc-6:  #9c009c; --irc-7:  #fc7f00;
    --irc-8:  #ffff00; --irc-9:  #00fc00; --irc-10: #009393; --irc-11: #00ffff;
    --irc-12: #0000fc; --irc-13: #ff00ff; --irc-14: #7f7f7f; --irc-15: #cfcfcf;

    /* ---- Semantic status (light) ---- */
    --success: #22c55e;
    --warning: #f59e0b;

    /* ---- Type scale ---- */
    --text-2xs: 0.6875rem;
    --text-xs:  0.75rem;
    --text-sm:  0.875rem;
    --text-base:1rem;
    --text-lg:  1.125rem;
    --text-xl:  1.375rem;
    --text-2xl: 1.75rem;
    --text-3xl: 2.25rem;
    --text-4xl: 3rem;
    --leading-tight: 1.2;
    --leading-snug: 1.4;
    --leading-normal: 1.6;
    --tracking-tight: -0.02em;
    --tracking-body: 0.01em;
    --tracking-wide: 0.04em;

    /* ---- Glass / backdrop (header + composer + popovers) ---- */
    --backdrop-blur: 8px;
    --glass-bg: rgba(255, 255, 255, 0.50);
```

- [ ] **Step 2: Add the dark-mode overrides to `.dark`**

In the existing `.dark { ... }` block, append before its closing `}`:

```css
    --role-owner:  #c084fc;
    --role-admin:  #f87171;
    --role-op:     #f87171;
    --role-halfop: #fb923c;
    --role-voice:  #60a5fa;

    --mention-bg: rgba(250, 204, 21, 0.10);
    --mention-border: rgba(250, 204, 21, 0.45);

    --success: #4ade80;
    --warning: #fbbf24;

    --glass-bg: rgba(30, 41, 59, 0.50);
```

- [ ] **Step 3: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/style.css
git commit -m "Add role/presence/IRC-16/mention/type-scale/glass design tokens"
```

---

## Task 3: Active-pane glow utility + apply to the network tree

**Files:**
- Modify: `frontend/src/style.css` (append `.cc-active-pane` utility at end of file)
- Modify: `frontend/src/components/server-tree.tsx` (4 active-row class blocks)

- [ ] **Step 1: Add the `.cc-active-pane` utility to `style.css`**

Append to the very end of `frontend/src/style.css` (values copied from the design system's `template.css` glow):

```css
/* Active-pane indicator: a soft left-edge glow instead of a hard border bar.
   Softer in light, fuller in dark (matches the design system's final iteration). */
.cc-active-pane {
    background:
        linear-gradient(90deg, color-mix(in srgb, var(--primary) 10%, transparent), transparent 55%),
        color-mix(in srgb, var(--accent) 70%, transparent);
    box-shadow:
        inset 2px 0 0 0 color-mix(in srgb, var(--primary) 45%, transparent),
        inset 14px 0 20px -13px color-mix(in srgb, var(--primary) 40%, transparent);
}
.dark .cc-active-pane {
    background:
        linear-gradient(90deg, color-mix(in srgb, var(--primary) 18%, transparent), transparent 55%),
        color-mix(in srgb, var(--accent) 85%, transparent);
    box-shadow:
        inset 2px 0 0 0 color-mix(in srgb, var(--primary) 55%, transparent),
        inset 14px 0 20px -12px color-mix(in srgb, var(--primary) 70%, transparent);
}
```

- [ ] **Step 2: Replace the hard active bar in the 4 nav rows of `server-tree.tsx`**

There are 4 occurrences of the active-state classes `'bg-primary/10 border-l-4 border-primary'` (server row, status row, channel row, PM row). In each ternary, replace that string with `'cc-active-pane'`. The hover branch (`'hover:bg-accent/70'`) stays unchanged. For example, the channel row becomes:

```tsx
                            className={`p-2 cursor-pointer select-none flex items-center justify-between transition-all ${
                              isSelected && selectedChannel === channel
                                ? 'cc-active-pane'
                                : 'hover:bg-accent/70'
                            }`}
```

Apply the identical substitution to all four blocks (server: `isSelected ? 'bg-primary/10 border-l-4 border-primary'`; status: `isSelected && selectedChannel === 'status' ? ...`; channel; PM: `isSelected && selectedChannel === pmKey ? ...`).

- [ ] **Step 3: Verify no hard-bar active classes remain**

Run from the worktree root:

```bash
grep -n "border-l-4 border-primary" frontend/src/components/server-tree.tsx
grep -c "cc-active-pane" frontend/src/components/server-tree.tsx
```

Expected: first grep prints **nothing** (the `hover:border-l-4 hover:border-primary` in context-menu buttons is a different string and is intentionally left alone — confirm none of the 4 *row* selections match). Second grep prints `4`.

- [ ] **Step 4: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/style.css frontend/src/components/server-tree.tsx
git commit -m "Replace hard active-row bar with soft left-edge glow (cc-active-pane)"
```

---

## Task 4: Glass bump on header + composer

**Files:**
- Modify: `frontend/src/App.tsx` (header wrapper, ~line 511)
- Modify: `frontend/src/components/input-area.tsx` (composer wrapper, ~line 206)

- [ ] **Step 1: Bump the header to an 8px blur backed by `--glass-bg`**

In `frontend/src/App.tsx`, the header wrapper currently is:

```tsx
        <div className="border-b border-border bg-card/50 backdrop-blur-sm">
```

Replace with (drop the Tailwind `bg-card/50 backdrop-blur-sm`, use the glass tokens via inline style so the 8px value matches `--backdrop-blur`):

```tsx
        <div
          className="border-b border-border"
          style={{ background: 'var(--glass-bg)', backdropFilter: 'blur(var(--backdrop-blur))', WebkitBackdropFilter: 'blur(var(--backdrop-blur))' }}
        >
```

- [ ] **Step 2: Bump the composer the same way**

In `frontend/src/components/input-area.tsx`, the wrapper currently is:

```tsx
    <div className="border-t border-border p-4 bg-card/50 backdrop-blur-sm">
```

Replace with:

```tsx
    <div
      className="border-t border-border p-4"
      style={{ background: 'var(--glass-bg)', backdropFilter: 'blur(var(--backdrop-blur))', WebkitBackdropFilter: 'blur(var(--backdrop-blur))' }}
    >
```

- [ ] **Step 3: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/App.tsx frontend/src/components/input-area.tsx
git commit -m "Bump header + composer glass to 8px blur backed by --glass-bg"
```

---

## Task 5: Brand wordmark in the sidebar header

**Files:**
- Create: `frontend/src/assets/brand/cascade-wordmark.svg`, `frontend/src/assets/brand/cascade-mark.svg`
- Modify: `frontend/src/components/server-tree.tsx` (header block, ~lines 320-323)

- [ ] **Step 1: Copy the brand SVGs into the frontend assets**

Run from the worktree root:

```bash
mkdir -p frontend/src/assets/brand
cp /tmp/design_bundle/cascade-chat-design-system/project/assets/logo/cascade-wordmark.svg frontend/src/assets/brand/cascade-wordmark.svg
cp /tmp/design_bundle/cascade-chat-design-system/project/assets/logo/cascade-mark.svg frontend/src/assets/brand/cascade-mark.svg
ls -l frontend/src/assets/brand/
```

Expected: both SVGs present.

(If `/tmp/design_bundle/...` no longer exists, re-extract: `cd /tmp && rm -rf design_bundle && mkdir design_bundle && cd design_bundle && gzip -dc /Users/matt/.claude/projects/-Users-matt-Developer-irc--claude-worktrees-affectionate-maxwell-21af7e/*/tool-results/webfetch-*.bin | tar x` then retry the `cp`.)

- [ ] **Step 2: Import and render the brand lockup in the sidebar header**

**Why the mark, not the wordmark SVG:** the wordmark SVG bakes in dark text fills (`#1a1a1a`) that would be near-invisible on the dark sidebar; an `<img>` can't inherit theme colors. The `cascade-mark.svg` is an icon-only blue square (theme-agnostic). So we render the **mark icon** + the "Cascade" / "Chat" text in HTML, using theme tokens — this adapts to both themes and exercises the new Nunito 800 weight (`font-extrabold`).

In `frontend/src/components/server-tree.tsx`, add the import at the top with the other imports (Vite resolves `*.svg` imports to a URL string by default):

```tsx
import markUrl from '../assets/brand/cascade-mark.svg';
```

Then replace the existing header block:

```tsx
        <div className="p-4 border-b border-border bg-card/50">
        <h2 className="font-semibold text-lg">Networks</h2>
      </div>
```

with the brand lockup (`alt=""` because the adjacent text already names it; weights mirror the wordmark — 800 for "Cascade", 600/muted for "Chat"):

```tsx
        <div className="p-4 border-b border-border bg-card/50 flex items-center gap-2.5">
          <img src={markUrl} alt="" className="h-7 w-7 rounded-lg select-none flex-shrink-0" draggable={false} />
          <span className="font-extrabold text-lg tracking-tight text-foreground">Cascade</span>
          <span className="font-semibold text-sm text-muted-foreground">Chat</span>
        </div>
```

- [ ] **Step 3: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed. If `tsc` complains about the `*.svg` import having no type declaration, add a module declaration to `frontend/src/vite-env.d.ts`:

```ts
declare module '*.svg' {
    const src: string;
    export default src;
}
```

(Only add this if `vite-env.d.ts` does not already declare `*.svg`; Vite's `vite/client` types usually cover it. Check first with `grep -n "svg" frontend/src/vite-env.d.ts`.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/assets/brand frontend/src/components/server-tree.tsx frontend/src/vite-env.d.ts
git commit -m "Add Cascade brand lockup to the sidebar header (placeholder mark)"
```

---

## Task 6: Wire mention rows to the mention tokens

**Files:**
- Modify: `frontend/src/components/message-view.tsx` (mention row classes, ~lines 396-398)

- [ ] **Step 1: Replace the hardcoded mention colors with the tokens**

In `frontend/src/components/message-view.tsx`, the mention branch currently is:

```tsx
                hasMention
                  ? 'bg-yellow-500/10 dark:bg-yellow-400/10 border-l-2 border-yellow-500/50'
```

Replace the className string with a token-driven variant. Keep the structural `border-l-2` but source the colors from `--mention-bg` / `--mention-border` via an inline style on the row. Change the mention branch to add a marker class and move the color to inline style:

```tsx
                hasMention
                  ? 'cc-mention border-l-2'
```

Then add `.cc-mention` to the end of `frontend/src/style.css`:

```css
.cc-mention {
    background: var(--mention-bg);
    border-left-color: var(--mention-border);
}
```

(The `.dark` mention values are already handled by the `--mention-*` overrides in the `.dark` token block from Task 2, so no separate dark rule is needed.)

- [ ] **Step 2: Verify the change landed and the IRC-palette test still passes**

Run from the worktree root, then `frontend/`:

```bash
grep -n "cc-mention" frontend/src/components/message-view.tsx frontend/src/style.css
cd frontend && npx vitest run
```

Expected: grep shows the class in both files; vitest passes (notably `irc-formatted-text.test.tsx`).

- [ ] **Step 3: Type-check and build**

Run from `frontend/`:

```bash
npx tsc --noEmit && npm run build
```

Expected: both succeed.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/message-view.tsx frontend/src/style.css
git commit -m "Drive mention-row colors from --mention-* tokens"
```

---

## Task 7: Full verification pass

**Files:** none (verification only)

- [ ] **Step 1: Run the complete check suite**

Run from the worktree root:

```bash
cd frontend && npx tsc --noEmit && npx vitest run && npm run build
```

Expected: type-check clean, all tests pass, build succeeds.

- [ ] **Step 2: Visual smoke test (manual, if a dev environment is available)**

If `task dev` can run, launch it and confirm:
- Selected network/channel/PM rows show the soft left-edge glow (not a hard bar), in **both** light and dark mode.
- Timestamps and the search `kbd` hint render in JetBrains Mono (distinct from the Nunito body).
- The Cascade wordmark renders in the sidebar header and is legible in both themes.
- Header and composer remain glassy (content blurs softly beneath them).

If no dev environment is available, note in the summary that visual verification is pending and rely on the build/type/test gates.

- [ ] **Step 3: Confirm the branch state**

Run from the worktree root:

```bash
git log --oneline -7
git status --porcelain
```

Expected: 6 implementation commits (Tasks 1–6) on top of the spec commit; working tree clean.
