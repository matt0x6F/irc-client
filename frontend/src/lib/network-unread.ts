/**
 * Total unread across all of a network's panes. unreadCounts is keyed
 * `${networkId}:${channel}`; we match on the exact numeric-id prefix so
 * network 1 never absorbs network 10's counts.
 */
export function networkUnreadTotal(
  unreadCounts: Map<string, number>,
  networkId: number,
): number {
  const prefix = `${networkId}:`;
  let total = 0;
  for (const [key, count] of unreadCounts) {
    if (key.startsWith(prefix)) total += count;
  }
  return total;
}
