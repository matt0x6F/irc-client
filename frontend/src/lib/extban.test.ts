import { describe, it, expect } from 'vitest';
import { describeBan } from './extban';

describe('describeBan', () => {
  const prefix = '$'; // server advertised EXTBAN=$,a...

  it('labels an account extban with the account name', () => {
    expect(describeBan('$a:alice', prefix)).toEqual({
      kind: 'account',
      label: 'account: alice',
      mask: '$a:alice',
    });
  });

  it('labels a bare account extban (any logged-in user)', () => {
    expect(describeBan('$a', prefix)).toEqual({
      kind: 'account',
      label: 'any logged-in account',
      mask: '$a',
    });
  });

  it('leaves a plain hostmask untouched', () => {
    expect(describeBan('nick!user@host', prefix)).toEqual({
      kind: 'mask',
      label: 'nick!user@host',
      mask: 'nick!user@host',
    });
  });

  it('labels a non-account extban generically as an extban', () => {
    expect(describeBan('$r:realname', prefix)).toEqual({
      kind: 'extban',
      label: 'extban: $r:realname',
      mask: '$r:realname',
    });
  });

  it('treats a "$"-prefixed mask as a plain mask when no extban prefix is advertised', () => {
    expect(describeBan('$a:alice', '')).toEqual({
      kind: 'mask',
      label: '$a:alice',
      mask: '$a:alice',
    });
  });
});
