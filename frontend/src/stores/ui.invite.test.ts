import { describe, it, expect, beforeEach } from 'vitest';
import { useUIStore } from './ui';

describe('useUIStore.inviteTo', () => {
  beforeEach(() => useUIStore.getState().setInviteTo(null));

  it('sets and clears the invite target', () => {
    expect(useUIStore.getState().inviteTo).toBeNull();
    useUIStore.getState().setInviteTo({ networkId: 2, nick: 'bob', channel: '#x' });
    expect(useUIStore.getState().inviteTo).toEqual({ networkId: 2, nick: 'bob', channel: '#x' });
    useUIStore.getState().setInviteTo(null);
    expect(useUIStore.getState().inviteTo).toBeNull();
  });
});
