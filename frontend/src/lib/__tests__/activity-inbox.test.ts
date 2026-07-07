import { describe, it, expect } from 'vitest';
import { coalesceActivity, unseenGroupCount, activationFor } from '../activity-inbox';

const item = (o: Partial<any>) => ({ id: 1, network_id: 1, source_type: 'highlight', target: '#dev', actor: 'a', preview: 'p', msgid: '', keyword: '', seen: false, timestamp: '2026-07-06T12:00:00Z', trusted: false, expires_at: null, ...o });

describe('coalesceActivity', () => {
  it('groups highlights per channel and PMs per person; invites stay individual', () => {
    const groups = coalesceActivity([
      item({ id: 1, source_type: 'highlight', target: '#dev', timestamp: '2026-07-06T12:00:00Z' }),
      item({ id: 2, source_type: 'highlight', target: '#dev', timestamp: '2026-07-06T12:01:00Z' }),
      item({ id: 3, source_type: 'pm', target: 'bob', timestamp: '2026-07-06T12:02:00Z' }),
      item({ id: 4, source_type: 'invite', target: '#ops', timestamp: '2026-07-06T12:03:00Z' }),
      item({ id: 5, source_type: 'invite', target: '#qa', timestamp: '2026-07-06T12:04:00Z' }),
    ]);
    const dev = groups.find((g) => g.sourceType === 'highlight' && g.target === '#dev')!;
    expect(dev.count).toBe(2);
    expect(groups.filter((g) => g.sourceType === 'invite')).toHaveLength(2); // not merged
    // reverse-chron: newest group (invite #qa @ 12:04) first
    expect(groups[0].target).toBe('#qa');
  });

  it('groups are keyed by network too', () => {
    const groups = coalesceActivity([
      item({ id: 1, network_id: 1, target: '#dev' }),
      item({ id: 2, network_id: 2, target: '#dev' }),
    ]);
    expect(groups).toHaveLength(2);
  });

  it('returns groups in deterministic order when latest timestamps tie', () => {
    const input = [
      item({ id: 1, source_type: 'highlight', target: '#a', timestamp: '2026-07-06T12:00:00Z' }),
      item({ id: 2, source_type: 'highlight', target: '#b', timestamp: '2026-07-06T12:00:00Z' }),
    ];
    const first = coalesceActivity(input).map((g) => g.key);
    const second = coalesceActivity(input).map((g) => g.key);
    expect(first).toEqual(second);
    expect(first).toEqual([...first].sort());
  });

  it('coalesces mixed-case targets into one group', () => {
    const groups = coalesceActivity([
      item({ id: 1, source_type: 'highlight', target: '#Dev', timestamp: '2026-07-06T12:00:00Z' }),
      item({ id: 2, source_type: 'highlight', target: '#dev', timestamp: '2026-07-06T12:01:00Z' }),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].count).toBe(2);
  });
});

describe('unseenGroupCount', () => {
  it('counts groups that have at least one unseen item', () => {
    expect(unseenGroupCount([
      item({ id: 1, target: '#dev', seen: true }),
      item({ id: 2, target: '#dev', seen: false }),
      item({ id: 3, source_type: 'pm', target: 'bob', seen: true }),
    ])).toBe(1);
  });

  it('returns 0 when every item is seen', () => {
    expect(unseenGroupCount([
      item({ id: 1, target: '#dev', seen: true }),
      item({ id: 2, source_type: 'pm', target: 'bob', seen: true }),
    ])).toBe(0);
  });
});

describe('activationFor', () => {
  it('highlight → jump with msgid + channel pane', () => {
    const g = coalesceActivity([item({ source_type: 'highlight', target: '#dev', msgid: 'm9' })])[0];
    expect(activationFor(g)).toMatchObject({ kind: 'jump', networkId: 1, paneKey: '#dev', msgid: 'm9' });
  });
  it('pm → open the pm pane', () => {
    const g = coalesceActivity([item({ source_type: 'pm', target: 'bob' })])[0];
    expect(activationFor(g)).toMatchObject({ kind: 'openPane', paneKey: 'pm:bob' });
  });
  it('invite → open the channel', () => {
    const g = coalesceActivity([item({ source_type: 'invite', target: '#ops' })])[0];
    expect(activationFor(g)).toMatchObject({ kind: 'openChannel', paneKey: '#ops' });
  });
});
