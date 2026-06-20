import { create } from 'zustand';
import { storage, main } from '../../wailsjs/go/models';
import { markdownToIrc } from '../lib/irc-markup';
import {
  GetNetworks,
  GetConnectionStatus,
  GetCurrentNick,
  ConnectNetwork,
  DisconnectNetwork,
  DeleteNetwork,
  GetMessages,
  GetMessagesAround,
  GetMessagesAfter,
  GetMessagesBeforeTime,
  GetPrivateMessages,
  GetChannelIDByName,
  RequestChatHistoryBefore,
  RequestChatHistoryLatest,
  GetChannelInfo,
  GetOpenChannels,
  GetLastOpenPane,
  SetPaneFocus,
  ClearPaneFocus,
  SetChannelOpen,
  SetPrivateMessageOpen,
  GetPrivateMessageConversations,
  GetPinnedMessages,
  PinMessage,
  UnpinMessage,
  SendMessage,
  SendCommand,
  GetNetworkBots,
  GetMonitorList,
  GetMonitorPresence,
  AddMonitor,
  RemoveMonitor,
  GetNetworkUserMeta,
  PrintLocalLines,
} from '../../wailsjs/go/main/App';
import { useCommandsStore, lookupCommand } from './commands';
import { usePreferencesStore } from './preferences';
import { useUIStore } from './ui';
import { formatCommandHelp, formatHelpList } from '../lib/help-format';

// UserMetaT mirrors the Go irc.UserMeta JSON shape: the live, session-local
// roster attributes Cascade tracks per nick via away-notify / account-notify /
// extended-join / chghost / account-tag.
export interface UserMetaT {
  away: boolean;
  away_message: string;
  account: string;
  host: string;
  realname: string;
}

// One entry on a network's MONITOR buddy list: a monitored nick and whether it
// is currently online (per the latest 730/731 from the server).
export interface MonitorBuddy {
  nick: string;
  online: boolean;
}

// How many messages of surrounding context to load when jumping to a pinned message.
const JUMP_WINDOW = 50;

// How many older messages to fetch per scrollback page.
const SCROLLBACK_PAGE = 100;

// Optimistic (just-sent) messages use Date.now() as a placeholder id until the
// real DB row loads. Never paginate past one — its id isn't a real row boundary.
const OPTIMISTIC_ID_THRESHOLD = 1_000_000_000_000;

// Give up waiting for a CHATHISTORY reply after this long, so a silent/unsupported
// server can't wedge scrollback pagination (loadOlderMessages would never resolve).
const HISTORY_REQUEST_TIMEOUT_MS = 8000;

// A scroll-driven CHATHISTORY request in flight. loadOlderMessages parks its
// resolver here when the local store is exhausted; the history-event handler
// (onHistoryReceived) re-queries the now-backfilled store, prepends the rows, and
// resolves with how many were added — so the message-view's existing scroll-
// preservation path works identically for local and server-fetched history.
// Module-level (not store state) to avoid re-renders on every transition.
let pendingHistoryWaiter:
  | { key: string; resolve: (added: number) => void; timer: ReturnType<typeof setTimeout> }
  | null = null;

function clearHistoryWaiter(): void {
  if (pendingHistoryWaiter) {
    clearTimeout(pendingHistoryWaiter.timer);
    pendingHistoryWaiter = null;
  }
}

// Resolve the CHATHISTORY target for a buffer: a channel name (#foo), a PM nick,
// or null for panes that have no server-side history (status). Returns the IRC
// target plus the local-query routing (channelId for channels, pmTarget for PMs).
function historyTargetFor(channel: string | null): { target: string; isPM: boolean } | null {
  if (!channel || channel === 'status') return null;
  if (channel.startsWith('pm:')) return { target: channel.substring(3), isPM: true };
  return { target: channel, isPM: false };
}

// Does a (null-channel) message belong to the private-message conversation with `user`?
// Mirrors the backend GetPrivateMessages matching, which keys PM rows by pm_target
// (the conversation peer). Falls back to the legacy raw_line heuristic for rows
// written before the pm_target column existed and not yet backfilled.
function messageBelongsToPM(msg: storage.Message, user: string): boolean {
  const target = user.toLowerCase();
  if (msg.pm_target) return msg.pm_target.toLowerCase() === target;
  // Legacy fallback (pm_target missing): received from the user, or sent to them.
  if (msg.user.toLowerCase() === target) return true;
  return (msg.raw_line || '').toLowerCase().includes(`privmsg ${target}`);
}

// Sort messages chronologically (oldest first) as a safety net
function sortByTimestamp(msgs: storage.Message[]): storage.Message[] {
  return [...msgs].sort((a, b) => {
    const ta = new Date(a.timestamp).getTime();
    const tb = new Date(b.timestamp).getTime();
    if (ta !== tb) return ta - tb;
    return a.id - b.id; // stable sort by ID for same timestamp
  });
}

interface NetworkState {
  // Data
  networks: storage.Network[];
  connectionStatus: Record<number, boolean>;
  connectionStatusAt: Record<number, number>; // last-applied event time (ms) per network
  currentNick: Record<number, string>; // server-assigned nick per network; differs from the configured nick during a collision
  messages: storage.Message[];
  channelInfo: main.ChannelInfo | null;
  unreadCounts: Map<string, number>;

  // Bot mode (IRCv3): nicks recognized as bots this session, per network.
  // Keys are lowercased nicks; the set is in-memory and re-accrues per session.
  botNicks: Record<number, Set<string>>;

  // MONITOR (IRCv3): the durable per-network buddy list with live presence.
  // The list (membership + display case) is loaded from the backend; presence
  // toggles arrive via 'monitor-event'.
  monitor: Record<number, MonitorBuddy[]>;

  // Live MONITOR presence for every tracked nick (lowercased nick -> online),
  // including auto-monitored PM correspondents that are not durable buddies.
  // Drives the DM-list dots; seeded from GetMonitorPresence and kept fresh by
  // the same 'monitor-event' the buddy list listens for.
  presence: Record<number, Record<string, boolean>>;

  // Live roster (IRCv3 away-notify / account-notify / extended-join / chghost /
  // account-tag): per-network map of lowercased nick -> attributes. In-memory,
  // re-accrues per session.
  userMeta: Record<number, Record<string, UserMetaT>>;

  // Pinned messages / jump-to-message
  pinnedMessages: storage.PinnedMessage[];
  viewMode: 'live' | 'anchored'; // 'anchored' = viewing a context window, live updates paused
  anchoredMessageId: number | null; // message id to scroll to + flash
  newSinceAnchor: number; // count of messages that arrived while anchored

  // CHATHISTORY scrollback
  loadingHistory: boolean; // a server history fetch is in flight (drives the top spinner)
  reachedStart: boolean; // the server reported no more history for the current buffer

  // Selection
  selectedNetwork: number | null;
  selectedChannel: string | null;

  // Loading
  loadNetworks: () => Promise<void>;
  loadMessages: () => Promise<void>;
  loadOlderMessages: () => Promise<number>;
  loadNewerMessages: () => Promise<number>;
  onHistoryReceived: (target: string, inserted: number) => boolean;
  loadChannelInfo: () => Promise<void>;
  loadConnectionStatus: (networkId?: number) => Promise<void>;
  refreshAllConnectionStatus: () => Promise<void>;
  loadCurrentNick: (networkId?: number) => Promise<void>;

  // Pinned message actions
  loadPinnedMessages: () => Promise<void>;
  pinMessage: (messageId: number) => Promise<void>;
  unpinMessage: (messageId: number) => Promise<void>;
  jumpToMessage: (messageId: number) => Promise<void>;
  returnToLive: () => Promise<void>;
  clearAnchorFlash: () => void;
  noteNewWhileAnchored: () => void;

  // Selection actions
  setSelectedNetwork: (id: number | null) => void;
  setSelectedChannel: (channel: string | null) => void;
  selectPane: (networkId: number, channel: string | null) => Promise<void>;

  // Network actions
  connectNetwork: (config: main.NetworkConfig) => Promise<void>;
  disconnectNetwork: (networkId: number) => Promise<void>;
  deleteNetwork: (networkId: number) => Promise<void>;

  // Message actions
  sendMessage: (message: string) => Promise<void>;

  // Activity tracking
  markActivity: (key: string) => void;
  clearActivity: (key: string) => void;
  clearNetworkActivity: (networkId: number) => void;

  // Connection status
  setConnectionStatus: (networkId: number, connected: boolean, at?: number) => void;
  setCurrentNick: (networkId: number, nick: string) => void;

  // Bot mode
  loadNetworkBots: (networkId?: number) => Promise<void>;
  addBot: (networkId: number, nick: string) => void;
  isBot: (networkId: number, nick: string) => boolean;

  // MONITOR buddy list
  loadMonitor: (networkId?: number) => Promise<void>;
  addMonitorNick: (networkId: number, nick: string) => Promise<void>;
  removeMonitorNick: (networkId: number, nick: string) => Promise<void>;
  setMonitorOnline: (networkId: number, nick: string, online: boolean) => void;

  // MONITOR presence map (drives DM-list dots)
  loadPresence: (networkId: number) => Promise<void>;
  setPresence: (networkId: number, nick: string, online: boolean) => void;

  // Live roster metadata
  loadNetworkUserMeta: (networkId?: number) => Promise<void>;
  setUserMeta: (networkId: number, nick: string, meta: UserMetaT) => void;
  getUserMeta: (networkId: number, nick: string) => UserMetaT | undefined;
  isAway: (networkId: number, nick: string) => boolean;

  // Pane restoration
  restoreLastPane: () => Promise<void>;

  // DM / query
  openQuery: (networkId: number, nick: string) => Promise<void>;
}

export const useNetworkStore = create<NetworkState>((set, get) => ({
  networks: [],
  connectionStatus: {},
  connectionStatusAt: {},
  currentNick: {},
  messages: [],
  channelInfo: null,
  unreadCounts: new Map(),
  botNicks: {},
  monitor: {},
  presence: {},
  userMeta: {},
  pinnedMessages: [],
  viewMode: 'live',
  anchoredMessageId: null,
  newSinceAnchor: 0,
  loadingHistory: false,
  reachedStart: false,
  selectedNetwork: null,
  selectedChannel: null,

  loadNetworks: async () => {
    try {
      const networkList = await GetNetworks();
      const nets = networkList || [];
      set({ networks: nets });

      if (nets.length > 0) {
        const statusPromises = nets.map(async (network) => {
          try {
            const connected = await GetConnectionStatus(network.id);
            return { networkId: network.id, connected };
          } catch {
            return { networkId: network.id, connected: false };
          }
        });
        const statuses = await Promise.all(statusPromises);
        const statusMap: Record<number, boolean> = {};
        statuses.forEach(({ networkId, connected }) => {
          statusMap[networkId] = connected;
        });
        set((state) => ({
          connectionStatus: { ...state.connectionStatus, ...statusMap },
        }));
      }
    } catch (error) {
      console.error('Failed to load networks:', error);
      set({ networks: [] });
    }
  },

  loadMessages: async () => {
    const { selectedNetwork, selectedChannel, viewMode } = get();
    if (selectedNetwork === null) return;
    // While anchored to a pinned/old message, suppress reloads so the polling
    // in App.tsx and message events don't snap the view back to the latest 100.
    if (viewMode === 'anchored') return;

    // The pane can be switched while a load is in flight (the App.tsx poll, message
    // events, and selection changes all race — and each load is an async round-trip).
    // Discard results whose pane is no longer selected so a stale load can't clobber
    // the new channel's messages (which would also defeat the message-view's
    // scroll-to-latest on switch).
    const isStale = () =>
      get().selectedNetwork !== selectedNetwork || get().selectedChannel !== selectedChannel;

    try {
      if (selectedChannel === null || selectedChannel === 'status') {
        const msgs = await GetMessages(selectedNetwork, null, 100);
        if (isStale()) return;
        set({ messages: sortByTimestamp(msgs || []) });
        return;
      }

      if (selectedChannel.startsWith('pm:')) {
        const user = selectedChannel.substring(3);
        const msgs = await GetPrivateMessages(selectedNetwork, user, 100);
        if (isStale()) return;
        set({ messages: sortByTimestamp(msgs || []) });
        return;
      }

      const channelId = await GetChannelIDByName(selectedNetwork, selectedChannel);
      const msgs = await GetMessages(selectedNetwork, channelId as number, 100);
      if (isStale()) return;
      set({ messages: sortByTimestamp(msgs || []) });
    } catch (error) {
      console.error('Failed to load messages:', error);
      if (isStale()) return;
      set({ messages: [] });
    }
  },

  loadOlderMessages: async (): Promise<number> => {
    const { selectedNetwork, selectedChannel, messages } = get();
    if (selectedNetwork === null || messages.length === 0) return 0;

    const oldest = messages[0];
    // Don't paginate past an optimistic (not-yet-persisted) placeholder row.
    if (!oldest || oldest.id >= OPTIMISTIC_ID_THRESHOLD) return 0;

    const hist = historyTargetFor(selectedChannel);

    try {
      // Local-query routing: pmTarget for PMs, channelId for channels (status = both nil).
      let channelId: number | null = null;
      let pmTarget = '';
      if (hist?.isPM) {
        pmTarget = hist.target;
      } else if (selectedChannel && selectedChannel !== 'status') {
        channelId = (await GetChannelIDByName(selectedNetwork, selectedChannel)) as number;
      }

      // Page by the oldest loaded message's server-time timestamp (not id) so that
      // CHATHISTORY-backfilled rows (high id, old timestamp) are included.
      const beforeISO = new Date(oldest.timestamp).toISOString();
      const older =
        (await GetMessagesBeforeTime(
          selectedNetwork,
          channelId,
          pmTarget,
          beforeISO,
          SCROLLBACK_PAGE
        )) || [];

      const seen = new Set(get().messages.map((m) => m.id));
      const fresh = older.filter((m) => !seen.has(m.id));

      if (fresh.length > 0) {
        // Prepend history and enter 'anchored' mode so the live poll (which reloads
        // the latest 100) can't discard the older messages we just loaded. The
        // existing scroll-to-bottom badge returns the user to live.
        set((state) => ({
          messages: sortByTimestamp([...fresh, ...state.messages]),
          viewMode: 'anchored',
        }));
        return fresh.length;
      }

      // Local store exhausted. If the server supports CHATHISTORY and we haven't
      // already reached the start, request older history and resolve once the
      // replay lands (onHistoryReceived re-queries + prepends + resolves). Status
      // panes (hist === null) have no server-side history.
      if (hist && !get().reachedStart && !get().loadingHistory) {
        return await new Promise<number>((resolve) => {
          clearHistoryWaiter();
          const key = `${selectedNetwork}:${selectedChannel}`;
          const timer = setTimeout(() => {
            // No reply in time: drop the spinner but don't mark reachedStart (the
            // server may just be slow) — a later scroll-to-top retries.
            if (pendingHistoryWaiter && pendingHistoryWaiter.key === key) {
              pendingHistoryWaiter = null;
              set({ loadingHistory: false });
              resolve(0);
            }
          }, HISTORY_REQUEST_TIMEOUT_MS);
          pendingHistoryWaiter = { key, resolve, timer };
          set({ loadingHistory: true });
          RequestChatHistoryBefore(selectedNetwork, hist.target, beforeISO, SCROLLBACK_PAGE).catch(
            (err) => {
              console.error('CHATHISTORY BEFORE request failed:', err);
              if (pendingHistoryWaiter && pendingHistoryWaiter.key === key) {
                clearHistoryWaiter();
                set({ loadingHistory: false, reachedStart: true });
                resolve(0);
              }
            }
          );
        });
      }

      return 0;
    } catch (error) {
      console.error('Failed to load older messages:', error);
      return 0;
    }
  },

  // Called by the App-level history-event subscription when a CHATHISTORY replay
  // for `target` has been stored (`inserted` = new rows). If a scroll-driven
  // request is parked for the active buffer, re-query the now-backfilled local
  // store, prepend the older rows, and resolve loadOlderMessages()'s promise with
  // the count — so the message-view preserves the viewport just like a local page.
  onHistoryReceived: (target, inserted) => {
    const waiter = pendingHistoryWaiter;
    if (!waiter) return false;

    const { selectedNetwork, selectedChannel } = get();
    if (selectedNetwork === null) return false;
    const hist = historyTargetFor(selectedChannel);
    if (!hist || waiter.key !== `${selectedNetwork}:${selectedChannel}`) return false;
    if (target && hist.target.toLowerCase() !== target.toLowerCase()) return false;

    clearHistoryWaiter();

    (async () => {
      let added = 0;
      try {
        const { messages } = get();
        const oldest = messages[0];
        if (oldest && oldest.id < OPTIMISTIC_ID_THRESHOLD) {
          let channelId: number | null = null;
          let pmTarget = '';
          if (hist.isPM) {
            pmTarget = hist.target;
          } else {
            channelId = (await GetChannelIDByName(selectedNetwork, selectedChannel!)) as number;
          }
          const beforeISO = new Date(oldest.timestamp).toISOString();
          const older =
            (await GetMessagesBeforeTime(
              selectedNetwork,
              channelId,
              pmTarget,
              beforeISO,
              SCROLLBACK_PAGE
            )) || [];
          const seen = new Set(get().messages.map((m) => m.id));
          const fresh = older.filter((m) => !seen.has(m.id));
          if (fresh.length > 0) {
            set((state) => ({
              messages: sortByTimestamp([...fresh, ...state.messages]),
              viewMode: 'anchored',
            }));
            added = fresh.length;
          }
        }
      } catch (err) {
        console.error('Failed to load backfilled history:', err);
      } finally {
        // inserted===0 means the server has no more history before our cursor.
        set({ loadingHistory: false, reachedStart: inserted === 0 });
        waiter.resolve(added);
      }
    })();

    return true;
  },

  loadNewerMessages: async (): Promise<number> => {
    const { selectedNetwork, selectedChannel, messages, viewMode } = get();
    // Only meaningful while anchored (viewing a window that may not reach live).
    if (viewMode !== 'anchored') return 0;
    if (selectedNetwork === null || messages.length === 0) return 0;
    if (selectedChannel && selectedChannel.startsWith('pm:')) return 0;

    const newest = messages[messages.length - 1];
    if (!newest || newest.id >= OPTIMISTIC_ID_THRESHOLD) return 0;

    try {
      let channelId: number | null = null;
      if (selectedChannel && selectedChannel !== 'status') {
        channelId = (await GetChannelIDByName(selectedNetwork, selectedChannel)) as number;
      }
      const newer =
        (await GetMessagesAfter(selectedNetwork, channelId, newest.id, SCROLLBACK_PAGE)) || [];

      const seen = new Set(get().messages.map((m) => m.id));
      const fresh = newer.filter((m) => !seen.has(m.id));

      if (fresh.length === 0) {
        // No more newer rows: the loaded window now extends to the live tip.
        // Resume live (badge clears, new messages append) without a reload/jump.
        set({ viewMode: 'live', anchoredMessageId: null, newSinceAnchor: 0 });
        return 0;
      }

      // Append below the current view (no scroll adjustment needed for appends).
      set((state) => ({ messages: [...state.messages, ...fresh] }));
      return fresh.length;
    } catch (error) {
      console.error('Failed to load newer messages:', error);
      return 0;
    }
  },

  loadChannelInfo: async () => {
    const { selectedNetwork, selectedChannel } = get();
    if (
      selectedNetwork === null ||
      selectedChannel === null ||
      selectedChannel === 'status' ||
      selectedChannel.startsWith('pm:')
    ) {
      set({ channelInfo: null });
      return;
    }
    try {
      const info = await GetChannelInfo(selectedNetwork, selectedChannel);
      set({ channelInfo: info });
    } catch (error) {
      console.error('Failed to load channel info:', error);
      set({ channelInfo: null });
    }
  },

  loadConnectionStatus: async (networkId?: number) => {
    const id = networkId ?? get().selectedNetwork;
    if (id === null) return;
    try {
      const connected = await GetConnectionStatus(id);
      set((state) => ({
        connectionStatus: { ...state.connectionStatus, [id]: connected },
      }));
    } catch (error) {
      console.error('Failed to load connection status:', error);
    }
  },

  refreshAllConnectionStatus: async () => {
    // Authoritative poll over every network (O(N) IPC per tick — fine for realistic
    // network counts). Writes connectionStatus directly and intentionally does NOT
    // update connectionStatusAt, so it never raises the out-of-order-event watermark.
    const { networks } = get();
    const results = await Promise.all(
      networks.map(async (n) => {
        try {
          return { id: n.id, connected: await GetConnectionStatus(n.id) };
        } catch {
          return { id: n.id, connected: false };
        }
      }),
    );
    set((state) => {
      const next = { ...state.connectionStatus };
      results.forEach(({ id, connected }) => {
        next[id] = connected;
      });
      return { connectionStatus: next };
    });
  },

  loadCurrentNick: async (networkId?: number) => {
    const id = networkId ?? get().selectedNetwork;
    if (id === null) return;
    try {
      const nick = await GetCurrentNick(id);
      if (nick) {
        set((state) => ({
          currentNick: { ...state.currentNick, [id]: nick },
        }));
      }
    } catch (error) {
      console.error('Failed to load current nick:', error);
    }
  },

  loadPinnedMessages: async () => {
    const { selectedNetwork, selectedChannel } = get();
    if (selectedNetwork === null) {
      set({ pinnedMessages: [] });
      return;
    }
    try {
      // Channel pane: filter by channel_id at the DB level (clean).
      if (
        selectedChannel &&
        selectedChannel !== 'status' &&
        !selectedChannel.startsWith('pm:')
      ) {
        const channelId = await GetChannelIDByName(selectedNetwork, selectedChannel);
        const pins = await GetPinnedMessages(selectedNetwork, channelId as number);
        set({ pinnedMessages: pins || [] });
        return;
      }

      // PM pane: pins live under channel_id NULL (shared with status + other PMs),
      // so fetch all null-channel pins and filter to this conversation client-side.
      if (selectedChannel && selectedChannel.startsWith('pm:')) {
        const user = selectedChannel.substring(3);
        const pins = await GetPinnedMessages(selectedNetwork, null);
        set({ pinnedMessages: (pins || []).filter((p) => messageBelongsToPM(p, user)) });
        return;
      }

      // Status pane: no pinned sidebar.
      set({ pinnedMessages: [] });
    } catch (error) {
      console.error('Failed to load pinned messages:', error);
      set({ pinnedMessages: [] });
    }
  },

  pinMessage: async (messageId) => {
    const { selectedNetwork, selectedChannel, loadPinnedMessages } = get();
    if (selectedNetwork === null) return;
    try {
      let channelId: number | null = null;
      if (
        selectedChannel &&
        selectedChannel !== 'status' &&
        !selectedChannel.startsWith('pm:')
      ) {
        channelId = (await GetChannelIDByName(selectedNetwork, selectedChannel)) as number;
      }
      await PinMessage(selectedNetwork, messageId, channelId);
      await loadPinnedMessages();
    } catch (error) {
      console.error('Failed to pin message:', error);
    }
  },

  unpinMessage: async (messageId) => {
    try {
      await UnpinMessage(messageId);
      await get().loadPinnedMessages();
    } catch (error) {
      console.error('Failed to unpin message:', error);
    }
  },

  jumpToMessage: async (messageId) => {
    const { selectedNetwork, selectedChannel, messages } = get();
    if (selectedNetwork === null) return;

    // Already loaded? Just anchor + flash, no reload (and freeze live updates).
    if (messages.some((m) => m.id === messageId)) {
      set({ viewMode: 'anchored', anchoredMessageId: messageId, newSinceAnchor: 0 });
      return;
    }

    try {
      let window: storage.Message[];
      if (selectedChannel && selectedChannel.startsWith('pm:')) {
        // PMs are conversation-filtered, so load a generous slice of the conversation
        // rather than a raw null-channel id-window (which would mix other PMs/status).
        const user = selectedChannel.substring(3);
        window = (await GetPrivateMessages(selectedNetwork, user, 500)) || [];
      } else {
        let channelId: number | null = null;
        if (selectedChannel && selectedChannel !== 'status') {
          channelId = (await GetChannelIDByName(selectedNetwork, selectedChannel)) as number;
        }
        window =
          (await GetMessagesAround(selectedNetwork, channelId, messageId, JUMP_WINDOW)) || [];
      }
      set({
        messages: sortByTimestamp(window),
        viewMode: 'anchored',
        anchoredMessageId: messageId,
        newSinceAnchor: 0,
      });
    } catch (error) {
      console.error('Failed to jump to message:', error);
    }
  },

  returnToLive: async () => {
    set({ viewMode: 'live', anchoredMessageId: null, newSinceAnchor: 0 });
    await get().loadMessages();
  },

  clearAnchorFlash: () => set({ anchoredMessageId: null }),

  noteNewWhileAnchored: () =>
    set((state) => ({ newSinceAnchor: state.newSinceAnchor + 1 })),

  setSelectedNetwork: (id) => set({ selectedNetwork: id }),
  setSelectedChannel: (channel) => {
    // Channel changes invalidate any in-flight scrollback history request and
    // reset CHATHISTORY pagination state for the new buffer.
    clearHistoryWaiter();
    set({ selectedChannel: channel, loadingHistory: false, reachedStart: false });
  },

  selectPane: async (networkId, channel) => {
    const prev = get();

    // A pending scrollback history request belongs to the pane we're leaving.
    clearHistoryWaiter();

    // Switching panes always returns to the live view and clears any anchor, and
    // resets CHATHISTORY pagination state for the freshly-selected buffer.
    set({
      selectedNetwork: networkId,
      selectedChannel: channel,
      viewMode: 'live',
      anchoredMessageId: null,
      newSinceAnchor: 0,
      loadingHistory: false,
      reachedStart: false,
    });

    // PM/query panes aren't "joined", so the backend's on-JOIN catch-up doesn't
    // cover them — request recent history when opening one. Channels are covered
    // server-side on JOIN. No-op if the server lacks CHATHISTORY; replays dedupe
    // by msgid so re-opening a pane won't duplicate messages.
    if (channel && channel.startsWith('pm:')) {
      RequestChatHistoryLatest(networkId, channel.substring(3), SCROLLBACK_PAGE).catch(() => {
        /* server may not support chathistory; ignore */
      });
    }

    // Clear activity for selected channel
    if (channel && channel !== 'status') {
      const activityKey = `${networkId}:${channel}`;
      set((state) => {
        const next = new Map(state.unreadCounts);
        next.delete(activityKey);
        return { unreadCounts: next };
      });
    }

    // Set focus on the backend
    if (channel !== null) {
      try {
        if (channel === 'status') {
          await SetPaneFocus(networkId, 'status', 'status');
        } else if (channel.startsWith('pm:')) {
          await SetPaneFocus(networkId, 'pm', channel.substring(3));
        } else {
          await SetPaneFocus(networkId, 'channel', channel);
        }
      } catch (error) {
        console.error('Failed to set focus on pane:', error);
      }
    }
  },

  connectNetwork: async (config) => {
    const { networks, connectionStatus, loadNetworks } = get();
    const existingNetwork = networks.find(
      (n) => (n.address === config.address && n.port === config.port) || n.name === config.name
    );
    if (existingNetwork && connectionStatus[existingNetwork.id]) {
      return;
    }
    try {
      await ConnectNetwork(config);
      await loadNetworks();
    } catch (error) {
      console.error('Failed to connect:', error);
      throw error;
    }
  },

  disconnectNetwork: async (networkId) => {
    try {
      await DisconnectNetwork(networkId);
      await get().loadNetworks();
    } catch (error) {
      console.error('Failed to disconnect:', error);
      throw error;
    }
  },

  deleteNetwork: async (networkId) => {
    try {
      await DeleteNetwork(networkId);
      await get().loadNetworks();
      const { selectedNetwork } = get();
      if (selectedNetwork === networkId) {
        set({ selectedNetwork: null, selectedChannel: null });
      }
      // Clear activity for deleted network
      get().clearNetworkActivity(networkId);
    } catch (error) {
      console.error('Failed to delete:', error);
      throw error;
    }
  },

  sendMessage: async (message) => {
    const { selectedNetwork, selectedChannel, networks, loadMessages } = get();
    if (selectedNetwork === null || selectedChannel === null) return;

    const trimmedMessage = message.trim();

    // Slash commands
    if (trimmedMessage.startsWith('/')) {
      let commandToSend = trimmedMessage;

      // /help is handled entirely client-side (it owns the cached registry).
      const helpMatch = trimmedMessage.match(/^\/help(?:\s+(\S+))?\s*$/i);
      if (helpMatch) {
        const target =
          selectedChannel === 'status'
            ? 'status'
            : selectedChannel.startsWith('pm:')
              ? selectedChannel.substring(3)
              : selectedChannel;
        const commands = useCommandsStore.getState().commands;
        const arg = helpMatch[1];
        if (arg) {
          const cmd = lookupCommand(commands, arg.replace(/^\//, ''));
          const lines = cmd ? formatCommandHelp(cmd) : [`Unknown command: ${arg}`];
          await PrintLocalLines(selectedNetwork, target, lines);
        } else if (usePreferencesStore.getState().helpDisplayMode === 'dialog') {
          useUIStore.getState().setHelpOpen(true);
        } else {
          await PrintLocalLines(selectedNetwork, target, formatHelpList(commands));
        }
        await loadMessages();
        return;
      }

      // Handle /me command — prepend target. The action text is formatting
      // markup like any other message, so convert it to IRC codes.
      if (trimmedMessage.toLowerCase().startsWith('/me ') && selectedChannel !== 'status') {
        const parts = markdownToIrc(trimmedMessage.substring(4).trim());
        if (selectedChannel.startsWith('pm:')) {
          commandToSend = `/me ${selectedChannel.substring(3)} ${parts}`;
        } else {
          commandToSend = `/me ${selectedChannel} ${parts}`;
        }
      }
      // Handle /part and /leave — inject current channel if none specified
      else if (
        (trimmedMessage.toLowerCase().startsWith('/part') ||
          trimmedMessage.toLowerCase().startsWith('/leave')) &&
        selectedChannel !== 'status'
      ) {
        const isPart = trimmedMessage.toLowerCase().startsWith('/part');
        const cmdLength = isPart ? 5 : 6;
        const rest = trimmedMessage.substring(cmdLength).trim();
        const parts = rest ? rest.split(/\s+/) : [];

        if (parts.length === 0 || (!parts[0].startsWith('#') && !parts[0].startsWith('&'))) {
          const cmd = isPart ? '/part' : '/leave';
          commandToSend =
            parts.length === 0
              ? `${cmd} ${selectedChannel}`
              : `${cmd} ${selectedChannel} ${parts.join(' ')}`;
        }
      }

      try {
        await SendCommand(selectedNetwork, commandToSend);
        await loadMessages();
      } catch (error) {
        console.error('Failed to send command:', error);
        await loadMessages();
      }

      // /query and /q navigate to the PM pane for the target nick.
      // /msg must NOT navigate (it only sends a one-off message).
      const queryMatch = trimmedMessage.match(/^\/(?:query|q)\s+(\S+)\s*$/i);
      if (queryMatch) {
        await get().selectPane(selectedNetwork, `pm:${queryMatch[1]}`);
      }

      return;
    }

    // Convert formatting markup (*bold*, _italic_, __underline__, #4(color)) to
    // IRC control codes at the wire boundary. History and the input keep the raw
    // markup; only what's sent and shown optimistically is converted.
    const wire = markdownToIrc(message);

    // Private messages
    if (selectedChannel.startsWith('pm:')) {
      const user = selectedChannel.substring(3);
      try {
        await SendMessage(selectedNetwork, user, wire);
        setTimeout(() => loadMessages(), 100);
      } catch (error) {
        console.error('Failed to send private message:', error);
      }
      return;
    }

    // Regular channel messages — optimistic UI
    const currentNetwork = networks.find((n) => n.id === selectedNetwork);
    if (currentNetwork && selectedChannel !== 'status') {
      try {
        const channelId = await GetChannelIDByName(selectedNetwork, selectedChannel);
        const optimisticMessage = storage.Message.createFrom({
          id: Date.now(),
          network_id: selectedNetwork,
          channel_id: channelId as number,
          user: currentNetwork.nickname || 'You',
          message: wire,
          message_type: 'privmsg',
          timestamp: new Date().toISOString(),
          raw_line: '',
        });
        set((state) => ({ messages: sortByTimestamp([...state.messages, optimisticMessage]) }));
      } catch {
        // Channel ID lookup failed, skip optimistic update
      }
    }

    try {
      if (selectedChannel === 'status') {
        await SendCommand(selectedNetwork, message);
        await loadMessages();
      } else {
        await SendMessage(selectedNetwork, selectedChannel, wire);
        setTimeout(() => loadMessages(), 100);
      }
    } catch (error) {
      console.error('Failed to send message:', error);
      await loadMessages();
    }
  },

  markActivity: (key) =>
    set((state) => {
      const next = new Map(state.unreadCounts);
      next.set(key, (next.get(key) || 0) + 1);
      return { unreadCounts: next };
    }),

  clearActivity: (key) =>
    set((state) => {
      const next = new Map(state.unreadCounts);
      next.delete(key);
      return { unreadCounts: next };
    }),

  clearNetworkActivity: (networkId) =>
    set((state) => {
      const next = new Map(state.unreadCounts);
      for (const key of state.unreadCounts.keys()) {
        if (key.startsWith(`${networkId}:`)) {
          next.delete(key);
        }
      }
      return { unreadCounts: next };
    }),

  setConnectionStatus: (networkId, connected, at) =>
    set((state) => {
      // Timestamped updates (events) may arrive out of order because the Go event
      // bus dispatches subscribers on unordered goroutines. Drop an event strictly
      // older than the last one applied for this network. Untimestamped updates
      // (the poll) are authoritative and always win. Equal timestamps are applied
      // (idempotent) so a same-instant correction is not lost.
      if (at !== undefined) {
        const lastAt = state.connectionStatusAt[networkId];
        if (lastAt !== undefined && at < lastAt) {
          return state;
        }
        return {
          connectionStatus: { ...state.connectionStatus, [networkId]: connected },
          connectionStatusAt: { ...state.connectionStatusAt, [networkId]: at },
        };
      }
      return {
        connectionStatus: { ...state.connectionStatus, [networkId]: connected },
      };
    }),

  setCurrentNick: (networkId, nick) =>
    set((state) => ({
      currentNick: { ...state.currentNick, [networkId]: nick },
    })),

  // Hydrate the bot set for a network from the backend (e.g. on window open or
  // network select). Live additions arrive via the 'bot-event' event -> addBot.
  loadNetworkBots: async (networkId?: number) => {
    const id = networkId ?? get().selectedNetwork;
    if (id === null) return;
    try {
      const nicks = await GetNetworkBots(id);
      set((state) => ({
        botNicks: { ...state.botNicks, [id]: new Set((nicks || []).map((n) => n.toLowerCase())) },
      }));
    } catch (error) {
      console.error('Failed to load network bots:', error);
    }
  },

  addBot: (networkId, nick) =>
    set((state) => {
      const key = nick.toLowerCase();
      const existing = state.botNicks[networkId];
      if (existing?.has(key)) return state; // no-op: avoid needless re-render
      const next = new Set(existing ?? []);
      next.add(key);
      return { botNicks: { ...state.botNicks, [networkId]: next } };
    }),

  isBot: (networkId, nick) =>
    get().botNicks[networkId]?.has(nick.toLowerCase()) ?? false,

  // Hydrate the MONITOR buddy list (membership + presence) for a network from the
  // backend. Live presence toggles arrive via 'monitor-event' -> setMonitorOnline.
  loadMonitor: async (networkId?: number) => {
    const id = networkId ?? get().selectedNetwork;
    if (id === null) return;
    try {
      const list = await GetMonitorList(id);
      const buddies: MonitorBuddy[] = (list || []).map((m) => ({
        nick: m.nick,
        online: !!m.online,
      }));
      set((state) => ({ monitor: { ...state.monitor, [id]: buddies } }));
    } catch (error) {
      console.error('Failed to load monitor list:', error);
    }
  },

  addMonitorNick: async (networkId, nick) => {
    const trimmed = nick.trim();
    if (!trimmed) return;
    await AddMonitor(networkId, trimmed);
    await get().loadMonitor(networkId);
  },

  removeMonitorNick: async (networkId, nick) => {
    await RemoveMonitor(networkId, nick);
    await get().loadMonitor(networkId);
  },

  setMonitorOnline: (networkId, nick, online) =>
    set((state) => {
      const list = state.monitor[networkId];
      if (!list) return state;
      const key = nick.toLowerCase();
      let changed = false;
      const next = list.map((b) => {
        if (b.nick.toLowerCase() === key && b.online !== online) {
          changed = true;
          return { ...b, online };
        }
        return b;
      });
      if (!changed) return state; // avoid needless re-renders
      return { monitor: { ...state.monitor, [networkId]: next } };
    }),

  // Seed the presence map for a network from the backend snapshot of every nick
  // currently tracked via MONITOR. Covers 730/731 replies that arrived before the
  // UI subscribed to 'monitor-event'. Keys are lowercased nicks.
  loadPresence: async (networkId) => {
    try {
      const snapshot = await GetMonitorPresence(networkId);
      const map: Record<string, boolean> = {};
      for (const [nick, online] of Object.entries(snapshot || {})) {
        map[nick.toLowerCase()] = !!online;
      }
      set((state) => ({ presence: { ...state.presence, [networkId]: map } }));
    } catch (error) {
      console.error('Failed to load MONITOR presence:', error);
    }
  },

  setPresence: (networkId, nick, online) =>
    set((state) => {
      const key = nick.toLowerCase();
      const current = state.presence[networkId];
      if (current && current[key] === online) return state; // no change
      const next: Record<string, boolean> = { ...(current ?? {}) };
      next[key] = online;
      return { presence: { ...state.presence, [networkId]: next } };
    }),

  // Hydrate the roster metadata for a network from the backend (e.g. on window
  // open or network select). Live changes arrive via 'usermeta-event' ->
  // setUserMeta. Keys from the backend are already lowercased.
  loadNetworkUserMeta: async (networkId?: number) => {
    const id = networkId ?? get().selectedNetwork;
    if (id === null) return;
    try {
      const meta = await GetNetworkUserMeta(id);
      const map: Record<string, UserMetaT> = {};
      for (const [nick, m] of Object.entries(meta || {})) {
        map[nick.toLowerCase()] = {
          away: !!m?.away,
          away_message: m?.away_message ?? '',
          account: m?.account ?? '',
          host: m?.host ?? '',
          realname: m?.realname ?? '',
        };
      }
      set((state) => ({ userMeta: { ...state.userMeta, [id]: map } }));
    } catch (error) {
      console.error('Failed to load network user metadata:', error);
    }
  },

  setUserMeta: (networkId, nick, meta) =>
    set((state) => {
      const key = nick.toLowerCase();
      const existing = state.userMeta[networkId]?.[key];
      // No-op when nothing changed, to avoid needless re-renders.
      if (
        existing &&
        existing.away === meta.away &&
        existing.away_message === meta.away_message &&
        existing.account === meta.account &&
        existing.host === meta.host &&
        existing.realname === meta.realname
      ) {
        return state;
      }
      const networkMeta = { ...(state.userMeta[networkId] ?? {}), [key]: meta };
      return { userMeta: { ...state.userMeta, [networkId]: networkMeta } };
    }),

  getUserMeta: (networkId, nick) =>
    get().userMeta[networkId]?.[nick.toLowerCase()],

  isAway: (networkId, nick) =>
    get().userMeta[networkId]?.[nick.toLowerCase()]?.away ?? false,

  openQuery: async (networkId, nick) => {
    try {
      await SetPrivateMessageOpen(networkId, nick, true);
    } catch (error) {
      console.error('Failed to open query:', error);
    }
    await get().selectPane(networkId, `pm:${nick}`);
  },

  restoreLastPane: async () => {
    const { networks } = get();
    if (networks.length === 0) return;

    try {
      const lastPane = await GetLastOpenPane();
      if (!lastPane) {
        set({ selectedNetwork: networks[0].id, selectedChannel: 'status' });
        try {
          await SetPaneFocus(networks[0].id, 'status', 'status');
        } catch {}
        return;
      }

      const networkExists = networks.some((n) => n.id === lastPane.network_id);
      if (!networkExists) {
        set({ selectedNetwork: networks[0].id, selectedChannel: 'status' });
        return;
      }

      if (lastPane.type === 'channel') {
        // Try to verify channel exists with retries
        let found = false;
        for (let attempt = 0; attempt < 5; attempt++) {
          try {
            await GetChannelIDByName(lastPane.network_id, lastPane.name);
            set({ selectedNetwork: lastPane.network_id, selectedChannel: lastPane.name });
            await SetPaneFocus(lastPane.network_id, 'channel', lastPane.name);
            found = true;
            break;
          } catch {
            await new Promise((r) => setTimeout(r, 300));
          }
        }

        if (!found) {
          // Try case-insensitive fallback
          try {
            const openChannels = await GetOpenChannels(lastPane.network_id);
            const match = openChannels.find(
              (ch) => ch.name.toLowerCase() === lastPane.name.toLowerCase()
            );
            if (match) {
              set({ selectedNetwork: lastPane.network_id, selectedChannel: match.name });
              await SetPaneFocus(lastPane.network_id, 'channel', match.name);
              found = true;
            }
          } catch {}
        }

        if (!found) {
          set({ selectedNetwork: lastPane.network_id, selectedChannel: 'status' });
          try {
            await SetPaneFocus(lastPane.network_id, 'status', 'status');
          } catch {}
        }
      } else if (lastPane.type === 'pm') {
        set({ selectedNetwork: lastPane.network_id, selectedChannel: `pm:${lastPane.name}` });
        try {
          await SetPaneFocus(lastPane.network_id, 'pm', lastPane.name);
        } catch {}
      }
    } catch (error) {
      console.error('Failed to restore last open pane:', error);
      if (networks.length > 0) {
        set({ selectedNetwork: networks[0].id, selectedChannel: 'status' });
      }
    }
  },
}));
