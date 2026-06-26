// Human-readable labels for channel mode letters, resolved per server software family.
//
// IRC mode letters are defined per-ircd: the same letter means different things on
// different servers (e.g. +p is "private" on most ircds but "no KNOCK" on Solanum). The
// protocol carries no descriptions, so this is necessarily client-side knowledge keyed by
// the detected family (from the backend's RPL_MYINFO/ISUPPORT detection). Resolution is
// layered — family map → universal base map → typed generic fallback — with a guard that
// suppresses a label when the server advertised the letter in a different CHANMODES class
// than we expect (a confidently-wrong label is worse than a generic one).
//
// Family data is seeded from the community machine-readable defs (ircdocs irc-defs /
// defs.ircdocs.horse). Solanum (Libera, OFTC, …) is covered fully; other families list
// only high-confidence entries and otherwise rely on the base map + typed fallback.

export type ModeClass = 'A' | 'B' | 'C' | 'D';

export interface ModeContext {
  family: string; // detected software_family ('' when unknown)
  classA: string; // advertised CHANMODES class letters (list modes)
  classB: string; // always-parameterized
  classC: string; // parameterized only when set
  classD: string; // boolean flags
}

export interface ModeDescription {
  label: string;
  known: boolean; // false when we fell back to a generic typed label
}

interface LabelEntry {
  label: string;
  type: ModeClass; // expected class; used for the type-consistency guard
}

// Near-universal modes — same meaning across essentially every ircd.
const BASE: Record<string, LabelEntry> = {
  b: { label: 'Ban', type: 'A' },
  e: { label: 'Ban exception', type: 'A' },
  I: { label: 'Invite exception', type: 'A' },
  k: { label: 'Key (password)', type: 'B' },
  l: { label: 'User limit', type: 'C' },
  i: { label: 'Invite only', type: 'D' },
  m: { label: 'Moderated', type: 'D' },
  n: { label: 'No external messages', type: 'D' },
  s: { label: 'Secret', type: 'D' },
  t: { label: 'Topic locked to ops', type: 'D' },
  C: { label: 'Block CTCP', type: 'D' },
};

// charybdis / Solanum family (Libera, OFTC, …). Meanings per Libera's published modes.
const SOLANUM: Record<string, LabelEntry> = {
  q: { label: 'Quiet', type: 'A' },
  f: { label: 'Forward channel', type: 'C' },
  j: { label: 'Join throttle', type: 'C' },
  c: { label: 'Strip colors & formatting', type: 'D' },
  g: { label: 'Free invite', type: 'D' },
  p: { label: 'No KNOCK', type: 'D' },
  r: { label: 'Must be registered to join', type: 'D' },
  R: { label: 'Must be registered to speak', type: 'D' },
  z: { label: 'Reduced moderation (ops bypass)', type: 'D' },
  F: { label: 'Free forward target', type: 'D' },
  L: { label: 'Large ban list', type: 'D' },
  P: { label: 'Permanent channel', type: 'D' },
  Q: { label: 'No forwarding into here', type: 'D' },
  S: { label: 'TLS-only', type: 'D' },
  T: { label: 'Block channel notices', type: 'D' },
  u: { label: 'Allow server-filtered messages', type: 'D' },
};

// ergo / oragono — high-confidence entries only; the rest fall back.
const ERGO: Record<string, LabelEntry> = {
  q: { label: 'Quiet', type: 'A' },
  f: { label: 'Forward channel', type: 'C' },
  j: { label: 'Join throttle', type: 'C' },
  C: { label: 'Block CTCP', type: 'D' },
  M: { label: 'Must be registered to speak', type: 'D' },
  R: { label: 'Registered users only', type: 'D' },
  u: { label: 'Auditorium', type: 'D' },
  T: { label: 'No notices', type: 'D' },
};

const FAMILIES: Record<string, Record<string, LabelEntry>> = {
  solanum: SOLANUM,
  ergo: ERGO,
  // unrealircd / inspircd: not yet curated — base map + typed fallback apply.
};

const GENERIC: Record<ModeClass, string> = {
  A: 'List mode',
  B: 'Parameter',
  C: 'Parameter',
  D: 'Flag',
};

// advertisedClass reports which CHANMODES class the server put this letter in, or
// undefined when the letter appears in none (so we can't even classify it).
function advertisedClass(letter: string, ctx: ModeContext): ModeClass | undefined {
  if (ctx.classA.includes(letter)) return 'A';
  if (ctx.classB.includes(letter)) return 'B';
  if (ctx.classC.includes(letter)) return 'C';
  if (ctx.classD.includes(letter)) return 'D';
  return undefined;
}

// describeMode resolves a human label for a channel mode letter. It returns
// `known: false` when it could only produce a generic typed fallback, so callers may
// render uncertain labels differently.
export function describeMode(letter: string, ctx: ModeContext): ModeDescription {
  const cls = advertisedClass(letter, ctx);
  const entry = FAMILIES[ctx.family]?.[letter] ?? BASE[letter];

  // Use the curated label only when the server's advertised class agrees with ours.
  // If the server didn't classify the letter at all (cls undefined), trust the map.
  if (entry && (cls === undefined || cls === entry.type)) {
    return { label: entry.label, known: true };
  }

  return { label: cls ? GENERIC[cls] : 'Mode', known: false };
}
