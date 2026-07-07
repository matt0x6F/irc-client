import { describe, it, expect } from 'vitest';

// expandInvite is the pure helper extracted from the send path.
import { expandInvite } from '../invite-command';

describe('expandInvite', () => {
  it('appends the active channel when only a user is given', () => {
    expect(expandInvite('/invite bob', '#dev')).toBe('/invite bob #dev');
  });
  it('passes through when a channel is already provided', () => {
    expect(expandInvite('/invite bob #other', '#dev')).toBe('/invite bob #other');
  });
  it('returns an error sentinel in a non-channel pane with one arg', () => {
    expect(expandInvite('/invite bob', 'status')).toEqual({ error: expect.any(String) });
    expect(expandInvite('/invite bob', 'pm:carol')).toEqual({ error: expect.any(String) });
    expect(expandInvite('/invite bob', 'activity')).toEqual({ error: expect.any(String) });
  });
  it('ignores non-invite commands', () => {
    expect(expandInvite('/join #x', '#dev')).toBeNull();
  });
});
