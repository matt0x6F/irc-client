import { describe, it, expect, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetInvites: vi.fn().mockResolvedValue([
    { inviter: 'alice', channel: '#a', trusted: true, receivedAt: '2026-06-28T12:00:00Z' },
  ]),
}));

import { useNetworkStore } from '../network';

describe('invites slice', () => {
  it('loadInvites populates invitesByNetwork', async () => {
    await useNetworkStore.getState().loadInvites(1);
    expect(useNetworkStore.getState().invitesByNetwork[1]).toHaveLength(1);
    expect(useNetworkStore.getState().invitesByNetwork[1][0].inviter).toBe('alice');
  });
});
