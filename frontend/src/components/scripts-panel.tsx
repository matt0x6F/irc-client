// frontend/src/components/scripts-panel.tsx
import { useEffect, useState } from 'react';
import { useScriptsStore } from '../stores/scripts';
import { main } from '../../wailsjs/go/models';

// statusBadge maps an extension status string to a label + Tailwind classes.
// Status values come from internal/extension: loaded | disabled | error | runaway.
function statusBadge(status: string): { label: string; className: string } {
  switch (status) {
    case 'loaded':
      return { label: 'Loaded', className: 'bg-green-500/15 text-green-600 dark:text-green-400' };
    case 'disabled':
      return { label: 'Disabled', className: 'bg-muted text-muted-foreground' };
    case 'runaway':
      return { label: 'Runaway', className: 'bg-destructive/15 text-destructive' };
    case 'error':
      return { label: 'Error', className: 'bg-destructive/15 text-destructive' };
    default:
      return { label: status || 'Unknown', className: 'bg-muted text-muted-foreground' };
  }
}

function ScriptRow({ script }: { script: main.ScriptInfo }) {
  const busy = useScriptsStore((s) => s.busy.has(script.id));
  const enable = useScriptsStore((s) => s.enable);
  const disable = useScriptsStore((s) => s.disable);
  const reload = useScriptsStore((s) => s.reload);
  const badge = statusBadge(script.status);

  return (
    <div className="border border-border rounded p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <h4 className="font-semibold">{script.name}</h4>
          <span className={`px-2 py-0.5 text-xs rounded ${badge.className}`}>{badge.label}</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => reload(script.id)}
            disabled={busy}
            className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Reload
          </button>
          <button
            onClick={() => (script.enabled ? disable(script.id) : enable(script.id))}
            disabled={busy}
            className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {script.enabled ? 'Disable' : 'Enable'}
          </button>
        </div>
      </div>
      {script.description && (
        <p className="text-sm text-muted-foreground mb-2">{script.description}</p>
      )}
      {script.perms && script.perms.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-2">
          {script.perms.map((p) => (
            <span key={p} className="px-2 py-0.5 text-xs rounded bg-muted text-muted-foreground">{p}</span>
          ))}
        </div>
      )}
      {script.error && (
        <p className="text-sm text-destructive break-words">{script.error}</p>
      )}
    </div>
  );
}

export function ScriptsPanel() {
  const scripts = useScriptsStore((s) => s.scripts);
  const error = useScriptsStore((s) => s.error);
  const lastCreatedPath = useScriptsStore((s) => s.lastCreatedPath);
  const fetch = useScriptsStore((s) => s.fetch);
  const create = useScriptsStore((s) => s.create);
  const openDir = useScriptsStore((s) => s.openDir);
  const clearCreated = useScriptsStore((s) => s.clearCreated);
  const [name, setName] = useState('');

  useEffect(() => {
    void fetch();
  }, [fetch]);

  const onCreate = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    void create(trimmed);
    setName('');
  };

  return (
    <div className="mb-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-md font-semibold">Scripts</h3>
        <button
          onClick={() => openDir()}
          className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
        >
          Open scripts folder
        </button>
      </div>

      {/* New script */}
      <div className="flex items-center gap-2 mb-3">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') onCreate(); }}
          placeholder="script name"
          className="flex-1 px-3 py-1.5 text-sm border border-border rounded-lg bg-background"
        />
        <button
          onClick={onCreate}
          className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
        >
          New script
        </button>
      </div>

      {lastCreatedPath && (
        <div className="flex items-center justify-between gap-2 mb-3 p-2 border border-border rounded bg-muted/30">
          <code className="text-xs break-all">{lastCreatedPath}</code>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button
              onClick={() => openDir()}
              className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-accent transition-all"
            >
              Reveal in folder
            </button>
            <button
              onClick={() => clearCreated()}
              className="px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {error && <p className="text-sm text-destructive mb-3">{error}</p>}

      {scripts.length === 0 ? (
        <div className="text-center text-muted-foreground py-8">
          No scripts yet. Create one above, then edit its <code>.go</code> file.
        </div>
      ) : (
        <div className="space-y-3">
          {scripts.map((s) => (
            <ScriptRow key={s.id} script={s} />
          ))}
        </div>
      )}
    </div>
  );
}
