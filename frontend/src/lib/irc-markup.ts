import { URL_REGEX_SOURCE } from '../components/irc-formatted-text';

// IRC formatting control codes. We wrap each span with the matching toggle code
// on both ends (color is terminated by a bare \x03), so spans stay independent
// without a global reset (\x0F).
export const IRC_BOLD = '\x02';
export const IRC_ITALIC = '\x1D';
export const IRC_UNDERLINE = '\x1F';
export const IRC_MONOSPACE = '\x11';
export const IRC_COLOR = '\x03';

// Anchored URL matcher built from the renderer's pattern, so markup inside a URL
// (e.g. underscores) is never interpreted as formatting.
const URL_AT = new RegExp('^' + URL_REGEX_SOURCE);
const COLOR_AT = /^#(\d{1,2})(?:,(\d{1,2}))?\(/;

// Toggle delimiters, ordered longest-first so `__` is tried as underline before
// `_` is tried as italic.
const DELIMITERS: { open: string; code: string }[] = [
  { open: '__', code: IRC_UNDERLINE },
  { open: '*', code: IRC_BOLD },
  { open: '_', code: IRC_ITALIC },
];

const ESCAPABLE = new Set(['*', '_', '#', '`', '\\']);

// A '#channel' word: '#' hugging a run of non-space characters. Kept in sync
// (loosely) with the renderer's channel matcher so what we protect here is what
// links there.
const CHANNEL_AT = /^#\S+/;

function isSpace(ch: string): boolean {
  return ch === '' || /\s/.test(ch);
}

/**
 * Convert a small, deliberately-limited markdown dialect into IRC control codes.
 *
 *   *bold*  _italic_  __underline__  `mono`  #N(text)  #N,M(text)
 *
 * Outbound-only: incoming messages already carry control codes and render through
 * the existing parser. Rules: delimiters must hug non-whitespace; unmatched
 * delimiters stay literal; URLs and #channel words pass through verbatim; a
 * `backtick` span is monospace with its contents emitted literally (no markup
 * interpreted inside); `\` escapes a delimiter.
 */
export function markdownToIrc(text: string): string {
  let out = '';
  let i = 0;
  const n = text.length;

  while (i < n) {
    const ch = text[i];

    // Backslash escape: emit the literal following markup char.
    if (ch === '\\' && i + 1 < n && ESCAPABLE.has(text[i + 1])) {
      out += text[i + 1];
      i += 2;
      continue;
    }

    // URLs pass through untouched.
    if (ch === 'h') {
      const m = text.slice(i).match(URL_AT);
      if (m) {
        out += m[0];
        i += m[0].length;
        continue;
      }
    }

    if (ch === '#') {
      // Color: #N(...) / #N,M(...) with N,M in 0..15 and '(' immediately after.
      const color = tryColor(text, i);
      if (color) {
        out += IRC_COLOR + color.spec + markdownToIrc(color.inner) + IRC_COLOR;
        i = color.end;
        continue;
      }
      // Otherwise a '#channel' word passes through verbatim, so markup chars in
      // a channel name (e.g. #fix_your_connection) are never interpreted.
      const chan = text.slice(i).match(CHANNEL_AT);
      if (chan) {
        out += chan[0];
        i += chan[0].length;
        continue;
      }
    }

    // Backtick monospace: contents are emitted literally (no recursion), so no
    // markup, color, or channel logic runs inside a `code span`.
    if (ch === '`') {
      const span = tryDelimiter(text, i, '`');
      if (span) {
        out += IRC_MONOSPACE + span.inner + IRC_MONOSPACE;
        i = span.end;
        continue;
      }
    }

    // Toggle delimiters (bold / italic / underline), longest-first.
    let matched = false;
    for (const d of DELIMITERS) {
      if (text.startsWith(d.open, i)) {
        const span = tryDelimiter(text, i, d.open);
        if (span) {
          out += d.code + markdownToIrc(span.inner) + d.code;
          i = span.end;
          matched = true;
          break;
        }
      }
    }
    if (matched) continue;

    out += ch;
    i += 1;
  }

  return out;
}

/** True when conversion would change the text (used to gate the live preview). */
export function hasMarkup(text: string): boolean {
  return markdownToIrc(text) !== text;
}

// Matches a color code with its optional spec, plus the other toggle/reset codes.
const IRC_CODES = /\x03(\d{1,2}(,\d{1,2})?)?|[\x02\x1D\x1F\x11\x16\x0F]/g;

/**
 * Strip all markup, returning plain text — used by the toolbar's "clear
 * formatting" action. Reuses the converter (markup → codes) then removes the
 * codes, so escapes resolve to literals for free.
 */
export function stripMarkup(text: string): string {
  return markdownToIrc(text).replace(IRC_CODES, '');
}

function tryDelimiter(text: string, i: number, open: string): { inner: string; end: number } | null {
  const contentStart = i + open.length;
  // Opening delimiter must hug a non-space.
  if (isSpace(text[contentStart] ?? '')) return null;

  let from = contentStart;
  while (from < text.length) {
    const close = text.indexOf(open, from);
    if (close === -1) return null;
    // Require non-empty content whose last char hugs the closing delimiter.
    if (close > contentStart && !isSpace(text[close - 1] ?? '')) {
      return { inner: text.slice(contentStart, close), end: close + open.length };
    }
    from = close + 1;
  }
  return null;
}

function tryColor(text: string, i: number): { spec: string; inner: string; end: number } | null {
  const m = text.slice(i).match(COLOR_AT);
  if (!m) return null;

  if (parseInt(m[1], 10) > 15) return null;
  let spec = m[1];
  if (m[2] !== undefined) {
    if (parseInt(m[2], 10) > 15) return null;
    spec += ',' + m[2];
  }

  const innerStart = i + m[0].length; // just past '('
  const close = text.indexOf(')', innerStart);
  if (close === -1) return null;

  return { spec, inner: text.slice(innerStart, close), end: close + 1 };
}
