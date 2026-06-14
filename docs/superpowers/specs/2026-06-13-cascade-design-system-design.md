# Apply the Cascade Chat Design System — Design

_2026-06-13_

## Context

A design-system handoff bundle (`cascade-chat-design-system`, exported from Claude
Design) was provided. It was **reverse-engineered from this codebase**, then
iterated in HTML/CSS/JS prototypes that layered visual refinements on top. The
foundations (semantic colors, shadows, transitions, spacing, radius, Nunito 400)
already live in `frontend/src/style.css`. This work closes the gap between the
shipped design system and the live app.

Scope (confirmed with user): **Foundations + visual refinements**. Not a
component-by-component pixel-parity rewrite.

## Goals

Apply the genuinely-new material from the design system to the real frontend:

1. **Fonts** — self-host JetBrains Mono (400/500/600) and Nunito 500–800 so
   `font-mono` and weighted text render true instead of falling back to system
   fonts. (Decision: **self-host**, because this is an offline-capable Wails
   desktop app; the design README itself flags self-hosting as the offline path.)
2. **Token coverage** — promote the design's missing CSS custom properties into
   `style.css`: member-role colors, presence colors, the IRC-16 palette, mention
   bg/border, the type scale, and glass tokens (`--backdrop-blur`, `--glass-bg`).
3. **Active-pane glow** — replace the hard `border-l-4 border-primary` selection
   bar on nav rows with a theme-aware soft left-edge glow (the user's final
   iteration in the design chat: softer in light, fuller in dark).
4. **Glass bump** — header + composer backdrop blur 4px → 8px to match the design
   token; back the tint with `--glass-bg`.
5. **Brand wordmark** — drop the design system's `cascade-wordmark.svg` into the
   sidebar header in place of the plain "Networks" text. (Decision: **yes**;
   placeholder logo, to be swapped for an official mark later.)

## Non-Goals (explicit exclusions)

- **Reply / React / "More" message actions.** The prototype mocked these; they
  are not real features in this IRC client. Adding non-functional buttons would
  be inventing features. The existing **Pin** action is real and is kept.
- **Logo-to-collapse + members "rail".** The live app already evolved past the
  prototype here (header collapse toggles + Users/Pinned tabs, which are more
  functional). Per the design README, match visuals but don't copy prototype
  structure when the app's own structure fits better.
- **Deep refactor of every token consumer.** Tokens are added as pure additions;
  only obvious consumers (e.g. mention rows) are rewired. No broad churn.
- **Go backend.** Untouched.

## Approach by file

### `frontend/src/style.css` (primary)
- Add `@font-face` rules for the self-hosted woff2 files (JetBrains Mono 400/500/600,
  Nunito 500/600/700/800). Keep the existing Nunito 400 face.
- Add `--font-sans` / `--font-mono` family tokens (mono ahead of system fallbacks).
- Add token blocks (`:root` + `.dark` where the design defines dark variants):
  role colors, presence, IRC-16, mention, type scale, glass tokens.
- Add a `.cc-active-pane` utility (and `.dark` override) implementing the
  left-edge glow via a gradient — replacing the hard bar.

### `frontend/src/components/server-tree.tsx`
- Swap the 4 active-row class blocks from
  `bg-primary/10 border-l-4 border-primary` → `cc-active-pane` (keeps the active
  tint, replaces the hard bar with the glow). Hover state unchanged.

### `frontend/src/App.tsx` + `frontend/src/components/input-area.tsx`
- Header and composer: `backdrop-blur-sm` → an 8px blur backed by `--glass-bg`
  (via an inline style or a small utility class, since Tailwind's named steps
  don't include an 8px token here).

### `frontend/src/components/server-tree.tsx` (header) + assets
- Copy `cascade-wordmark.svg` (and `cascade-mark.svg` for future use) into
  `frontend/src/assets/brand/`. Render the wordmark in the sidebar header,
  `currentColor`-tinted so it works in both themes; keep an accessible label.

### Fonts: acquisition
- Nunito 400 woff2 already exists in the repo. Fetch the remaining weights and
  JetBrains Mono woff2 from the Google Fonts CDN (latin subset) at build-prep
  time, store under `frontend/src/assets/fonts/`. If a fetch fails, fall back to
  a Google Fonts `@import` for the missing family and flag it.

## Verification

- `cd frontend && npx tsc --noEmit` — type-check clean.
- `cd frontend && npx vitest run` — existing tests pass (notably the
  irc-formatted-text test, which exercises the IRC palette).
- `task check` if available (fmt/lint/test across the repo).
- Visual smoke: active-row glow reads correctly in **both** light and dark; mono
  text (timestamps, `kbd`) renders in JetBrains Mono; wordmark renders in the
  sidebar header in both themes.

## Risks

- **Font fetch** depends on network at implement time; mitigated by the
  Google-Fonts `@import` fallback.
- **Glass blur** on a low-power desktop could be a perf consideration; it's
  already in use (4px), so 8px is a marginal change.
