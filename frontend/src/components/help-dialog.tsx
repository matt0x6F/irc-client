import { useState } from 'react';
import { useUIStore } from '../stores/ui';
import { useCommandsStore } from '../stores/commands';
import { main } from '../../wailsjs/go/models';

const CATEGORY_TITLES: Record<string, string> = {
  client: 'Client commands',
  server: 'Server commands',
  ctcp: 'CTCP commands',
  plugin: 'Plugin commands',
};

export function HelpDialog() {
  const open = useUIStore((s) => s.helpOpen);
  const setOpen = useUIStore((s) => s.setHelpOpen);
  const commands = useCommandsStore((s) => s.commands);
  const [query, setQuery] = useState('');

  if (!open) return null;

  const q = query.toLowerCase();
  const filtered = commands.filter(
    (c) =>
      c.name.toLowerCase().includes(q) ||
      c.description.toLowerCase().includes(q) ||
      (c.aliases || []).some((a) => a.toLowerCase().includes(q))
  );
  const order = ['client', 'server', 'ctcp', 'plugin'];
  const grouped = order
    .map((cat) => ({ cat, items: filtered.filter((c) => c.category === cat) }))
    .filter((g) => g.items.length);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setOpen(false)}>
      <div className="w-[36rem] max-h-[80vh] overflow-hidden rounded-lg border border-border bg-background shadow-[var(--shadow-lg)] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="p-4 border-b border-border">
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search commands…"
            className="w-full px-3 py-2 rounded-md border border-border bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary"
          />
        </div>
        <div className="overflow-y-auto p-2">
          {grouped.map((g) => (
            <div key={g.cat} className="mb-3">
              <div className="px-2 py-1 text-xs uppercase tracking-wide text-muted-foreground">{CATEGORY_TITLES[g.cat]}</div>
              {g.items.map((c: main.CommandInfo) => (
                <div key={`${c.source}:${c.name}`} className="px-2 py-1.5 rounded hover:bg-accent/60">
                  <div className="text-sm font-medium">/{c.name.toLowerCase()} <span className="text-muted-foreground font-normal">{c.usage}</span></div>
                  {c.description ? <div className="text-xs text-muted-foreground">{c.description}{c.source ? ` · ${c.source}` : ''}</div> : null}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
