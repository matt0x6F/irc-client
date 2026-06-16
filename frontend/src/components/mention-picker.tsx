import { useEffect, useRef, useState } from 'react';
import { AtSign } from 'lucide-react';
import { GetChannelInfo } from '../../wailsjs/go/main/App';
import { storage } from '../../wailsjs/go/models';
import { useDismiss } from '../hooks/useDismiss';

/**
 * Member picker. Loads the channel's users (same call tab-completion uses) when
 * opened, filters them, and emits the chosen nick. Disabled for the status
 * window and PM buffers, which have no member list.
 */
export function MentionPicker({
  networkId,
  channelName,
  onPick,
}: {
  networkId?: number | null;
  channelName?: string | null;
  onPick: (nick: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [nicks, setNicks] = useState<string[]>([]);
  const [filter, setFilter] = useState('');
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(open, () => setOpen(false), ref);

  const disabled =
    !networkId || !channelName || channelName === 'status' || channelName.startsWith('pm:');

  useEffect(() => {
    if (!open || disabled) return;
    let cancelled = false;
    (async () => {
      try {
        const info = await GetChannelInfo(networkId!, channelName!);
        const users = (info?.users || []) as storage.ChannelUser[];
        if (!cancelled) {
          setNicks(
            users
              .map((u) => u.nickname)
              .sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase())),
          );
        }
      } catch (e) {
        console.error('mention picker: failed to load members', e);
      }
    })();
    return () => { cancelled = true; };
  }, [open, disabled, networkId, channelName]);

  const filtered = nicks.filter((n) => n.toLowerCase().includes(filter.toLowerCase()));

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        type="button"
        title="Mention a member"
        disabled={disabled}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setOpen((v) => !v)}
        className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/50 transition-colors disabled:opacity-40 disabled:cursor-default"
      >
        <AtSign size={16} />
      </button>

      {open && (
        <div className="absolute bottom-full left-0 mb-2 z-50 w-52 p-2 rounded-lg border border-border bg-card shadow-[var(--shadow-lg)]">
          <input
            autoFocus
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter members…"
            className="w-full mb-2 px-2 py-1 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
          />
          <div className="max-h-48 overflow-y-auto">
            {filtered.length === 0 ? (
              <div className="px-2 py-1 text-sm text-muted-foreground">No members</div>
            ) : (
              filtered.map((n) => (
                <button
                  key={n}
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => { onPick(n); setOpen(false); setFilter(''); }}
                  className="block w-full text-left px-2 py-1 text-sm rounded hover:bg-accent/60 transition-colors"
                >
                  {n}
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
