import { useEffect, useState } from 'react';
import { X } from 'lucide-react';
import { useNetworkStore } from '../stores/network';
import { buddyPresence } from '../lib/presence';
import { casefold } from '../lib/casefold';

interface MonitorListProps {
  networkId: number | null;
}

// MonitorList renders a network's IRCv3 MONITOR buddy list: monitored nicks with
// a live online/offline indicator, plus controls to add and remove buddies. The
// list is durable per network (persisted backend-side); presence is updated live
// via the 'monitor-event' the store listens for.
export function MonitorList({ networkId }: MonitorListProps) {
  const buddies = useNetworkStore((s) => (networkId !== null ? s.monitor[networkId] : undefined));
  // While the network is disconnected the server tracks no one, so we can't claim
  // a buddy is online/offline/away — every dot renders neutral ("unknown"), the
  // same honest fallback the DM list uses. Only force this when we KNOW we're
  // disconnected (=== false); undefined (status not yet known) leaves the live dots.
  const disconnected = useNetworkStore(
    (s) => networkId !== null && s.connectionStatus[networkId] === false,
  );
  // Per-network roster metadata: with extended-monitor this carries live away
  // state for monitored buddies even when we share no channel with them.
  const userMeta = useNetworkStore((s) => (networkId !== null ? s.userMeta[networkId] : undefined));
  const caseMapping = useNetworkStore((s) => (networkId !== null ? s.caseMapping?.[networkId] : undefined)) ?? '';
  const loadMonitor = useNetworkStore((s) => s.loadMonitor);
  const addMonitorNick = useNetworkStore((s) => s.addMonitorNick);
  const removeMonitorNick = useNetworkStore((s) => s.removeMonitorNick);
  const [input, setInput] = useState('');

  useEffect(() => {
    if (networkId !== null) loadMonitor(networkId);
  }, [networkId, loadMonitor]);

  const list = buddies ?? [];
  const onlineCount = list.filter((b) => b.online).length;

  const handleAdd = async () => {
    const nick = input.trim();
    if (!nick || networkId === null) return;
    setInput('');
    await addMonitorNick(networkId, nick);
  };

  return (
    <div className="flex flex-col h-full" data-testid="monitor-list">
      <div className="px-1 pb-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-muted-foreground/80">
        {disconnected ? `Buddies (${list.length})` : `Buddies (${onlineCount}/${list.length} online)`}
      </div>

      {/* Add a buddy */}
      <div className="flex gap-1 px-1 pb-2">
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void handleAdd();
          }}
          placeholder="Add nick…"
          aria-label="Add a nick to monitor"
          disabled={networkId === null}
          className="flex-1 min-w-0 rounded-md bg-background border border-border px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          onClick={() => void handleAdd()}
          disabled={networkId === null || input.trim() === ''}
          className="rounded-md px-2 py-1 text-sm bg-accent hover:bg-accent/70 disabled:opacity-40 cursor-pointer"
        >
          Add
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {list.map((b) => {
          const meta = userMeta?.[casefold(caseMapping, b.nick)];
          const presence = buddyPresence(b.online, meta?.away ?? false);
          // Disconnected -> a hollow neutral ring (no presence claim). Otherwise the
          // solid live dot: green online, amber away, grey offline.
          const dotClass = disconnected
            ? 'border border-muted-foreground/40 bg-transparent'
            : presence === 'online'
              ? 'bg-green-500'
              : presence === 'away'
                ? 'bg-amber-500'
                : 'bg-muted-foreground/40';
          const awayMsg = meta?.away_message?.trim();
          const dotTitle = disconnected
            ? 'Presence unknown — disconnected'
            : presence === 'away'
              ? (awayMsg ? `Away — ${awayMsg}` : 'Away')
              : presence === 'online'
                ? 'Online'
                : 'Offline';
          return (
          <div
            key={b.nick}
            className="group flex items-center gap-2 text-sm py-1.5 px-2 rounded-md hover:bg-accent/70"
          >
            <span
              className={`w-2 h-2 rounded-full flex-shrink-0 ${dotClass}`}
              title={dotTitle}
            />
            <span className={`font-medium truncate flex-1 ${disconnected || !b.online ? 'opacity-60' : ''}`}>{b.nick}</span>
            <button
              onClick={() => networkId !== null && void removeMonitorNick(networkId, b.nick)}
              title={`Stop monitoring ${b.nick}`}
              aria-label={`Stop monitoring ${b.nick}`}
              className="opacity-0 group-hover:opacity-100 focus:opacity-100 text-muted-foreground hover:text-foreground flex-shrink-0 cursor-pointer"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
          );
        })}
        {list.length === 0 && (
          <div className="text-sm text-muted-foreground px-2">No buddies yet — add a nick to track when they come online.</div>
        )}
      </div>
    </div>
  );
}
