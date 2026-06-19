import { describe, it, expect } from 'vitest';
import { formatCommandHelp, formatHelpList } from './help-format';
import { main } from '../../wailsjs/go/models';

const mk = (o: Partial<main.CommandInfo>): main.CommandInfo =>
  ({ name: 'X', aliases: [], category: 'server', usage: '', description: '', source: '', ...o } as main.CommandInfo);

describe('help-format', () => {
  it('formats a single command', () => {
    const lines = formatCommandHelp(mk({ name: 'JOIN', aliases: ['J'], usage: '#channel [key]', description: 'Join a channel' }));
    expect(lines.join('\n')).toContain('/join #channel [key]');
    expect(lines.join('\n')).toContain('Join a channel');
    expect(lines.join('\n')).toContain('J');
  });
  it('groups the full list by category', () => {
    const lines = formatHelpList([
      mk({ name: 'JOIN', category: 'server' }),
      mk({ name: 'QUERY', category: 'client' }),
      mk({ name: 'WEATHER', category: 'plugin', source: 'weather-plugin' }),
    ]);
    const text = lines.join('\n');
    expect(text).toMatch(/Client commands/i);
    expect(text).toMatch(/Server commands/i);
    expect(text).toMatch(/weather-plugin/i);
  });
});
