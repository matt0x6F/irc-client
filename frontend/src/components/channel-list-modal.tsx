import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { RotateCw } from 'lucide-react';
import {
  GetCachedChannelList,
  RequestChannelList,
  SendCommand,
} from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

interface ChannelListModalProps {
  networkId: number;
  onClose: () => void;
}

interface ChannelListEntry {
  channel: string;
  users: number;
  topic: string;
  networkId: number;
}

type SortField = 'channel' | 'users';
type SortDirection = 'asc' | 'desc';

// Cached LIST results older than this are considered stale: the modal still shows
// them instantly on open, but kicks off a background refresh. This is the single
// authority for the fresh-vs-stale decision (the backend only stamps fetchedAt).
const CHANNEL_LIST_TTL_MS = 15 * 60 * 1000;

// formatAgo renders a fetch timestamp as a short, human-readable "time since" string.
export function formatAgo(ageMs: number): string {
  if (ageMs < 10_000) return 'just now';
  const seconds = Math.floor(ageMs / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

// mapEntries normalizes both cached and freshly-emitted channel payloads (same shape)
// into typed entries, defaulting the network id when an item omits it.
function mapEntries(items: any[], networkId: number): ChannelListEntry[] {
  return (items || []).map((item: any) => ({
    channel: item.channel || '',
    users: item.users || 0,
    topic: item.topic || '',
    networkId: item.networkId || networkId,
  }));
}

export function ChannelListModal({ networkId, onClose }: ChannelListModalProps) {
  const [channels, setChannels] = useState<ChannelListEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const [sortField, setSortField] = useState<SortField>('users');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');
  const [joiningChannel, setJoiningChannel] = useState<string | null>(null);
  const [fetchedAt, setFetchedAt] = useState<number | null>(null);
  // Re-render tick so the "Updated X ago" label stays current while the modal is open.
  const [, setTick] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  // Ensures the initial fetch decision runs exactly once per open. React StrictMode
  // (dev) double-invokes effects; two concurrent LIST requests share one backend
  // accumulation buffer, and the second LISTEND emits an empty list that overwrites
  // the populated one — making the modal always show "No channels".
  const requestedRef = useRef(false);

  // Force-fetch a fresh list from the server. Used for stale-cache background
  // refreshes, cache misses, and the manual refresh button.
  const doRefresh = useCallback(() => {
    setRefreshing(true);
    setError(null);
    RequestChannelList(networkId).catch((err) => {
      setError(`Failed to request channel list: ${err}`);
      setRefreshing(false);
      setLoading(false);
    });
  }, [networkId]);

  // Focus input on mount
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Keyboard handler for Escape
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  // Keep the "Updated X ago" label fresh while the modal is open.
  useEffect(() => {
    if (fetchedAt == null) return;
    const id = setInterval(() => setTick((t) => t + 1), 30_000);
    return () => clearInterval(id);
  }, [fetchedAt]);

  // Subscribe to LIST results and seed from cache on open.
  useEffect(() => {
    const unsubscribe = EventsOn('channel-list', (data: any) => {
      const eventData = data?.data;
      if (!eventData) return;
      if (eventData.networkId !== networkId) return;

      setChannels(mapEntries(eventData.channels || [], networkId));
      setFetchedAt(Date.now());
      setLoading(false);
      setRefreshing(false);
      setError(null);
    });

    // Initial open decision — runs exactly once per open (see requestedRef).
    if (!requestedRef.current) {
      requestedRef.current = true;
      GetCachedChannelList(networkId)
        .then((res) => {
          if (res.found) {
            // Render cached rows instantly; refresh in the background if stale.
            setChannels(mapEntries(res.channels || [], networkId));
            setFetchedAt(res.fetchedAt);
            setLoading(false);
            if (Date.now() - res.fetchedAt > CHANNEL_LIST_TTL_MS) {
              doRefresh();
            }
          } else {
            // Cache miss — full fetch.
            setLoading(true);
            doRefresh();
          }
        })
        .catch(() => {
          // If the cache read itself fails, fall back to a plain fetch.
          setLoading(true);
          doRefresh();
        });
    }

    return () => unsubscribe();
  }, [networkId, doRefresh]);

  // Filter and sort channels
  const filteredChannels = useMemo(() => {
    let result = channels;

    if (filter.trim()) {
      const lowerFilter = filter.toLowerCase();
      result = result.filter(
        (ch) =>
          ch.channel.toLowerCase().includes(lowerFilter) ||
          ch.topic.toLowerCase().includes(lowerFilter)
      );
    }

    result.sort((a, b) => {
      let cmp = 0;
      if (sortField === 'channel') {
        cmp = a.channel.localeCompare(b.channel);
      } else if (sortField === 'users') {
        cmp = a.users - b.users;
      }
      return sortDirection === 'asc' ? cmp : -cmp;
    });

    return result;
  }, [channels, filter, sortField, sortDirection]);

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortDirection(field === 'users' ? 'desc' : 'asc');
    }
  };

  const handleJoin = async (channelName: string) => {
    setJoiningChannel(channelName);
    try {
      await SendCommand(networkId, `/join ${channelName}`);
      onClose();
    } catch (err) {
      setError(`Failed to join ${channelName}: ${err}`);
      setJoiningChannel(null);
    }
  };

  const sortIndicator = (field: SortField) => {
    if (sortField !== field) return null;
    return sortDirection === 'asc' ? ' ▲' : ' ▼';
  };

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="bg-card border border-border rounded-lg shadow-lg w-[1040px] max-w-[92vw] max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <h2 className="text-lg font-semibold">Browse Channels</h2>
          <div className="flex items-center gap-2">
            {fetchedAt !== null && (
              <span className="text-xs text-muted-foreground tabular-nums">
                Updated {formatAgo(Date.now() - fetchedAt)}
              </span>
            )}
            <button
              onClick={doRefresh}
              disabled={refreshing}
              title="Refresh channel list"
              className="text-muted-foreground hover:text-foreground cursor-pointer p-1 rounded hover:bg-accent/50 transition-colors disabled:opacity-50 disabled:cursor-default"
            >
              <RotateCw size={16} className={refreshing ? 'animate-spin' : ''} />
            </button>
            <button
              onClick={onClose}
              className="text-muted-foreground hover:text-foreground cursor-pointer p-1 rounded hover:bg-accent/50 transition-colors"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M18 6 6 18" />
                <path d="m6 6 12 12" />
              </svg>
            </button>
          </div>
        </div>

        {/* Filter */}
        <div className="px-5 py-3 border-b border-border">
          <input
            ref={inputRef}
            type="text"
            placeholder="Filter by channel name or topic..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="w-full px-3 py-2 rounded-md border border-border bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/50 text-sm"
          />
        </div>

        {/* Error banner — non-blocking when cached rows are present */}
        {error && (
          <div className="px-5 py-2 border-b border-border bg-destructive/10 text-destructive text-sm">
            {error}
          </div>
        )}

        {/* Content */}
        <div className="flex-1 overflow-y-auto min-h-0">
          {loading && channels.length === 0 && !error && (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <svg
                className="animate-spin h-8 w-8 mb-3"
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
              >
                <circle
                  className="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  strokeWidth="4"
                />
                <path
                  className="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
                />
              </svg>
              <span className="text-sm">Loading channel list...</span>
            </div>
          )}

          {!loading && !error && channels.length === 0 && (
            <div className="flex items-center justify-center py-16 text-muted-foreground text-sm">
              No channels found.
            </div>
          )}

          {channels.length > 0 && (
            <table className="w-full text-sm table-fixed">
              <thead className="sticky top-0 bg-card border-b border-border">
                <tr>
                  <th
                    className="text-left px-5 py-2 font-medium text-muted-foreground cursor-pointer hover:text-foreground select-none w-56"
                    onClick={() => handleSort('channel')}
                  >
                    Channel{sortIndicator('channel')}
                  </th>
                  <th
                    className="text-right px-4 py-2 font-medium text-muted-foreground cursor-pointer hover:text-foreground select-none w-20"
                    onClick={() => handleSort('users')}
                  >
                    Users{sortIndicator('users')}
                  </th>
                  <th className="text-left px-4 py-2 font-medium text-muted-foreground">
                    Topic
                  </th>
                </tr>
              </thead>
              <tbody>
                {filteredChannels.map((ch) => (
                  <tr
                    key={ch.channel}
                    className="border-b border-border/50 hover:bg-accent/30 cursor-pointer transition-colors"
                    onClick={() => handleJoin(ch.channel)}
                    title={`Click to join ${ch.channel}`}
                  >
                    <td className="px-5 py-2 font-medium text-primary truncate">
                      {joiningChannel === ch.channel ? (
                        <span className="text-muted-foreground italic">Joining...</span>
                      ) : (
                        ch.channel
                      )}
                    </td>
                    <td className="text-right px-4 py-2 text-muted-foreground tabular-nums">
                      {ch.users}
                    </td>
                    <td className="px-4 py-2 text-muted-foreground truncate" title={ch.topic}>
                      {ch.topic}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* Footer */}
        {channels.length > 0 && (
          <div className="px-5 py-2 border-t border-border text-xs text-muted-foreground">
            {filteredChannels.length} of {channels.length} channels
            {filter.trim() ? ' (filtered)' : ''}
          </div>
        )}
      </div>
    </div>
  );
}
