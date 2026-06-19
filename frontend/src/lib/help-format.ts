import { main } from '../../wailsjs/go/models';

export function formatCommandHelp(cmd: main.CommandInfo): string[] {
  const lines = [`/${cmd.name.toLowerCase()} ${cmd.usage}`.trim()];
  if (cmd.description) lines.push(`  ${cmd.description}`);
  if (cmd.aliases && cmd.aliases.length) {
    lines.push(`  Aliases: ${cmd.aliases.map((a) => '/' + a.toLowerCase()).join(', ')}`);
  }
  return lines;
}

export function formatHelpList(commands: main.CommandInfo[]): string[] {
  const lines: string[] = [];
  const section = (title: string, cmds: main.CommandInfo[]) => {
    if (!cmds.length) return;
    lines.push(`— ${title} —`);
    for (const c of [...cmds].sort((a, b) => a.name.localeCompare(b.name))) {
      lines.push(`/${c.name.toLowerCase()} ${c.usage}`.trim() + (c.description ? `  ${c.description}` : ''));
    }
  };
  section('Client commands', commands.filter((c) => c.category === 'client'));
  section('Server commands', commands.filter((c) => c.category === 'server'));
  section('CTCP commands', commands.filter((c) => c.category === 'ctcp'));

  const plugin = commands.filter((c) => c.category === 'plugin');
  const bySource = new Map<string, main.CommandInfo[]>();
  for (const c of plugin) {
    const arr = bySource.get(c.source) || [];
    arr.push(c);
    bySource.set(c.source, arr);
  }
  for (const [source, cmds] of bySource) section(`Plugin: ${source}`, cmds);
  return lines;
}
