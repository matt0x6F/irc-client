import { describe, it, expect } from 'vitest';
import { filterCommands } from './commands';
import { main } from '../../wailsjs/go/models';

const mk = (name: string, aliases: string[] = [], category = 'server'): main.CommandInfo =>
  ({ name, aliases, category, usage: '', description: '', source: '' } as main.CommandInfo);

describe('filterCommands', () => {
  const cmds = [mk('JOIN', ['J']), mk('QUERY', ['Q']), mk('QUIT')];
  it('matches by name prefix, case-insensitive', () => {
    expect(filterCommands(cmds, 'qu').map(c => c.name)).toEqual(['QUERY', 'QUIT']);
  });
  it('matches aliases', () => {
    expect(filterCommands(cmds, 'j').map(c => c.name)).toEqual(['JOIN']);
  });
  it('returns all on empty prefix', () => {
    expect(filterCommands(cmds, '').length).toBe(3);
  });
});
