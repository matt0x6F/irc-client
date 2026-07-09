import { useState, useEffect, useLayoutEffect, useRef } from 'react';
import { storage } from '../../wailsjs/go/models';
import { GetChannels, GetJoinedChannels, GetOpenChannels, LeaveChannel, CloseChannel, ToggleChannelAutoJoin, GetPrivateMessageConversations, SendCommand, SetPrivateMessageOpen, ClearPaneFocus } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useNetworkStore } from '../stores/network';
import { usePreferencesStore } from '../stores/preferences';
import { dmPresenceState } from '../lib/presence';
import { isChannelName } from '../lib/channel-name';
import { casefold } from '../lib/casefold';
import { Terminal } from 'lucide-react';

type Channel = storage.Channel;

interface ChannelPanelProps {
  network: storage.Network;
  selectedChannel: string | null;
  connected: boolean;
  currentNick?: string;
  unreadCounts: Map<string, number>;
  onSelectChannel: (networkId: number, channel: string | null) => void;
  onShowUserInfo: (networkId: number, nickname: string) => void;
}

interface ContextMenu {
  x: number;
  y: number;
  type: 'channel' | 'pm' | null;
  channel?: string;
  user?: string; // For PM context menu
}

export function ChannelPanel({
  network,
  selectedChannel,
  connected,
  currentNick,
  unreadCounts,
  onSelectChannel,
  onShowUserInfo,
}: ChannelPanelProps) {
  const networkId = network.id;
  const [channelList, setChannelList] = useState<string[]>([]);
  const [pmConversations, setPmConversations] = useState<string[]>([]);
  const [contextMenu, setContextMenu] = useState<ContextMenu>({ x: 0, y: 0, type: null });
  const [contextMenuChannelData, setContextMenuChannelData] = useState<Channel | null>(null);
  const [contextMenuIsJoined, setContextMenuIsJoined] = useState<boolean>(false);
  const [contextMenuJoinedChannels, setContextMenuJoinedChannels] = useState<Channel[]>([]);
  const contextMenuRef = useRef<HTMLDivElement>(null);

  // Live MONITOR presence for this network (lowercased nick -> online), driving
  // the DM-list dots. Seeded on mount, kept fresh by 'monitor-event' in App.tsx.
  const presence = useNetworkStore((s) => s.presence);
  // CASEMAPPING for this network, so DM-presence lookups fold nicks the same way
  // the store keys them (rfc1459 []\~ -> {}|^). Empty falls back to rfc1459.
  const caseMapping = useNetworkStore((s) => s.caseMapping);
  const loadPresence = useNetworkStore((s) => s.loadPresence);

  const refreshChannelList = async (id: number) => {
    const list = await GetOpenChannels(id);
    if (list && Array.isArray(list)) {
      setChannelList(list.map((c: Channel) => c.name));
    } else {
      setChannelList([]);
    }
  };

  const refreshPmConversations = async (id: number) => {
    try {
      const pmList = await GetPrivateMessageConversations(id, true);
      setPmConversations(pmList && Array.isArray(pmList) ? pmList : []);
    } catch (error) {
      console.error('Failed to load PM conversations:', error);
      setPmConversations([]);
    }
  };

  // Load channels and PM conversations for this network, and seed presence.
  useEffect(() => {
    (async () => {
      try {
        await refreshChannelList(networkId);
      } catch (error) {
        console.error('Failed to load channels:', error);
        setChannelList([]);
      }
      await refreshPmConversations(networkId);
      // Seed MONITOR presence for the DM-list dots (auto-monitored PM
      // correspondents plus durable buddies). Live updates arrive via
      // 'monitor-event'.
      void loadPresence(networkId);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [networkId, loadPresence]);

  // Listen for channels changed events for immediate sidebar updates
  useEffect(() => {
    const unsubscribe = EventsOn('channels-changed', (data: any) => {
      const id = data?.networkId;
      if (id !== networkId) return;
      refreshChannelList(id).catch((err) => {
        console.error('[ChannelPanel] Failed to refresh channels after channels-changed:', err);
      });
    });
    return () => unsubscribe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [networkId]);

  // Refresh PM conversations when channels change event is received
  useEffect(() => {
    const unsubscribe = EventsOn('channels-changed', (data: any) => {
      const id = data?.networkId;
      if (id !== networkId) return;
      refreshPmConversations(id).catch((error) => {
        console.error('Failed to refresh PM conversations:', error);
      });
    });
    return () => unsubscribe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [networkId]);

  // Refresh PM conversations when UI pane events are received (PM opened/closed)
  useEffect(() => {
    const unsubscribe = EventsOn('ui-pane-event', (data: any) => {
      const id = data?.networkId;
      const paneType = data?.paneType;
      if (id !== networkId || paneType !== 'pm') return;
      refreshPmConversations(id).catch((error) => {
        console.error('Failed to refresh PM conversations after pane event:', error);
      });
    });
    return () => unsubscribe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [networkId]);

  // A newly-arrived (or sent) private message can create a PM conversation that
  // is NOT announced via channels-changed/ui-pane-event — notably a
  // +draft/channel-context PM, which never focuses a pane. The old server tree
  // masked this by re-loading PMs on the 5s networks poll; this panel is
  // event-driven, so refresh the PM list directly on any PM message-event for
  // this network. A PM is identified by `pmTarget` (the peer); for a PM the
  // `channel` field holds the recipient's own nick, so it can't be used to
  // distinguish PMs from channel messages.
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      const type = data?.type;
      if (type !== 'message.received' && type !== 'message.sent') return;
      const d = data?.data || {};
      const id = d.networkId != null && d.networkId !== '' ? Number(d.networkId) : undefined;
      if (id !== networkId || !d.pmTarget) return;
      refreshPmConversations(networkId).catch((error) => {
        console.error('Failed to refresh PM conversations after message-event:', error);
      });
    });
    return () => unsubscribe();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [networkId]);

  // Clamp the fixed-position context menu into the viewport. It renders at the
  // raw click coordinates, so a right-click near the window's bottom would push
  // its items off-screen where they cannot be clicked. Runs again when async
  // menu data (joined channels) changes the height.
  useLayoutEffect(() => {
    const el = contextMenuRef.current;
    if (!el || !contextMenu.type) return;
    const margin = 8;
    const rect = el.getBoundingClientRect();
    const left = Math.min(contextMenu.x, window.innerWidth - rect.width - margin);
    const top = Math.min(contextMenu.y, window.innerHeight - rect.height - margin);
    el.style.left = `${Math.max(margin, left)}px`;
    el.style.top = `${Math.max(margin, top)}px`;
  }, [contextMenu, contextMenuIsJoined, contextMenuJoinedChannels]);

  // Close context menu on outside click
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(event.target as Node)) {
        setContextMenu({ x: 0, y: 0, type: null });
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu({ x: 0, y: 0, type: null });
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, []);

  const handleContextMenu = async (e: React.MouseEvent, type: 'channel' | 'pm', channel?: string, user?: string) => {
    e.preventDefault();
    e.stopPropagation();
    // Prevent text selection
    if (window.getSelection) {
      window.getSelection()?.removeAllRanges();
    }

    // Reset the PM "Invite to" channel list so a prior fetch never flashes
    // before this menu's own fetch resolves (only the 'pm' branch repopulates it).
    setContextMenuJoinedChannels([]);

    // If it's a channel context menu, load the channel data and check if joined
    let chData: Channel | null = null;
    let isJoined = false;
    if (type === 'channel' && channel) {
      try {
        const list = await GetChannels(networkId);
        const foundChannel = list?.find((c: Channel) => c.name === channel);
        if (foundChannel) {
          chData = foundChannel;
        }
        // Check if channel is joined
        const joinedChannels = await GetJoinedChannels(networkId);
        isJoined = joinedChannels?.some((c: Channel) => c.name === channel) || false;
      } catch (error) {
        console.error('Failed to load channel data for context menu:', error);
      }
    }

    // If it's a PM context menu, load joined channels for the "Invite to" submenu
    if (type === 'pm') {
      try {
        const joinedChannels = await GetJoinedChannels(networkId);
        setContextMenuJoinedChannels(joinedChannels ?? []);
      } catch (error) {
        console.error('Failed to load joined channels for PM context menu:', error);
        setContextMenuJoinedChannels([]);
      }
    }

    setContextMenuChannelData(chData);
    setContextMenuIsJoined(isJoined);
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      type,
      channel,
      user,
    });
  };

  const handleChannelClick = (channel: string) => {
    onSelectChannel(networkId, channel);
  };

  // Close = hide the buffer pane, stay joined on the server.
  const handleCloseChannel = async (channelName: string) => {
    try {
      await CloseChannel(networkId, channelName);
      await refreshChannelList(networkId);
      if (selectedChannel === channelName) {
        onSelectChannel(networkId, 'status');
      }
    } catch (error) {
      console.error('Failed to close channel:', error);
      alert(`Failed to close channel: ${error}`);
    }
    setContextMenu({ x: 0, y: 0, type: null });
  };

  // Leave = send a real PART. If the preference is on, also close the buffer.
  const handleLeaveChannel = async (channelName: string) => {
    try {
      await LeaveChannel(networkId, channelName);
      if (usePreferencesStore.getState().closeBufferOnLeave) {
        await CloseChannel(networkId, channelName);
      }
      await refreshChannelList(networkId);
      if (selectedChannel === channelName) {
        onSelectChannel(networkId, 'status');
      }
    } catch (error) {
      console.error('Failed to leave channel:', error);
      alert(`Failed to leave channel: ${error}`);
    }
    setContextMenu({ x: 0, y: 0, type: null });
  };

  return (
    <div data-testid="channel-panel" className="h-full flex flex-col relative bg-card/30">
      <div className="h-13 px-3.5 border-b border-border flex flex-col justify-center" style={{ height: 52 }}>
        <div className="flex items-center gap-1.5">
          <span className="text-[15px] font-medium truncate">{network.name}</span>
          <span className="w-[7px] h-[7px] rounded-full" style={{ background: connected ? 'var(--presence-online)' : 'var(--presence-offline)' }} />
        </div>
        <span className="text-xs text-muted-foreground truncate">
          {connected ? 'connected' : 'disconnected'}{currentNick ? ` · ${currentNick}` : ''}
        </span>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="ml-[1.1rem] pl-1.5 border-l border-border/60 mt-2 mb-1">
          {/* Server log (status pane) */}
          <div
            className={`px-2 py-1.5 mr-1 rounded-md cursor-pointer select-none transition-all flex items-center gap-2 ${
              selectedChannel === 'status'
                ? 'cc-active-pane'
                : 'hover:bg-accent/70'
            }`}
            style={{ transition: 'var(--transition-base)' }}
            onClick={() => handleChannelClick('status')}
            onMouseDown={(e) => {
              if (e.button === 2) {
                e.preventDefault();
              }
            }}
          >
            <Terminal className="w-3.5 h-3.5 flex-shrink-0 opacity-85" />
            <span className="text-sm">Server log</span>
          </div>

          {channelList.length > 0 && (
            <div className="px-3 pt-3 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-muted-foreground/80">
              Channels
            </div>
          )}
          {/* Regular channels */}
          {channelList.map((channel) => {
            const activityKey = `${networkId}:${channel}`;
            const unreadCount = unreadCounts.get(activityKey) || 0;
            return (
              <div
                key={channel}
                data-testid="channel-node"
                data-channel={channel}
                className={`px-2 py-1.5 mr-1 rounded-md cursor-pointer select-none flex items-center justify-between transition-all ${
                  selectedChannel === channel
                    ? 'cc-active-pane'
                    : 'hover:bg-accent/70'
                }`}
                style={{ transition: 'var(--transition-base)' }}
                onClick={() => handleChannelClick(channel)}
                onContextMenu={(e) => handleContextMenu(e, 'channel', channel)}
                onMouseDown={(e) => {
                  if (e.button === 2) {
                    e.preventDefault();
                  }
                }}
              >
                <span className={`text-sm truncate ${unreadCount > 0 ? 'font-semibold' : ''}`}>
                  {isChannelName(channel, useNetworkStore.getState().chanTypes[networkId]) ? (
                    <>
                      <span className="text-muted-foreground/50">{channel[0]}</span>
                      {channel.slice(1)}
                    </>
                  ) : (
                    channel
                  )}
                </span>
                {unreadCount > 0 && (
                  <span className="bg-primary text-primary-foreground text-xs px-1.5 min-w-[1.25rem] text-center rounded-full ml-2" title="Unread messages">
                    {unreadCount > 99 ? '99+' : unreadCount}
                  </span>
                )}
              </div>
            );
          })}
          {/* Private Message conversations */}
          {pmConversations.length > 0 && (
            <>
              <div className="px-3 pt-3 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-muted-foreground/80">
                Direct messages
              </div>
              {pmConversations.map((user) => {
                const pmKey = `pm:${user}`;
                const activityKey = `${networkId}:${pmKey}`;
                const unreadCount = unreadCounts.get(activityKey) || 0;
                const dotState = dmPresenceState(
                  user,
                  presence[networkId]?.[casefold(caseMapping?.[networkId] ?? '', user)],
                  connected
                );
                return (
                  <div
                    key={pmKey}
                    className={`px-2 py-1.5 mr-1 rounded-md cursor-pointer select-none flex items-center justify-between transition-all ${
                      selectedChannel === pmKey
                        ? 'cc-active-pane'
                        : 'hover:bg-accent/70'
                    }`}
                    style={{ transition: 'var(--transition-base)' }}
                    onClick={() => handleChannelClick(pmKey)}
                    onContextMenu={(e) => handleContextMenu(e, 'pm', undefined, user)}
                    onMouseDown={(e) => {
                      if (e.button === 2) {
                        e.preventDefault();
                      }
                    }}
                  >
                    <span className={`text-sm flex items-center gap-2 min-w-0 ${unreadCount > 0 ? 'font-semibold' : ''}`}>
                      <span
                        className="w-2 h-2 rounded-full flex-shrink-0"
                        title={dotState === 'online' ? 'Online' : dotState === 'offline' ? 'Offline' : 'Presence unknown'}
                        style={
                          dotState === 'online'
                            ? { background: 'var(--presence-online)' }
                            : dotState === 'offline'
                              ? { background: 'var(--presence-offline)' }
                              : { background: 'transparent', border: '1.5px solid var(--presence-offline)', opacity: 0.5 }
                        }
                      />
                      <span className="truncate">{user}</span>
                    </span>
                    {unreadCount > 0 && (
                      <span className="bg-primary text-primary-foreground text-xs px-1.5 min-w-[1.25rem] text-center rounded-full ml-2" title="Unread messages">
                        {unreadCount > 99 ? '99+' : unreadCount}
                      </span>
                    )}
                  </div>
                );
              })}
            </>
          )}
        </div>
      </div>

      {/* Context Menu */}
      {contextMenu.type && (
        <div
          ref={contextMenuRef}
          data-testid="context-menu"
          className="fixed border border-border rounded-lg shadow-[var(--shadow-lg)] z-50 w-auto py-1 bg-card/95 backdrop-blur-md"
          style={{
            left: contextMenu.x,
            top: contextMenu.y,
            backgroundColor: 'var(--card)',
            minWidth: '140px',
            maxWidth: '200px',
            transition: 'var(--transition-base)',
          }}
        >
          {contextMenu.type === 'channel' && contextMenu.channel && (
            <>
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                Channel
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (!contextMenu.channel) return;
                  const channelName = contextMenu.channel;
                  try {
                    await ToggleChannelAutoJoin(networkId, channelName);
                    const list = await GetOpenChannels(networkId);
                    if (list && Array.isArray(list)) {
                      setChannelList(list.map((c: Channel) => c.name));
                      const updatedChannel = list.find((c: Channel) => c.name === channelName);
                      if (updatedChannel) {
                        setContextMenuChannelData(updatedChannel);
                      }
                    }
                  } catch (error) {
                    console.error('Failed to toggle auto-join:', error);
                    alert(`Failed to toggle auto-join: ${error}`);
                  }
                }}
              >
                {contextMenuChannelData?.auto_join
                  ? 'Disable Auto-Join'
                  : 'Enable Auto-Join'}
              </button>
              {contextMenuIsJoined && (
                <button
                  className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                  style={{ transition: 'var(--transition-base)' }}
                  onClick={() => {
                    if (contextMenu.channel) {
                      handleLeaveChannel(contextMenu.channel);
                    }
                  }}
                >
                  Leave Channel
                </button>
              )}
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground"
                onClick={() => {
                  const nick = window.prompt(`Invite to ${contextMenu.channel}:`)?.trim();
                  if (nick && contextMenu.channel) {
                    void SendCommand(networkId, `/invite ${nick} ${contextMenu.channel}`);
                  }
                  setContextMenu({ x: 0, y: 0, type: null });
                }}
              >
                Invite user…
              </button>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground"
                onClick={() => {
                  if (contextMenu.channel) {
                    handleCloseChannel(contextMenu.channel);
                  }
                }}
              >
                Close Channel
              </button>
              {!contextMenuIsJoined && (
                <button
                  className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground"
                  onClick={async () => {
                    if (contextMenu.channel) {
                      const channelName = contextMenu.channel;
                      try {
                        await SendCommand(networkId, `/join ${channelName}`);
                        await refreshChannelList(networkId);
                      } catch (error) {
                        console.error('Failed to join channel:', error);
                        alert(`Failed to join channel: ${error}`);
                      }
                    }
                    setContextMenu({ x: 0, y: 0, type: null });
                  }}
                >
                  Join Channel
                </button>
              )}
            </>
          )}
          {contextMenu.type === 'pm' && contextMenu.user && (
            <>
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                Private Message
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (contextMenu.user) {
                    const user = contextMenu.user;
                    const pmKey = `pm:${user}`;
                    try {
                      // Close the PM conversation
                      await SetPrivateMessageOpen(networkId, user, false);
                      // Clear focus
                      await ClearPaneFocus(networkId, 'pm', user);
                      // If this PM is currently selected, switch to status
                      if (selectedChannel === pmKey) {
                        onSelectChannel(networkId, 'status');
                      }
                      // Refresh PM conversations list
                      await refreshPmConversations(networkId);
                    } catch (error) {
                      console.error('Failed to close PM:', error);
                      alert(`Failed to close PM: ${error}`);
                    }
                  }
                  setContextMenu({ x: 0, y: 0, type: null });
                }}
              >
                Close
              </button>
              <div className="border-t border-border my-1" />
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={() => {
                  if (contextMenu.user) {
                    onShowUserInfo(networkId, contextMenu.user);
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                Whois
              </button>
              <div className="border-t border-border my-1" />
              <div className="px-4 py-1 text-xs font-semibold text-muted-foreground uppercase">Invite to</div>
              {contextMenuJoinedChannels.length === 0 ? (
                <div className="w-full text-left px-4 py-2 text-sm text-muted-foreground">
                  No channels — join one first
                </div>
              ) : (
                contextMenuJoinedChannels.map((ch) => (
                  <button
                    key={ch.name}
                    className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                    style={{ transition: 'var(--transition-base)' }}
                    onClick={() => {
                      if (contextMenu.user) {
                        void SendCommand(networkId, `/invite ${contextMenu.user} ${ch.name}`);
                      }
                      setContextMenu({ x: 0, y: 0, type: null });
                    }}
                  >
                    {ch.name}
                  </button>
                ))
              )}
              <div className="border-t border-border my-1" />
              <div className="px-4 py-1 text-xs font-semibold text-muted-foreground uppercase">
                CTCP
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (contextMenu.user) {
                    try {
                      await SendCommand(networkId, `/version ${contextMenu.user}`);
                    } catch (error) {
                      console.error('Failed to send CTCP VERSION:', error);
                    }
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                CTCP Version
              </button>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (contextMenu.user) {
                    try {
                      await SendCommand(networkId, `/time ${contextMenu.user}`);
                    } catch (error) {
                      console.error('Failed to send CTCP TIME:', error);
                    }
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                CTCP Time
              </button>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (contextMenu.user) {
                    try {
                      await SendCommand(networkId, `/ping ${contextMenu.user}`);
                    } catch (error) {
                      console.error('Failed to send CTCP PING:', error);
                    }
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                CTCP Ping
              </button>
              <button
                className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                style={{ transition: 'var(--transition-base)' }}
                onClick={async () => {
                  if (contextMenu.user) {
                    try {
                      await SendCommand(networkId, `/clientinfo ${contextMenu.user}`);
                    } catch (error) {
                      console.error('Failed to send CTCP CLIENTINFO:', error);
                    }
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                CTCP ClientInfo
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}
