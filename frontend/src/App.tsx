import { useEffect, useCallback, useRef, useState } from 'react';
import { SendCommand, OpenSettings, GetServers, ReorderNetworks } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { main } from '../wailsjs/go/models';
import { useNetworkStore } from './stores/network';
import { useUIStore } from './stores/ui';
import { eventMatchesPane } from './lib/pane-routing';
import { activityTargetForEvent } from './lib/activity';
import { isChannelName } from './lib/channel-name';
import { initCommands } from './stores/commands';
import { initDeepLinks } from './stores/deeplink';
import { useNotificationRouting } from './hooks/useNotificationRouting';
import { useTypingRouting } from './hooks/useTypingRouting';
import { NetworkRail } from './components/network-rail';
import { ChannelPanel } from './components/channel-panel';
import { NetworkContextMenu } from './components/network-context-menu';
import { MessageView } from './components/message-view';
import { InputArea } from './components/input-area';
import { ChannelInfo } from './components/channel-info';
import { PinnedMessages } from './components/pinned-messages';
import { TopicEditModal } from './components/topic-edit-modal';
import { ChannelModeEditor } from './components/channel-mode-editor';
import { UserInfo } from './components/user-info';
import { SearchModal } from './components/search-modal';
import { ChannelListModal } from './components/channel-list-modal';
import { KeyboardShortcutsModal } from './components/keyboard-shortcuts-modal';
import { HelpDialog } from './components/help-dialog';
import { UpdateAvailableDialog } from './components/update-available-dialog';
import { AuthBanner } from './components/AuthBanner';
import { DeepLinkDisambiguation } from './components/deeplink-disambiguation';
import { InviteToChannelModal } from './components/invite-to-channel-modal';
import { ActivityInbox } from './components/activity-inbox';
import { unseenGroupCount } from './lib/activity-inbox';
import { List, Bell } from 'lucide-react';

function App() {
  // Network store
  const networks = useNetworkStore((s) => s.networks);
  const selectedNetwork = useNetworkStore((s) => s.selectedNetwork);
  const selectedChannel = useNetworkStore((s) => s.selectedChannel);
  const connectionStatus = useNetworkStore((s) => s.connectionStatus);
  const currentNick = useNetworkStore((s) => s.currentNick);
  const channelInfo = useNetworkStore((s) => s.channelInfo);
  const unreadCounts = useNetworkStore((s) => s.unreadCounts);
  const loadNetworks = useNetworkStore((s) => s.loadNetworks);
  const loadMessages = useNetworkStore((s) => s.loadMessages);
  const loadChannelInfo = useNetworkStore((s) => s.loadChannelInfo);
  const loadConnectionStatus = useNetworkStore((s) => s.loadConnectionStatus);
  const loadCurrentNick = useNetworkStore((s) => s.loadCurrentNick);
  const loadServerCapabilities = useNetworkStore((s) => s.loadServerCapabilities);
  const loadPinnedMessages = useNetworkStore((s) => s.loadPinnedMessages);
  const noteNewWhileAnchored = useNetworkStore((s) => s.noteNewWhileAnchored);
  const pinnedCount = useNetworkStore((s) => s.pinnedMessages.length);
  const selectPane = useNetworkStore((s) => s.selectPane);
  const connectNetwork = useNetworkStore((s) => s.connectNetwork);
  const disconnectNetwork = useNetworkStore((s) => s.disconnectNetwork);
  const deleteNetwork = useNetworkStore((s) => s.deleteNetwork);
  const sendMessage = useNetworkStore((s) => s.sendMessage);
  const setConnectionStatus = useNetworkStore((s) => s.setConnectionStatus);
  const setCurrentNick = useNetworkStore((s) => s.setCurrentNick);
  const loadNetworkBots = useNetworkStore((s) => s.loadNetworkBots);
  const addBot = useNetworkStore((s) => s.addBot);
  const setMonitorOnline = useNetworkStore((s) => s.setMonitorOnline);
  const setPresence = useNetworkStore((s) => s.setPresence);
  const loadNetworkUserMeta = useNetworkStore((s) => s.loadNetworkUserMeta);
  const setUserMeta = useNetworkStore((s) => s.setUserMeta);
  const markActivity = useNetworkStore((s) => s.markActivity);
  const activityItems = useNetworkStore((s) => s.activityItems);
  const unseenActivity = unseenGroupCount(activityItems);
  const connectingNetworks = useNetworkStore((s) => s.connectingNetworks);
  const setSelectedNetwork = useNetworkStore((s) => s.setSelectedNetwork);
  const selectActivityInbox = useNetworkStore((s) => s.selectActivityInbox);
  const restoreLastPane = useNetworkStore((s) => s.restoreLastPane);

  // Network-rail right-click menu: which tile, at what viewport coordinates.
  const [networkMenu, setNetworkMenu] = useState<{ x: number; y: number; networkId: number } | null>(null);
  const openNetworkContextMenu = useCallback((e: React.MouseEvent, id: number) => {
    e.preventDefault();
    setNetworkMenu({ x: e.clientX, y: e.clientY, networkId: id });
  }, []);

  useNotificationRouting();
  useTypingRouting();

  // UI store
  const showTopicModal = useUIStore((s) => s.showTopicModal);
  const setShowTopicModal = useUIStore((s) => s.setShowTopicModal);
  const showModeModal = useUIStore((s) => s.showModeModal);
  const setShowModeModal = useUIStore((s) => s.setShowModeModal);
  const showUserInfo = useUIStore((s) => s.showUserInfo);
  const setShowUserInfo = useUIStore((s) => s.setShowUserInfo);
  const inviteTo = useUIStore((s) => s.inviteTo);
  const setInviteTo = useUIStore((s) => s.setInviteTo);
  const showSearch = useUIStore((s) => s.showSearch);
  const openSearch = useUIStore((s) => s.openSearch);
  const closeSearch = useUIStore((s) => s.closeSearch);
  const showChannelList = useUIStore((s) => s.showChannelList);
  const closeChannelList = useUIStore((s) => s.closeChannelList);
  const showKeyboardShortcuts = useUIStore((s) => s.showKeyboardShortcuts);
  const toggleKeyboardShortcuts = useUIStore((s) => s.toggleKeyboardShortcuts);
  const closeKeyboardShortcuts = useUIStore((s) => s.closeKeyboardShortcuts);
  const leftSidebarWidth = useUIStore((s) => s.leftSidebarWidth);
  const rightSidebarWidth = useUIStore((s) => s.rightSidebarWidth);
  const setLeftSidebarWidth = useUIStore((s) => s.setLeftSidebarWidth);
  const setRightSidebarWidth = useUIStore((s) => s.setRightSidebarWidth);
  const leftSidebarCollapsed = useUIStore((s) => s.leftSidebarCollapsed);
  const rightSidebarCollapsed = useUIStore((s) => s.rightSidebarCollapsed);
  const toggleLeftSidebar = useUIStore((s) => s.toggleLeftSidebar);
  const toggleRightSidebar = useUIStore((s) => s.toggleRightSidebar);
  const setLeftSidebarCollapsed = useUIStore((s) => s.setLeftSidebarCollapsed);
  const setRightSidebarCollapsed = useUIStore((s) => s.setRightSidebarCollapsed);
  const rightSidebarTab = useUIStore((s) => s.rightSidebarTab);
  const setRightSidebarTab = useUIStore((s) => s.setRightSidebarTab);

  // Refs
  const hasRestoredPaneRef = useRef(false);
  // `fromChannel` records the pane the user was on when they issued /join, so the
  // deferred auto-focus can yield if they navigate elsewhere before it fires.
  const pendingJoinChannelRef = useRef<{
    networkId: number;
    channel: string;
    fromChannel: string | null;
  } | null>(null);
  const resizeStartX = useRef(0);
  const resizeStartWidth = useRef(0);
  const isResizingLeftRef = useRef(false);
  const isResizingRightRef = useRef(false);

  // Ref for the server tree sidebar (for keyboard focus)
  const serverTreeRef = useRef<HTMLDivElement>(null);

  // --- Effects ---

  // Global keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;

      // Cmd/Ctrl+K — Search
      if (mod && e.key === 'k') {
        e.preventDefault();
        openSearch();
        return;
      }

      // Cmd/Ctrl+, — Open the standalone Settings window
      if (mod && e.key === ',') {
        e.preventDefault();
        void OpenSettings();
        return;
      }

      // Cmd/Ctrl+/ — Toggle keyboard shortcuts
      if (mod && e.key === '/') {
        e.preventDefault();
        toggleKeyboardShortcuts();
        return;
      }

      // Cmd/Ctrl+B — Toggle left sidebar
      if (mod && !e.shiftKey && (e.key === 'b' || e.key === 'B')) {
        e.preventDefault();
        toggleLeftSidebar();
        return;
      }

      // Cmd/Ctrl+Shift+B — Toggle right sidebar
      if (mod && e.shiftKey && (e.key === 'b' || e.key === 'B')) {
        e.preventDefault();
        toggleRightSidebar();
        return;
      }

      // Cmd/Ctrl+Shift+N — Focus network/channel tree
      if (mod && e.shiftKey && (e.key === 'N' || e.key === 'n')) {
        e.preventDefault();
        // Focus the first focusable element in the server tree sidebar
        const sidebar = serverTreeRef.current;
        if (sidebar) {
          const focusable = sidebar.querySelector<HTMLElement>(
            'button, [tabindex]:not([tabindex="-1"]), a'
          );
          if (focusable) {
            focusable.focus();
          } else {
            sidebar.focus();
          }
        }
        return;
      }

      // Escape — Close any open modal
      if (e.key === 'Escape') {
        if (showKeyboardShortcuts) {
          closeKeyboardShortcuts();
          return;
        }
        if (showSearch) {
          closeSearch();
          return;
        }
        if (showTopicModal) {
          setShowTopicModal(false);
          return;
        }
        if (showModeModal) {
          setShowModeModal(false);
          return;
        }
        if (showUserInfo) {
          setShowUserInfo(null);
          return;
        }
        if (showChannelList) {
          closeChannelList();
          return;
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [openSearch, toggleKeyboardShortcuts, closeKeyboardShortcuts, showKeyboardShortcuts, showSearch, closeSearch, showTopicModal, setShowTopicModal, showModeModal, setShowModeModal, showUserInfo, setShowUserInfo, showChannelList, closeChannelList, toggleLeftSidebar, toggleRightSidebar]);

  // Responsive sidebar collapse on small windows.
  // Only react when the window actually crosses the breakpoint — otherwise a
  // resize within the same band would overwrite a manual sidebar toggle on every
  // resize tick, making the sidebar fight the user and the layout look "messed up".
  useEffect(() => {
    const BREAKPOINT = 768;
    let wasNarrow: boolean | null = null;
    const handleResize = () => {
      const narrow = window.innerWidth < BREAKPOINT;
      if (narrow === wasNarrow) return;
      wasNarrow = narrow;
      setLeftSidebarCollapsed(narrow);
      setRightSidebarCollapsed(narrow);
    };
    // Check on mount (wasNarrow starts null, so this always sets initial state)
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Initial load + periodic refresh
  useEffect(() => {
    loadNetworks();
    void initCommands();
    const interval = setInterval(loadNetworks, 5000);

    // The backend broadcasts networks:changed after a save/delete/auto-connect
    // toggle. When the change was made in the standalone Settings window, this is
    // how the main window's list refreshes immediately rather than waiting on the
    // poll above.
    const unsubscribe = EventsOn('networks:changed', () => {
      loadNetworks();
    });

    return () => {
      clearInterval(interval);
      unsubscribe();
    };
  }, []);

  // Deep-link event router (main window only — App.tsx is never rendered in the
  // settings window; main.tsx routes ?view=settings directly to SettingsPanel).
  useEffect(() => {
    const off = initDeepLinks();
    return off;
  }, []);

  // Restore last pane on initial load
  useEffect(() => {
    if (hasRestoredPaneRef.current || networks.length === 0) return;
    hasRestoredPaneRef.current = true;
    const timeoutId = setTimeout(() => restoreLastPane(), 200);
    return () => clearTimeout(timeoutId);
  }, [networks]);

  // Load data when selection changes. Refreshes are fully event-driven
  // thereafter (message-event, connection-status, current-nick, channel.topic/
  // mode) — there is deliberately no periodic poll here.
  useEffect(() => {
    if (selectedNetwork !== null) {
      loadMessages();
      loadConnectionStatus();
      loadCurrentNick();
      loadServerCapabilities();
      loadChannelInfo();
      loadPinnedMessages();
      loadNetworkBots();
      loadNetworkUserMeta();
    }
  }, [selectedNetwork, selectedChannel]);

  // Connection status events
  useEffect(() => {
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      const networkId = data?.networkId;
      const connected = data?.connected;
      const at = data?.timestamp ? Date.parse(data.timestamp) : undefined;
      if (networkId !== undefined && typeof connected === 'boolean') {
        setConnectionStatus(networkId, connected, Number.isNaN(at) ? undefined : at);
        // Dismiss any auth-failure banner once the network comes back up.
        if (connected === true) {
          useNetworkStore.getState().clearAuthFailed(Number(networkId));
          // ISUPPORT (CHANTYPES/CASEMAPPING) arrives right after connect; refresh
          // the cached capabilities so channel detection uses the real set.
          void useNetworkStore.getState().loadServerCapabilities(Number(networkId));
        }
      }
    });
    // A connect attempt is in flight (dial + CAP/SASL handshake) or has settled.
    // Drives the "Connecting…" UI so the network can't be told to connect again
    // mid-handshake (a second attempt races the first and causes connect-then-drop
    // churn). Fires for every connect source, not just the context menu.
    const unsubscribeConnecting = EventsOn('connection-connecting', (data: any) => {
      const networkId = Number(data?.networkId);
      const connecting = data?.connecting;
      if (!Number.isNaN(networkId) && typeof connecting === 'boolean') {
        useNetworkStore.getState().setConnecting(networkId, connecting);
      }
    });
    const unsubscribeAuth = EventsOn('auth-failed', (data: any) => {
      const networkId = Number(data?.networkId);
      if (!Number.isNaN(networkId)) {
        useNetworkStore.getState().setAuthFailed(networkId, String(data?.reason ?? ''));
      }
    });
    return () => {
      unsubscribe();
      unsubscribeConnecting();
      unsubscribeAuth();
    };
  }, []);

  // Current-nick events: track the server-assigned nick so the header reflects
  // who we actually are, including after an automatic reclaim of our nick.
  useEffect(() => {
    const unsubscribe = EventsOn('current-nick', (data: any) => {
      const networkId = data?.networkId;
      const nick = data?.nick;
      if (networkId !== undefined && typeof nick === 'string' && nick) {
        setCurrentNick(networkId, nick);
      }
    });
    return () => unsubscribe();
  }, []);

  // Bot-detected events (IRCv3 bot mode): a nick was recognized as a bot via the
  // `bot` message tag or RPL_WHOISBOT. Add it to the per-network bot set so the
  // chat rows, nick list, and WHOIS panel can badge it.
  useEffect(() => {
    const unsubscribe = EventsOn('bot-event', (data: any) => {
      const networkId = data?.data?.networkId;
      const nick = data?.data?.nickname;
      if (typeof networkId === 'number' && typeof nick === 'string' && nick) {
        addBot(networkId, nick);
      }
    });
    return () => unsubscribe();
  }, []);

  // MONITOR presence events (IRCv3): a monitored nick came online/offline. Feed
  // both the Buddies pane (buddy list) and the general presence map that drives
  // the DM-list dots (which also covers auto-monitored PM correspondents).
  useEffect(() => {
    const unsubscribe = EventsOn('monitor-event', (data: any) => {
      const d = data?.data;
      const networkId = d?.networkId;
      const nick = d?.nickname;
      if (typeof networkId === 'number' && typeof nick === 'string' && nick) {
        setMonitorOnline(networkId, nick, !!d.online);
        setPresence(networkId, nick, !!d.online);
      }
    });
    return () => unsubscribe();
  }, []);

  // Live-roster events (IRCv3 away-notify / account-notify / extended-join /
  // chghost / account-tag): a user's away/account/host changed. Update the
  // per-network roster metadata so the nick list can dim away users and the
  // WHOIS panel can show live account/away.
  useEffect(() => {
    const unsubscribe = EventsOn('usermeta-event', (data: any) => {
      const d = data?.data;
      const networkId = d?.networkId;
      const nick = d?.nickname;
      if (typeof networkId === 'number' && typeof nick === 'string' && nick) {
        setUserMeta(networkId, nick, {
          away: !!d.away,
          away_message: typeof d.away_message === 'string' ? d.away_message : '',
          account: typeof d.account === 'string' ? d.account : '',
          host: typeof d.host === 'string' ? d.host : '',
          realname: typeof d.realname === 'string' ? d.realname : '',
        });
      }
    });
    return () => unsubscribe();
  }, []);

  // Activity inbox: load once on mount, then live-refresh whenever the
  // backend reports a change (new highlight/PM/invite, seen/dismiss, etc.).
  useEffect(() => {
    void useNetworkStore.getState().loadActivityItems();
    const off = EventsOn('activity-changed', () => {
      void useNetworkStore.getState().loadActivityItems();
    });
    return () => off();
  }, []);

  // Message events for real-time updates and activity tracking
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      const eventType = data?.type;
      const eventData = data?.data || {};
      // Resolve the source network by its unique id (the deprecated `network`
      // address is non-unique and collides across networks sharing an address).
      const networkId =
        eventData.networkId != null && eventData.networkId !== ''
          ? Number(eventData.networkId)
          : undefined;
      const target = eventData.target || eventData.channel;

      // Track activity for unfocused channels/PMs. The activity target is resolved
      // by the event's unique networkId (see lib/activity.ts) — never by the
      // deprecated, non-unique `network` address, which collided across networks
      // sharing an address (e.g. two Ergo servers' #programming badges merging).
      if (eventType === 'message.received' || eventType === 'message.sent') {
        const activity = activityTargetForEvent(eventType, eventData, networks);
        if (activity) {
          const isFocused =
            selectedNetwork === activity.networkId && selectedChannel === activity.paneKey;
          if (!isFocused) {
            markActivity(activity.activityKey);
          }
        }
      }

      // Handle pending join channel switching
      if (eventType === 'user.joined') {
        const networkObj = networks.find((n) => n.id === networkId);
        if (networkObj) {
          const user = eventData.user;
          const channel = eventData.channel || target;
          const pending = pendingJoinChannelRef.current;

          if (
            user &&
            networkObj.nickname &&
            user.toLowerCase() === networkObj.nickname.toLowerCase() &&
            pending &&
            pending.networkId === networkObj.id
          ) {
            const chanTypes = useNetworkStore.getState().chanTypes[networkObj.id];
            const norm = (ch: string) =>
              isChannelName(ch, chanTypes) ? ch.toLowerCase() : '#' + ch.toLowerCase();
            if (norm(pending.channel) === norm(channel)) {
              const nId = networkObj.id;
              const chName = channel;
              const fromNetwork = pending.networkId;
              const fromChannel = pending.fromChannel;
              setTimeout(() => {
                // Auto-focus the just-joined channel only if the user hasn't
                // navigated away since issuing /join — a manual switch wins.
                const { selectedNetwork: curNet, selectedChannel: curChan } =
                  useNetworkStore.getState();
                if (curNet === fromNetwork && curChan === fromChannel) {
                  selectPane(nId, chName);
                }
                pendingJoinChannelRef.current = null;
              }, 300);
            }
          }
        }
      }

      // Refresh messages if event matches current view
      if (selectedNetwork === null) return;
      const currentNetwork = networks.find((n) => n.id === selectedNetwork);
      if (currentNetwork && networkId === currentNetwork.id) {
        // Route the event to its buffer key (pm:<peer> for DMs via the backend's
        // pmTarget, #chan for channels, status otherwise) and compare to the open
        // pane. Matching DMs on the raw target never lined up with the "pm:<peer>"
        // pane, so DM panes only refreshed on the 2s poll.
        const matchesView = eventMatchesPane(eventData, selectedChannel);
        if (matchesView) {
          // While anchored to a pinned/old message, don't reload (which would snap
          // back to live). Instead count new messages so the badge can show them.
          if (useNetworkStore.getState().viewMode === 'anchored') {
            if (eventType === 'message.received' || eventType === 'message.sent') {
              noteNewWhileAnchored();
            }
          } else {
            loadMessages();
          }
        }
      }
    });
    return () => unsubscribe();
  }, [selectedNetwork, selectedChannel, networks]);

  // CHATHISTORY completion events. Resolves a parked scrollback request (prepending
  // older rows while preserving the viewport); for on-join/on-open catch-up there's
  // no parked request, so refresh the live view to surface the backfilled messages.
  useEffect(() => {
    const unsubscribe = EventsOn('history-event', (data: any) => {
      const eventData = data?.data || {};
      const target = (eventData.target as string) || '';
      const inserted = (eventData.inserted as number) || 0;
      const store = useNetworkStore.getState();

      const handledScrollback = store.onHistoryReceived(target, inserted);
      if (handledScrollback || inserted === 0) return;

      // Live catch-up: if the backfilled target is the active buffer and we're not
      // anchored, reload so the new history appears now rather than on the next poll.
      if (store.viewMode !== 'live' || selectedNetwork === null) return;
      const currentNetwork = networks.find((n) => n.id === selectedNetwork);
      const eventNetworkId =
        eventData.networkId != null && eventData.networkId !== ''
          ? Number(eventData.networkId)
          : undefined;
      if (!currentNetwork || eventNetworkId !== currentNetwork.id) return;
      const sel = store.selectedChannel;
      const matches =
        sel === target ||
        (!!sel && sel.startsWith('pm:') && sel.substring(3).toLowerCase() === target.toLowerCase());
      if (matches) store.loadMessages();
    });
    return () => unsubscribe();
  }, [selectedNetwork, networks]);

  // Topic/mode change events
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      if (selectedNetwork === null || selectedChannel === null || selectedChannel === 'status') return;
      const eventType = data?.type;
      const eventData = data?.data || {};
      const eventNetworkId =
        eventData.networkId != null && eventData.networkId !== ''
          ? Number(eventData.networkId)
          : undefined;
      const channel = eventData.channel;
      const currentNetwork = networks.find((n) => n.id === selectedNetwork);
      if (currentNetwork && eventNetworkId === currentNetwork.id && channel === selectedChannel) {
        if (eventType === 'channel.topic' || eventType === 'channel.mode') {
          loadChannelInfo();
        }
      }
    });
    return () => unsubscribe();
  }, [selectedNetwork, selectedChannel, networks]);

  // --- Event handlers ---

  const handleConnect = async (config: any) => {
    try {
      await connectNetwork(config);
    } catch (error) {
      alert(`Failed to connect: ${error}`);
    }
  };

  const handleDisconnect = async (networkId: number) => {
    try {
      await disconnectNetwork(networkId);
    } catch (error) {
      alert(`Failed to disconnect: ${error}`);
    }
  };

  // Reconnect from the auth-failure banner: rebuild a NetworkConfig from the
  // stored network record (same approach as the network context menu) and
  // delegate to the shared connectNetwork action.
  const handleAuthBannerReconnect = async (networkId: number) => {
    const network = useNetworkStore.getState().networks.find((n) => n.id === networkId);
    if (!network) return;
    try {
      const dbServers = await GetServers(networkId);
      const configData: any = {
        name: network.name,
        nickname: network.nickname,
        username: network.username,
        realname: network.realname,
        // Secrets are keychain-backed and not exposed to the UI; the backend
        // resolves them from the keychain on connect.
        password: '',
        sasl_enabled: network.sasl_enabled || false,
        sasl_mechanism: network.sasl_mechanism || '',
        sasl_username: network.sasl_username || '',
        sasl_password: '',
        sasl_external_cert: network.sasl_external_cert || '',
      };
      if (dbServers && dbServers.length > 0) {
        configData.servers = dbServers.map((srv: any) => ({
          address: srv.address,
          port: srv.port,
          tls: srv.tls,
          order: srv.order,
        }));
      } else {
        configData.address = network.address;
        configData.port = network.port;
        configData.tls = network.tls;
      }
      await connectNetwork(main.NetworkConfig.createFrom(configData));
    } catch (error) {
      alert(`Failed to reconnect: ${error}`);
    }
  };

  // Open the standalone Settings window so the user can edit the network's
  // credentials. The Settings window is the single source of truth for network
  // config; we open it the same way the toolbar Settings button does.
  const handleAuthBannerEditCredentials = (_networkId: number) => {
    void OpenSettings();
  };

  const handleDelete = async (networkId: number) => {
    try {
      await deleteNetwork(networkId);
    } catch (error) {
      alert(`Failed to delete: ${error}`);
      throw error;
    }
  };

  const handleSendMessage = async (message: string) => {
    if (selectedNetwork === null || selectedChannel === null) return;

    const trimmed = message.trim();

    // Track /join for channel switching
    if (trimmed.toLowerCase().startsWith('/join ')) {
      const rest = trimmed.substring(5).trim();
      const parts = rest ? rest.split(/\s+/) : [];
      const joinChanTypes = useNetworkStore.getState().chanTypes[selectedNetwork];
      if (parts.length > 0 && isChannelName(parts[0], joinChanTypes)) {
        pendingJoinChannelRef.current = {
          networkId: selectedNetwork,
          channel: parts[0],
          fromChannel: selectedChannel,
        };
        setTimeout(() => {
          if (
            pendingJoinChannelRef.current &&
            pendingJoinChannelRef.current.channel === parts[0]
          ) {
            pendingJoinChannelRef.current = null;
          }
        }, 5000);
      }
    }

    await sendMessage(message);
  };

  // --- Resize handlers ---

  const handleLeftResizeMove = useCallback((e: MouseEvent) => {
    if (!isResizingLeftRef.current) return;
    const diff = e.clientX - resizeStartX.current;
    setLeftSidebarWidth(Math.max(200, Math.min(400, resizeStartWidth.current + diff)));
  }, []);

  const handleLeftResizeEnd = useCallback(() => {
    isResizingLeftRef.current = false;
    document.removeEventListener('mousemove', handleLeftResizeMove);
    document.removeEventListener('mouseup', handleLeftResizeEnd);
  }, [handleLeftResizeMove]);

  const handleLeftResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      isResizingLeftRef.current = true;
      resizeStartX.current = e.clientX;
      resizeStartWidth.current = leftSidebarWidth;
      document.addEventListener('mousemove', handleLeftResizeMove);
      document.addEventListener('mouseup', handleLeftResizeEnd);
    },
    [leftSidebarWidth, handleLeftResizeMove, handleLeftResizeEnd]
  );

  const handleRightResizeMove = useCallback((e: MouseEvent) => {
    if (!isResizingRightRef.current) return;
    const diff = resizeStartX.current - e.clientX;
    setRightSidebarWidth(Math.max(150, Math.min(400, resizeStartWidth.current + diff)));
  }, []);

  const handleRightResizeEnd = useCallback(() => {
    isResizingRightRef.current = false;
    document.removeEventListener('mousemove', handleRightResizeMove);
    document.removeEventListener('mouseup', handleRightResizeEnd);
  }, [handleRightResizeMove]);

  const handleRightResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      isResizingRightRef.current = true;
      resizeStartX.current = e.clientX;
      resizeStartWidth.current = rightSidebarWidth;
      document.addEventListener('mousemove', handleRightResizeMove);
      document.addEventListener('mouseup', handleRightResizeEnd);
    },
    [rightSidebarWidth, handleRightResizeMove, handleRightResizeEnd]
  );

  // --- Render ---

  // Header nick chip: the nick the server currently knows us by, plus whether it
  // differs from our configured nick (meaning a reclaim of the preferred nick is
  // pending — the client retries automatically on each keepalive).
  const selectedNick = selectedNetwork !== null ? currentNick[selectedNetwork] : undefined;
  const preferredNick =
    selectedNetwork !== null ? networks.find((n) => n.id === selectedNetwork)?.nickname : undefined;
  const nickReclaimPending = !!selectedNick && !!preferredNick && selectedNick !== preferredNick;

  return (
    <div className="flex h-screen bg-background overflow-hidden">
      {/* Left region — always-visible network rail + collapsible channel panel */}
      <div
        ref={serverTreeRef}
        data-testid="left-sidebar"
        data-collapsed={String(leftSidebarCollapsed)}
        className="flex flex-shrink-0"
        tabIndex={-1}
      >
        {/* Network rail — always visible, fixed width */}
        <NetworkRail
          networks={networks}
          selectedNetwork={selectedNetwork}
          activityActive={selectedChannel === 'activity'}
          connectionStatus={connectionStatus}
          connectingNetworks={connectingNetworks}
          unreadCounts={unreadCounts}
          activityItems={activityItems}
          onSelectNetwork={(id) => {
            setSelectedNetwork(id);
            selectPane(id, 'status');
          }}
          onSelectActivity={() => void selectActivityInbox()}
          onAddNetwork={() => void OpenSettings()}
          onNetworkContextMenu={openNetworkContextMenu}
          onReordered={async (ids) => {
            await ReorderNetworks(ids);
            await loadNetworks();
          }}
        />

        {/* Channel panel — the selected network, hidden during the Activity
            takeover, when no network is selected, or when collapsed */}
        {selectedChannel !== 'activity' && selectedNetwork !== null && !leftSidebarCollapsed && (
          <div
            className="border-r border-border flex-shrink-0 relative bg-card/30"
            style={{ width: `${leftSidebarWidth}px`, transition: 'width 0.2s ease' }}
          >
            <ChannelPanel
              network={networks.find((n) => n.id === selectedNetwork)!}
              selectedChannel={selectedChannel}
              connected={connectionStatus[selectedNetwork] || false}
              currentNick={currentNick[selectedNetwork]}
              unreadCounts={unreadCounts}
              onSelectChannel={(networkId, channel) => selectPane(networkId, channel)}
              onShowUserInfo={(networkId, nickname) => setShowUserInfo({ networkId, nickname })}
            />
            <div
              data-testid="left-resize-handle"
              className="absolute top-0 right-0 w-1 h-full cursor-col-resize hover:w-2 hover:bg-primary/40 bg-transparent z-10"
              style={{ transition: 'var(--transition-base)' }}
              onMouseDown={handleLeftResizeStart}
              title="Drag to resize"
            />
          </div>
        )}
      </div>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <div
          className="border-b border-border"
          style={{ background: 'var(--glass-bg)', backdropFilter: 'blur(var(--backdrop-blur))', WebkitBackdropFilter: 'blur(var(--backdrop-blur))' }}
        >
          <div className="h-14 flex items-center justify-between px-3 sm:px-5">
            <div className="flex items-center gap-2 min-w-0">
              {/* Hamburger toggle for left sidebar */}
              {leftSidebarCollapsed && (
                <button
                  onClick={toggleLeftSidebar}
                  data-testid="toggle-left-sidebar"
                  className="flex-shrink-0 p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title="Show sidebar"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <line x1="3" y1="6" x2="21" y2="6" />
                    <line x1="3" y1="12" x2="21" y2="12" />
                    <line x1="3" y1="18" x2="21" y2="18" />
                  </svg>
                </button>
              )}
              {!leftSidebarCollapsed && (
                <button
                  onClick={toggleLeftSidebar}
                  data-testid="toggle-left-sidebar"
                  className="flex-shrink-0 p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title="Hide sidebar"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <rect x="3" y="3" width="18" height="18" rx="2" />
                    <line x1="9" y1="3" x2="9" y2="21" />
                  </svg>
                </button>
              )}
              {selectedNetwork !== null && (
                <>
                  <span className="font-semibold text-lg truncate">
                    {networks.find((n) => n.id === selectedNetwork)?.name || 'Unknown'}
                  </span>
                  <span
                    className={`inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full ${
                      connectionStatus[selectedNetwork]
                        ? 'bg-green-500/15 text-green-700 dark:text-green-400'
                        : 'bg-muted text-muted-foreground'
                    }`}
                    title={connectionStatus[selectedNetwork] ? 'Connected' : 'Disconnected'}
                  >
                    <span
                      className="w-1.5 h-1.5 rounded-full"
                      style={{ background: connectionStatus[selectedNetwork] ? 'var(--presence-online)' : 'var(--presence-offline)' }}
                    />
                    {connectionStatus[selectedNetwork] ? 'Connected' : 'Disconnected'}
                  </span>
                  {connectionStatus[selectedNetwork] && selectedNick && (
                    <span
                      data-testid="current-nick"
                      className={`text-xs font-medium ${
                        nickReclaimPending ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground'
                      }`}
                      title={
                        nickReclaimPending ? `Trying to reclaim ${preferredNick}` : `Your nick: ${selectedNick}`
                      }
                    >
                      · {selectedNick}
                    </span>
                  )}
                  {selectedChannel &&
                    selectedChannel !== 'status' &&
                    selectedChannel !== 'activity' &&
                    !selectedChannel.startsWith('pm:') && (
                      <>
                        <span className="text-muted-foreground/50">/</span>
                        <span
                          data-testid="active-channel-name"
                          className="text-muted-foreground font-medium"
                        >
                          {isChannelName(
                            selectedChannel,
                            selectedNetwork !== null
                              ? useNetworkStore.getState().chanTypes[selectedNetwork]
                              : undefined
                          )
                            ? selectedChannel
                            : `#${selectedChannel}`}
                        </span>
                      </>
                    )}
                  {selectedChannel && selectedChannel.startsWith('pm:') && (
                    <>
                      <span className="text-muted-foreground/50">/</span>
                      <span className="text-muted-foreground font-medium">
                        PM: {selectedChannel.substring(3)}
                      </span>
                    </>
                  )}
                  {selectedChannel === 'status' && (
                    <>
                      <span className="text-muted-foreground/50">/</span>
                      <span className="text-muted-foreground font-medium">Status</span>
                    </>
                  )}
                </>
              )}
            </div>
            <div className="flex items-center gap-1 flex-shrink-0">
              <button
                onClick={openSearch}
                className="flex items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground hover:text-foreground rounded-md hover:bg-accent/50 transition-colors cursor-pointer"
                title="Search messages (Ctrl+K)"
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <circle cx="11" cy="11" r="8" />
                  <path d="m21 21-4.3-4.3" />
                </svg>
                <span className="hidden sm:inline">Search</span>
                <kbd className="hidden sm:inline-flex h-5 select-none items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground opacity-60">
                  {navigator.platform?.includes('Mac') ? '\u2318' : 'Ctrl+'}K
                </kbd>
              </button>
              {/* Browse channels — for the selected network */}
              {selectedNetwork !== null && (
                <button
                  onClick={() => {
                    if (selectedNetwork !== null) {
                      useUIStore.getState().openChannelList(selectedNetwork);
                    }
                  }}
                  className="p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title="Browse channels"
                  aria-label="Browse channels"
                >
                  <List size={18} />
                </button>
              )}
              {/* Activity inbox */}
              <div className="relative">
                <button
                  onClick={() => void useNetworkStore.getState().selectActivityInbox()}
                  className="p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  aria-label="Activity"
                >
                  <Bell size={18} />
                </button>
                {unseenActivity > 0 && (
                  <span className="absolute -top-0.5 -right-0.5 bg-primary text-primary-foreground text-[10px] leading-none px-1 py-0.5 rounded-full">
                    {unseenActivity > 99 ? '99+' : unseenActivity}
                  </span>
                )}
              </div>
              {/* Right sidebar toggle — show for channels and PMs */}
              {selectedChannel && selectedChannel !== 'status' && selectedChannel !== 'activity' && (
                <button
                  onClick={toggleRightSidebar}
                  data-testid="toggle-right-sidebar"
                  className="p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title={rightSidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'}
                >
                  <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <rect x="3" y="3" width="18" height="18" rx="2" />
                    <line x1="15" y1="3" x2="15" y2="21" />
                  </svg>
                </button>
              )}
            </div>
          </div>
          {selectedChannel &&
            selectedChannel !== 'status' &&
            selectedChannel !== 'activity' &&
            !selectedChannel.startsWith('pm:') &&
            channelInfo?.channel && (
              <div className="px-5 pb-3 flex items-center gap-4 text-sm border-t border-border/50 pt-2">
                <button
                  data-testid="channel-modes-button"
                  onClick={() => setShowModeModal(true)}
                  className="text-muted-foreground hover:text-foreground cursor-pointer transition-all px-2 py-1 rounded-md hover:bg-accent/50 shrink-0"
                  style={{ transition: 'var(--transition-base)' }}
                  title="Click to edit channel modes"
                >
                  Modes: {channelInfo.channel.modes || '(none)'}
                </button>
                <button
                  onClick={() => setShowTopicModal(true)}
                  className="text-muted-foreground hover:text-foreground cursor-pointer italic flex-1 text-left truncate px-2 py-1 rounded-md hover:bg-accent/50 transition-all"
                  style={{ transition: 'var(--transition-base)' }}
                  title="Click to edit topic"
                >
                  {channelInfo.channel.topic || 'No topic set'}
                </button>
              </div>
            )}
          {selectedNetwork !== null && (
            <AuthBanner
              networkId={selectedNetwork}
              onReconnect={handleAuthBannerReconnect}
              onEditCredentials={handleAuthBannerEditCredentials}
            />
          )}
        </div>

        {/* Content Area */}
        <div className="flex-1 flex overflow-hidden">
          <div className="flex-1 overflow-y-auto">
            {selectedChannel === 'activity' ? (
              <ActivityInbox />
            ) : selectedNetwork !== null ? (
              <MessageView
                networkId={selectedNetwork}
                selectedChannel={selectedChannel}
              />
            ) : (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground px-4">
                <div className="text-5xl mb-4 opacity-40">💬</div>
                <div className="text-xl font-medium mb-2">Welcome to Cascade Chat</div>
                <div className="text-sm text-center max-w-md">
                  Select a network from the sidebar to start chatting, or add a new network in
                  Settings.
                </div>
              </div>
            )}
          </div>

          {/* Right Sidebar — Users (channels only) + Pinned messages */}
          {selectedChannel &&
            selectedChannel !== 'status' &&
            selectedChannel !== 'activity' &&
            (() => {
              const isPM = selectedChannel.startsWith('pm:');
              // PMs have no user list, so the pinned tab is the only option there.
              const effectiveTab = isPM ? 'pinned' : rightSidebarTab;
              return (
                <div
                  data-testid="right-sidebar"
                  data-collapsed={String(rightSidebarCollapsed)}
                  className="border-l border-border flex-shrink-0 relative"
                  style={{
                    width: rightSidebarCollapsed ? '0px' : `${rightSidebarWidth}px`,
                    minWidth: rightSidebarCollapsed ? '0px' : undefined,
                    overflow: 'hidden',
                    transition: 'width 0.2s ease',
                    borderLeftWidth: rightSidebarCollapsed ? '0px' : undefined,
                  }}
                >
                  {!rightSidebarCollapsed && (
                    <div
                      data-testid="right-resize-handle"
                      className="absolute top-0 left-0 w-1 h-full cursor-col-resize hover:w-2 hover:bg-primary/40 bg-transparent z-10"
                      style={{ transition: 'var(--transition-base)' }}
                      onMouseDown={handleRightResizeStart}
                      title="Drag to resize"
                    />
                  )}
                  {!rightSidebarCollapsed && (
                    <div className="flex flex-col h-full">
                      {/* Tab header */}
                      <div className="flex flex-shrink-0 border-b border-border text-sm">
                        {!isPM && (
                          <button
                            onClick={() => setRightSidebarTab('users')}
                            className={`flex-1 px-3 py-2 cursor-pointer transition-colors ${
                              effectiveTab === 'users'
                                ? 'text-foreground border-b-2 border-primary font-medium'
                                : 'text-muted-foreground hover:text-foreground'
                            }`}
                          >
                            Users
                          </button>
                        )}
                        <button
                          onClick={() => setRightSidebarTab('pinned')}
                          className={`flex-1 px-3 py-2 cursor-pointer transition-colors flex items-center justify-center gap-1.5 ${
                            effectiveTab === 'pinned'
                              ? 'text-foreground border-b-2 border-primary font-medium'
                              : 'text-muted-foreground hover:text-foreground'
                          }`}
                        >
                          Pinned
                          {pinnedCount > 0 && (
                            <span className="inline-flex items-center justify-center min-w-4 h-4 px-1 rounded-full bg-primary/20 text-primary text-[10px] font-medium">
                              {pinnedCount}
                            </span>
                          )}
                        </button>
                      </div>
                      {/* Body */}
                      <div className="flex-1 overflow-auto">
                        {effectiveTab === 'users' && !isPM ? (
                          <ChannelInfo
                            networkId={selectedNetwork}
                            channelName={selectedChannel}
                            currentNickname={selectedNick ?? preferredNick ?? null}
                            onSendCommand={async (command: string) => {
                              if (selectedNetwork !== null) {
                                await SendCommand(selectedNetwork, command);
                              }
                            }}
                            onOpenQuery={(nick) => {
                              if (selectedNetwork !== null) {
                                useNetworkStore.getState().openQuery(selectedNetwork, nick);
                              }
                            }}
                          />
                        ) : (
                          <PinnedMessages networkId={selectedNetwork} />
                        )}
                      </div>
                    </div>
                  )}
                </div>
              );
            })()}
        </div>

        {/* Input Area */}
        {selectedNetwork !== null && selectedChannel !== null && selectedChannel !== 'activity' && (
          <InputArea
            onSendMessage={handleSendMessage}
            placeholder={
              selectedChannel === 'status'
                ? 'Type a command (e.g., /join #channel, /msg user message) or raw IRC command...'
                : 'Type a message...'
            }
            networkId={selectedNetwork}
            channelName={selectedChannel}
          />
        )}
      </div>

      {/* Modals */}
      {showTopicModal &&
        selectedNetwork !== null &&
        selectedChannel !== null &&
        selectedChannel !== 'status' &&
        selectedChannel !== 'activity' &&
        channelInfo?.channel && (
          <TopicEditModal
            networkId={selectedNetwork}
            channelName={selectedChannel}
            currentTopic={channelInfo.channel.topic || ''}
            onClose={() => setShowTopicModal(false)}
            onUpdate={loadChannelInfo}
          />
        )}

      {showModeModal &&
        selectedNetwork !== null &&
        selectedChannel !== null &&
        selectedChannel !== 'status' &&
        selectedChannel !== 'activity' &&
        channelInfo?.channel && (
          <ChannelModeEditor
            networkId={selectedNetwork}
            channelName={selectedChannel}
            currentModes={channelInfo.channel.modes || ''}
            capabilities={channelInfo.capabilities ?? undefined}
            onClose={() => setShowModeModal(false)}
            onUpdate={loadChannelInfo}
          />
        )}

      {showSearch && <SearchModal onClose={closeSearch} />}

      {showChannelList && (
        <ChannelListModal
          networkId={showChannelList.networkId}
          initialFilter={showChannelList.filter}
          onClose={closeChannelList}
        />
      )}

      {showKeyboardShortcuts && (
        <KeyboardShortcutsModal onClose={closeKeyboardShortcuts} />
      )}

      {showUserInfo && (
        <UserInfo
          networkId={showUserInfo.networkId}
          nickname={showUserInfo.nickname}
          onClose={() => setShowUserInfo(null)}
        />
      )}

      {inviteTo && (
        <InviteToChannelModal
          networkId={inviteTo.networkId}
          nick={inviteTo.nick}
          currentChannel={inviteTo.channel}
          onClose={() => setInviteTo(null)}
        />
      )}

      {networkMenu &&
        (() => {
          const menuNetwork = networks.find((n) => n.id === networkMenu.networkId);
          if (!menuNetwork) return null;
          return (
            <NetworkContextMenu
              x={networkMenu.x}
              y={networkMenu.y}
              network={menuNetwork}
              connected={connectionStatus[menuNetwork.id] || false}
              connecting={connectingNetworks[menuNetwork.id] || false}
              onConnect={handleConnect}
              onDisconnect={handleDisconnect}
              onDelete={handleDelete}
              onReloadNetworks={loadNetworks}
              onClose={() => setNetworkMenu(null)}
            />
          );
        })()}

      <HelpDialog />
      <UpdateAvailableDialog />
      <DeepLinkDisambiguation />
    </div>
  );
}

export default App;
