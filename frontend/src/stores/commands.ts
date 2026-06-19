import { create } from 'zustand';
import { GetCommands } from '../../wailsjs/go/main/App';
import { main } from '../../wailsjs/go/models';

interface CommandsState {
  commands: main.CommandInfo[];
  loadCommands: () => Promise<void>;
}

export const useCommandsStore = create<CommandsState>((set) => ({
  commands: [],
  loadCommands: async () => {
    try {
      const cmds = await GetCommands();
      set({ commands: Array.isArray(cmds) ? cmds : [] });
    } catch (error) {
      console.error('Failed to load commands:', error);
    }
  },
}));

export async function initCommands(): Promise<void> {
  await useCommandsStore.getState().loadCommands();
}

/** Filter commands whose name or any alias starts with prefix (no leading slash). */
export function filterCommands(commands: main.CommandInfo[], prefix: string): main.CommandInfo[] {
  const p = prefix.toLowerCase();
  const matches = commands.filter(
    (c) =>
      c.name.toLowerCase().startsWith(p) ||
      (c.aliases || []).some((a) => a.toLowerCase().startsWith(p))
  );
  return matches.sort((a, b) => a.name.localeCompare(b.name));
}

/** Find the canonical spec for a typed command word (name or alias), or null. */
export function lookupCommand(commands: main.CommandInfo[], word: string): main.CommandInfo | null {
  const w = word.toLowerCase();
  return (
    commands.find(
      (c) => c.name.toLowerCase() === w || (c.aliases || []).some((a) => a.toLowerCase() === w)
    ) || null
  );
}
