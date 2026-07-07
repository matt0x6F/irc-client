import type { storage } from '../../wailsjs/go/models';

export type ActivityGroup = {
  key: string;
  networkId: number;
  sourceType: string;
  target: string;
  actor: string;
  items: storage.ActivityItem[];
  count: number;
  hasUnseen: boolean;
  latest: string;
};

// Invites stay one-row-each (keyed by id); everything else coalesces per (network, source, target).
function groupKey(i: storage.ActivityItem): string {
  if (i.source_type === 'invite') return `invite:${i.network_id}:${i.id}`;
  return `${i.source_type}:${i.network_id}:${i.target.toLowerCase()}`;
}

export function coalesceActivity(items: storage.ActivityItem[]): ActivityGroup[] {
  const map = new Map<string, ActivityGroup>();
  for (const i of items) {
    const key = groupKey(i);
    const g = map.get(key);
    if (!g) {
      map.set(key, {
        key, networkId: i.network_id, sourceType: i.source_type, target: i.target,
        actor: i.actor, items: [i], count: 1, hasUnseen: !i.seen, latest: i.timestamp,
      });
    } else {
      g.items.push(i);
      g.count += 1;
      g.hasUnseen = g.hasUnseen || !i.seen;
      if (i.timestamp >= g.latest) { g.latest = i.timestamp; g.actor = i.actor; g.target = i.target; }
    }
  }
  return [...map.values()].sort((a, b) => (a.latest < b.latest ? 1 : a.latest > b.latest ? -1 : (a.key < b.key ? -1 : a.key > b.key ? 1 : 0)));
}

export function unseenGroupCount(items: storage.ActivityItem[]): number {
  return coalesceActivity(items).filter((g) => g.hasUnseen).length;
}

export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  const mins = Math.max(0, Math.round((Date.now() - then) / 60000));
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  return `${Math.round(mins / 60)}h ago`;
}

export type Activation = { kind: 'jump' | 'openPane' | 'openChannel'; networkId: number; paneKey: string; msgid?: string };

export function activationFor(g: ActivityGroup): Activation {
  const newest = g.items.reduce((a, b) => (a.timestamp >= b.timestamp ? a : b));
  switch (g.sourceType) {
    case 'pm':
      return { kind: 'openPane', networkId: g.networkId, paneKey: `pm:${g.target}` };
    case 'invite':
      return { kind: 'openChannel', networkId: g.networkId, paneKey: g.target };
    default: // highlight, keyword
      return { kind: 'jump', networkId: g.networkId, paneKey: g.target, msgid: newest.msgid || undefined };
  }
}
