import { useEffect, useCallback, useRef } from 'react';
import { SendCommand } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { useNetworkStore } from './stores/network';
import { useUIStore } from './stores/ui';
import { ServerTree } from './components/server-tree';
import { MessageView } from './components/message-view';
import { InputArea } from './components/input-area';
import { SettingsModal } from './components/settings-modal';
import { ChannelInfo } from './components/channel-info';
import { TopicEditModal } from './components/topic-edit-modal';
import { ModeEditModal } from './components/mode-edit-modal';
import { UserInfo } from './components/user-info';
import { SearchModal } from './components/search-modal';
import { ChannelListModal } from './components/channel-list-modal';
import { KeyboardShortcutsModal } from './components/keyboard-shortcuts-modal';

function App() {
  // Network store
  const networks = useNetworkStore((s) => s.networks);
  const selectedNetwork = useNetworkStore((s) => s.selectedNetwork);
  const selectedChannel = useNetworkStore((s) => s.selectedChannel);
  const messages = useNetworkStore((s) => s.messages);
  const connectionStatus = useNetworkStore((s) => s.connectionStatus);
  const channelInfo = useNetworkStore((s) => s.channelInfo);
  const unreadCounts = useNetworkStore((s) => s.unreadCounts);
  const loadNetworks = useNetworkStore((s) => s.loadNetworks);
  const loadMessages = useNetworkStore((s) => s.loadMessages);
  const loadChannelInfo = useNetworkStore((s) => s.loadChannelInfo);
  const loadConnectionStatus = useNetworkStore((s) => s.loadConnectionStatus);
  const selectPane = useNetworkStore((s) => s.selectPane);
  const connectNetwork = useNetworkStore((s) => s.connectNetwork);
  const disconnectNetwork = useNetworkStore((s) => s.disconnectNetwork);
  const deleteNetwork = useNetworkStore((s) => s.deleteNetwork);
  const sendMessage = useNetworkStore((s) => s.sendMessage);
  const setConnectionStatus = useNetworkStore((s) => s.setConnectionStatus);
  const markActivity = useNetworkStore((s) => s.markActivity);
  const restoreLastPane = useNetworkStore((s) => s.restoreLastPane);

  // UI store
  const showSettings = useUIStore((s) => s.showSettings);
  const settingsSection = useUIStore((s) => s.settingsSection);
  const openSettings = useUIStore((s) => s.openSettings);
  const closeSettings = useUIStore((s) => s.closeSettings);
  const showTopicModal = useUIStore((s) => s.showTopicModal);
  const setShowTopicModal = useUIStore((s) => s.setShowTopicModal);
  const showModeModal = useUIStore((s) => s.showModeModal);
  const setShowModeModal = useUIStore((s) => s.setShowModeModal);
  const showUserInfo = useUIStore((s) => s.showUserInfo);
  const setShowUserInfo = useUIStore((s) => s.setShowUserInfo);
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

  // Refs
  const hasRestoredPaneRef = useRef(false);
  const pendingJoinChannelRef = useRef<{ networkId: number; channel: string } | null>(null);
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

      // Cmd/Ctrl+, — Open settings
      if (mod && e.key === ',') {
        e.preventDefault();
        openSettings(undefined);
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
        if (showSettings) {
          closeSettings();
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
  }, [openSearch, openSettings, toggleKeyboardShortcuts, closeKeyboardShortcuts, showKeyboardShortcuts, showSearch, closeSearch, showSettings, closeSettings, showTopicModal, setShowTopicModal, showModeModal, setShowModeModal, showUserInfo, setShowUserInfo, showChannelList, closeChannelList, toggleLeftSidebar, toggleRightSidebar]);

  // Responsive sidebar collapse on small windows
  useEffect(() => {
    const BREAKPOINT = 768;
    const handleResize = () => {
      const narrow = window.innerWidth < BREAKPOINT;
      setLeftSidebarCollapsed(narrow);
      setRightSidebarCollapsed(narrow);
    };
    // Check on mount
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Initial load + periodic refresh
  useEffect(() => {
    loadNetworks();
    const interval = setInterval(loadNetworks, 5000);

    const unsubscribe = EventsOn('open-settings', (section?: string) => {
      if (section === 'networks' || section === 'plugins' || section === 'display') {
        openSettings(section);
      } else {
        openSettings(undefined);
      }
    });

    return () => {
      clearInterval(interval);
      unsubscribe();
    };
  }, []);

  // Restore last pane on initial load
  useEffect(() => {
    if (hasRestoredPaneRef.current || networks.length === 0) return;
    hasRestoredPaneRef.current = true;
    const timeoutId = setTimeout(() => restoreLastPane(), 200);
    return () => clearTimeout(timeoutId);
  }, [networks]);

  // Load data when selection changes
  useEffect(() => {
    if (selectedNetwork !== null) {
      loadMessages();
      loadConnectionStatus();
      loadChannelInfo();
      const interval = setInterval(() => {
        loadMessages();
        loadConnectionStatus();
        loadChannelInfo();
      }, 2000);
      return () => clearInterval(interval);
    }
  }, [selectedNetwork, selectedChannel]);

  // Connection status events
  useEffect(() => {
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      const networkId = data?.networkId;
      const connected = data?.connected;
      if (networkId !== undefined && typeof connected === 'boolean') {
        setConnectionStatus(networkId, connected);
      }
    });
    return () => unsubscribe();
  }, []);

  // Message events for real-time updates and activity tracking
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      const eventType = data?.type;
      const eventData = data?.data || {};
      const network = eventData.network;
      const target = eventData.target || eventData.channel;

      // Track activity for unfocused channels/PMs
      if (eventType === 'message.received' || eventType === 'message.sent') {
        const networkObj = networks.find((n) => n.address === network);
        if (networkObj && target && target !== 'status') {
          const isChannel = target.startsWith('#') || target.startsWith('&');
          let pmUser: string | null = null;
          if (!isChannel) {
            pmUser = eventType === 'message.received' ? eventData.user || null : target;
          }
          const pmKey = pmUser ? `pm:${pmUser}` : null;
          const activityKey = isChannel
            ? `${networkObj.id}:${target}`
            : pmKey
            ? `${networkObj.id}:${pmKey}`
            : null;

          if (activityKey) {
            const isFocused =
              selectedNetwork === networkObj.id &&
              (isChannel ? selectedChannel === target : selectedChannel === pmKey);
            if (!isFocused) {
              markActivity(activityKey);
            }
          }
        }
      }

      // Handle pending join channel switching
      if (eventType === 'user.joined') {
        const networkObj = networks.find((n) => n.address === network);
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
            const norm = (ch: string) =>
              ch.startsWith('#') || ch.startsWith('&') ? ch.toLowerCase() : '#' + ch.toLowerCase();
            if (norm(pending.channel) === norm(channel)) {
              const nId = networkObj.id;
              const chName = channel;
              setTimeout(() => {
                selectPane(nId, chName);
                pendingJoinChannelRef.current = null;
              }, 300);
            }
          }
        }
      }

      // Refresh messages if event matches current view
      if (selectedNetwork === null) return;
      const currentNetwork = networks.find((n) => n.id === selectedNetwork);
      if (currentNetwork && network === currentNetwork.address) {
        if (
          (eventType === 'message.received' || eventType === 'message.sent') &&
          target &&
          selectedChannel === target
        ) {
          loadMessages();
        } else if (target && selectedChannel === target) {
          loadMessages();
        } else if (eventData.channel === null && selectedChannel === 'status') {
          loadMessages();
        }
      }
    });
    return () => unsubscribe();
  }, [selectedNetwork, selectedChannel, networks]);

  // Topic/mode change events
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      if (selectedNetwork === null || selectedChannel === null || selectedChannel === 'status') return;
      const eventType = data?.type;
      const eventData = data?.data || {};
      const network = eventData.network;
      const channel = eventData.channel;
      const currentNetwork = networks.find((n) => n.id === selectedNetwork);
      if (currentNetwork && network === currentNetwork.address && channel === selectedChannel) {
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
      if (parts.length > 0 && (parts[0].startsWith('#') || parts[0].startsWith('&'))) {
        pendingJoinChannelRef.current = { networkId: selectedNetwork, channel: parts[0] };
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

  return (
    <div className="flex h-screen bg-background">
      {/* Network Tree Sidebar */}
      <div
        ref={serverTreeRef}
        className="border-r border-border overflow-auto flex-shrink-0 relative bg-card/30"
        style={{
          width: leftSidebarCollapsed ? '0px' : `${leftSidebarWidth}px`,
          minWidth: leftSidebarCollapsed ? '0px' : undefined,
          overflow: leftSidebarCollapsed ? 'hidden' : undefined,
          transition: 'width 0.2s ease',
        }}
        tabIndex={-1}
      >
        <ServerTree
          servers={networks}
          selectedServer={selectedNetwork}
          selectedChannel={selectedChannel}
          onSelectServer={useNetworkStore.getState().setSelectedNetwork}
          unreadCounts={unreadCounts}
          onShowUserInfo={(networkId, nickname) => setShowUserInfo({ networkId, nickname })}
          onNetworkUpdate={loadNetworks}
          onSelectChannel={(networkId, channel) => selectPane(networkId, channel)}
          onConnect={handleConnect}
          onDisconnect={handleDisconnect}
          onDelete={handleDelete}
          connectionStatus={connectionStatus}
        />
        {!leftSidebarCollapsed && (
          <div
            className="absolute top-0 right-0 w-1 h-full cursor-col-resize hover:w-2 hover:bg-primary/40 bg-border/50 z-10"
            style={{ transition: 'var(--transition-base)' }}
            onMouseDown={handleLeftResizeStart}
            title="Drag to resize"
          />
        )}
      </div>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <div className="border-b border-border bg-card/50 backdrop-blur-sm">
          <div className="h-14 flex items-center justify-between px-3 sm:px-5">
            <div className="flex items-center gap-2 min-w-0">
              {/* Hamburger toggle for left sidebar */}
              {leftSidebarCollapsed && (
                <button
                  onClick={toggleLeftSidebar}
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
                    className={`text-xs font-medium px-1.5 py-0.5 rounded ${
                      connectionStatus[selectedNetwork]
                        ? 'bg-green-500/20 text-green-700 dark:text-green-400'
                        : 'bg-gray-500/20 text-gray-700 dark:text-gray-400'
                    }`}
                    title={connectionStatus[selectedNetwork] ? 'Connected' : 'Disconnected'}
                  >
                    {connectionStatus[selectedNetwork] ? '●' : '○'}
                  </span>
                  {selectedChannel &&
                    selectedChannel !== 'status' &&
                    !selectedChannel.startsWith('pm:') && (
                      <>
                        <span className="text-muted-foreground/50">/</span>
                        <span className="text-muted-foreground font-medium">
                          {selectedChannel.startsWith('#') || selectedChannel.startsWith('&')
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
                <kbd className="hidden sm:inline-flex h-5 select-none items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground opacity-60">
                  {navigator.platform?.includes('Mac') ? '\u2318' : 'Ctrl+'}K
                </kbd>
              </button>
              {/* Right sidebar toggle — only show when a channel is selected */}
              {selectedChannel && selectedChannel !== 'status' && !selectedChannel.startsWith('pm:') && (
                <button
                  onClick={toggleRightSidebar}
                  className="p-1.5 rounded-md hover:bg-accent/50 text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                  title={rightSidebarCollapsed ? 'Show channel info' : 'Hide channel info'}
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
            !selectedChannel.startsWith('pm:') &&
            channelInfo?.channel && (
              <div className="px-5 pb-3 flex items-center gap-4 text-sm border-t border-border/50 pt-2">
                {channelInfo.channel.modes && (
                  <button
                    onClick={() => setShowModeModal(true)}
                    className="text-muted-foreground hover:text-foreground cursor-pointer transition-all px-2 py-1 rounded-md hover:bg-accent/50"
                    style={{ transition: 'var(--transition-base)' }}
                    title="Click to edit modes"
                  >
                    Modes: {channelInfo.channel.modes}
                  </button>
                )}
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
        </div>

        {/* Content Area */}
        <div className="flex-1 flex overflow-hidden">
          <div className="flex-1 overflow-y-auto">
            {selectedNetwork !== null ? (
              <MessageView
                messages={messages}
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

          {/* Channel Info Sidebar */}
          {selectedChannel &&
            selectedChannel !== 'status' &&
            !selectedChannel.startsWith('pm:') && (
              <div
                className="border-l border-border overflow-auto flex-shrink-0 relative"
                style={{
                  width: rightSidebarCollapsed ? '0px' : `${rightSidebarWidth}px`,
                  minWidth: rightSidebarCollapsed ? '0px' : undefined,
                  overflow: rightSidebarCollapsed ? 'hidden' : undefined,
                  transition: 'width 0.2s ease',
                  borderLeftWidth: rightSidebarCollapsed ? '0px' : undefined,
                }}
              >
                {!rightSidebarCollapsed && (
                  <div
                    className="absolute top-0 left-0 w-1 h-full cursor-col-resize hover:w-2 hover:bg-primary/40 bg-border/50 z-10"
                    style={{ transition: 'var(--transition-base)' }}
                    onMouseDown={handleRightResizeStart}
                    title="Drag to resize"
                  />
                )}
                <ChannelInfo
                  networkId={selectedNetwork}
                  channelName={selectedChannel}
                  currentNickname={
                    selectedNetwork !== null
                      ? networks.find((n) => n.id === selectedNetwork)?.nickname || null
                      : null
                  }
                  onSendCommand={async (command: string) => {
                    if (selectedNetwork !== null) {
                      await SendCommand(selectedNetwork, command);
                    }
                  }}
                />
              </div>
            )}

          {showUserInfo && (
            <UserInfo
              networkId={showUserInfo.networkId}
              nickname={showUserInfo.nickname}
              onClose={() => setShowUserInfo(null)}
            />
          )}
        </div>

        {/* Input Area */}
        {selectedNetwork !== null && selectedChannel !== null && (
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
      {showSettings && (
        <SettingsModal
          onClose={closeSettings}
          initialSection={settingsSection}
          onServerUpdate={() => {
            loadNetworks();
            loadConnectionStatus();
          }}
        />
      )}

      {showTopicModal &&
        selectedNetwork !== null &&
        selectedChannel !== null &&
        selectedChannel !== 'status' &&
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
        channelInfo?.channel && (
          <ModeEditModal
            networkId={selectedNetwork}
            channelName={selectedChannel}
            currentModes={channelInfo.channel.modes || ''}
            onClose={() => setShowModeModal(false)}
            onUpdate={loadChannelInfo}
          />
        )}

      {showSearch && <SearchModal onClose={closeSearch} />}

      {showChannelList && (
        <ChannelListModal
          networkId={showChannelList.networkId}
          onClose={closeChannelList}
        />
      )}

      {showKeyboardShortcuts && (
        <KeyboardShortcutsModal onClose={closeKeyboardShortcuts} />
      )}
    </div>
  );
}

export default App;
