export interface NetworkColor {
  key: string;
  bg: string;
  fg: string;
}

/** Curated, contrast-safe tile colors. bg carries the monogram in fg. */
export const NETWORK_COLORS: NetworkColor[] = [
  { key: 'purple', bg: '#CECBF6', fg: '#26215C' },
  { key: 'teal', bg: '#9FE1CB', fg: '#04342C' },
  { key: 'coral', bg: '#F5C4B3', fg: '#4A1B0C' },
  { key: 'pink', bg: '#F4C0D1', fg: '#4B1528' },
  { key: 'blue', bg: '#B5D4F4', fg: '#042C53' },
  { key: 'green', bg: '#C0DD97', fg: '#173404' },
  { key: 'amber', bg: '#FAC775', fg: '#412402' },
  { key: 'red', bg: '#F7C1C1', fg: '#501313' },
  { key: 'gray', bg: '#D3D1C7', fg: '#2C2C2A' },
];

/** Stable non-negative hash (djb2) of a string. */
function hash(s: string): number {
  let h = 5381;
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0;
  return h;
}

/** Deterministic fallback palette key derived from the network name. */
export function networkColorKey(name: string): string {
  return NETWORK_COLORS[hash(name) % NETWORK_COLORS.length].key;
}

/** Resolve an explicit key (if valid) else the name-derived fallback. */
export function resolveNetworkColor(
  colorKey: string | undefined | null,
  name: string,
): { bg: string; fg: string } {
  const chosen = colorKey && NETWORK_COLORS.find((c) => c.key === colorKey);
  const c = chosen || NETWORK_COLORS.find((x) => x.key === networkColorKey(name))!;
  return { bg: c.bg, fg: c.fg };
}
