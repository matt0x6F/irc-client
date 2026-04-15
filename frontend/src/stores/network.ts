import { create } from 'zustand';
import { storage, main } from '../../wailsjs/go/models';
import {
  GetNetworks,
  GetConnectionStatus,
  ConnectNetwork,
  DisconnectNetwork,
  DeleteNetwork,
  GetMessages,
  GetPrivateMessages,
  GetChannelIDByName,
  GetChannelInfo,
  GetOpenChannels,
  GetLastOpenPane,
  SetPaneFocus,
  ClearPaneFocus,
  SetChannelOpen,
  SetPrivateMessageOpen,
  GetPrivateMessageConversations,
  SendMessage,
  SendCommand,
} from '../../wailsjs/go/main/App';

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
  messages: storage.Message[];
  channelInfo: main.ChannelInfo | null;
  unreadCounts: Map<string, number>;

  // Selection
  selectedNetwork: number | null;
  selectedChannel: string | null;

  // Loading
  loadNetworks: () => Promise<void>;
  loadMessages: () => Promise<void>;
  loadChannelInfo: () => Promise<void>;
  loadConnectionStatus: (networkId?: number) => Promise<void>;

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
  setConnectionStatus: (networkId: number, connected: boolean) => void;

  // Pane restoration
  restoreLastPane: () => Promise<void>;
}

export const useNetworkStore = create<NetworkState>((set, get) => ({
  networks: [],
  connectionStatus: {},
  messages: [],
  channelInfo: null,
  unreadCounts: new Map(),
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
    const { selectedNetwork, selectedChannel } = get();
    if (selectedNetwork === null) return;

    try {
      if (selectedChannel === null || selectedChannel === 'status') {
        const msgs = await GetMessages(selectedNetwork, null, 100);
        set({ messages: sortByTimestamp(msgs || []) });
        return;
      }

      if (selectedChannel.startsWith('pm:')) {
        const user = selectedChannel.substring(3);
        const msgs = await GetPrivateMessages(selectedNetwork, user, 100);
        set({ messages: sortByTimestamp(msgs || []) });
        return;
      }

      const channelId = await GetChannelIDByName(selectedNetwork, selectedChannel);
      const msgs = await GetMessages(selectedNetwork, channelId as number, 100);
      set({ messages: sortByTimestamp(msgs || []) });
    } catch (error) {
      console.error('Failed to load messages:', error);
      set({ messages: [] });
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

  setSelectedNetwork: (id) => set({ selectedNetwork: id }),
  setSelectedChannel: (channel) => set({ selectedChannel: channel }),

  selectPane: async (networkId, channel) => {
    const prev = get();

    set({ selectedNetwork: networkId, selectedChannel: channel });

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

      // Handle /me command — prepend target
      if (trimmedMessage.toLowerCase().startsWith('/me ') && selectedChannel !== 'status') {
        const parts = trimmedMessage.substring(4).trim();
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
      return;
    }

    // Private messages
    if (selectedChannel.startsWith('pm:')) {
      const user = selectedChannel.substring(3);
      try {
        await SendMessage(selectedNetwork, user, message);
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
          message: message,
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
        await SendMessage(selectedNetwork, selectedChannel, message);
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

  setConnectionStatus: (networkId, connected) =>
    set((state) => ({
      connectionStatus: { ...state.connectionStatus, [networkId]: connected },
    })),

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
