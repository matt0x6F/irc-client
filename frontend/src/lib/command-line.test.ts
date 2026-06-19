import { describe, it, expect } from 'vitest';
import { parseCommandLine } from './command-line';

describe('parseCommandLine', () => {
  it('detects a bare command being typed', () => {
    expect(parseCommandLine('/jo')).toEqual({ isCommand: true, word: 'jo', afterCommandName: false });
  });
  it('detects arguments started', () => {
    expect(parseCommandLine('/join ')).toEqual({ isCommand: true, word: 'join', afterCommandName: true });
  });
  it('is not a command without a leading slash', () => {
    expect(parseCommandLine('hello').isCommand).toBe(false);
  });
  it('ignores a lone slash mid-text', () => {
    expect(parseCommandLine('a/b').isCommand).toBe(false);
  });
});
