import { describe, it, expect } from 'vitest';
import { describeMode, type ModeContext } from './chanmodes';

// A Libera/Solanum-shaped context: the letters live in their advertised CHANMODES
// classes (D = boolean flags, C = param-on-set, B = always-param, A = list).
const solanum: ModeContext = {
  family: 'solanum',
  classA: 'eIbq',
  classB: 'k',
  classC: 'flj',
  classD: 'CFLMPQcgimnprstuz',
};

describe('describeMode', () => {
  it('labels a family-specific flag from the family map', () => {
    expect(describeMode('Q', solanum)).toEqual({
      label: 'No forwarding into here',
      known: true,
    });
  });

  it('labels a near-universal mode from the base map even when family is unknown', () => {
    const ctx: ModeContext = { family: '', classA: 'b', classB: 'k', classC: 'l', classD: 'imnstC' };
    expect(describeMode('n', ctx)).toEqual({ label: 'No external messages', known: true });
  });

  it('prefers the family meaning over the generic one for letters that differ per-ircd', () => {
    // +p is "No KNOCK" on Solanum (not the classic "Private").
    expect(describeMode('p', solanum)).toEqual({ label: 'No KNOCK', known: true });
  });

  it('labels a parameterized family mode in its correct class', () => {
    expect(describeMode('f', solanum)).toEqual({ label: 'Forward channel', known: true });
  });

  it('falls back to a typed generic label for an unknown letter', () => {
    const ctx: ModeContext = { family: 'solanum', classA: '', classB: '', classC: '', classD: 'X' };
    expect(describeMode('X', ctx)).toEqual({ label: 'Flag', known: false });
  });

  it('uses the parameter generic label for an unknown param letter', () => {
    const ctx: ModeContext = { family: 'solanum', classA: '', classB: '', classC: 'Y', classD: '' };
    expect(describeMode('Y', ctx)).toEqual({ label: 'Parameter', known: false });
  });

  it('suppresses a known label when the server advertises a different class (type guard)', () => {
    // Map knows +f as a type-C "Forward channel"; this server (wrongly) lists f as a
    // boolean flag, so the meaning almost certainly differs — fall back, do not mislabel.
    const ctx: ModeContext = { family: 'solanum', classA: '', classB: '', classC: '', classD: 'f' };
    expect(describeMode('f', ctx)).toEqual({ label: 'Flag', known: false });
  });

  it('falls back to a bare generic label when the letter is in no class', () => {
    const ctx: ModeContext = { family: '', classA: '', classB: '', classC: '', classD: '' };
    expect(describeMode('W', ctx)).toEqual({ label: 'Mode', known: false });
  });
});
