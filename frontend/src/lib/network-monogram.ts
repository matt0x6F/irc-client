/**
 * A 1–2 letter monogram derived from a network name, shown on rail tiles that
 * have no custom icon. Two+ words → first letter of the first two words; one
 * word → its first two letters; empty → "?".
 */
export function networkMonogram(name: string): string {
  const words = name.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  if (words.length === 0) return '?';
  if (words.length === 1) {
    const w = words[0];
    return (w.slice(0, 2)).replace(/^./, (c) => c.toUpperCase());
  }
  return (words[0][0] + words[1][0]).toUpperCase();
}
