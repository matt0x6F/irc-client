import { useState, useEffect, useRef } from 'react';
import { main, storage } from '../../wailsjs/go/models';
import { GetChannels, GetJoinedChannels, GetOpenChannels, GetServers, LeaveChannel, ToggleChannelAutoJoin, ToggleNetworkAutoConnect, SetChannelOpen, GetPrivateMessageConversations, SendCommand, SetPrivateMessageOpen, ClearPaneFocus } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

type Channel = storage.Channel;

interface ServerTreeProps {
  servers: storage.Network[];
  selectedServer: number | null;
  selectedChannel: string | null;
  onSelectServer: (id: number | null) => void;
  onSelectChannel: (networkId: number, channel: string | null) => void;
  onConnect: (config: main.NetworkConfig) => Promise<void>;
  onDisconnect: (id: number) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  connectionStatus: Record<number, boolean>;
  channelsWithActivity: Set<string>;
  onShowUserInfo: (networkId: number, nickname: string) => void;
}

interface ContextMenu {
  x: number;
  y: number;
  type: 'server' | 'channel' | 'pm' | null;
  serverId?: number;
  channel?: string;
  user?: string; // For PM context menu
}

export function ServerTree({
  servers,
  selectedServer,
  selectedChannel,
  onSelectServer,
  onSelectChannel,
  onConnect,
  onDisconnect,
  onDelete,
  connectionStatus,
  channelsWithActivity,
  onShowUserInfo,
}: ServerTreeProps) {
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());
  const [channels, setChannels] = useState<Record<number, string[]>>({});
  const [channelData, setChannelData] = useState<Record<number, Record<string, Channel>>>({});
  const [pmConversations, setPmConversations] = useState<Record<number, string[]>>({});
  const [contextMenu, setContextMenu] = useState<ContextMenu>({ x: 0, y: 0, type: null });
  const [contextMenuChannelData, setContextMenuChannelData] = useState<Channel | null>(null);
  const [contextMenuNetworkData, setContextMenuNetworkData] = useState<storage.Network | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState<{ serverId: number; serverName: string } | null>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);

  // Load channels and PM conversations for expanded networks
  useEffect(() => {
    expandedServers.forEach(async (networkId) => {
      // Always reload channels when network is expanded to ensure we have the latest data
      try {
        const channelList = await GetOpenChannels(networkId);
        if (channelList && Array.isArray(channelList)) {
          setChannels(prev => ({
            ...prev,
            [networkId]: channelList.map((c: Channel) => c.name),
          }));
          // Store full channel data for accessing auto_join
          const channelMap: Record<string, Channel> = {};
          channelList.forEach((c: Channel) => {
            channelMap[c.name] = c;
          });
          setChannelData(prev => ({
            ...prev,
            [networkId]: channelMap,
          }));
        } else {
          setChannels(prev => ({
            ...prev,
            [networkId]: [],
          }));
        }
      } catch (error) {
        console.error('Failed to load channels:', error);
        setChannels(prev => ({
          ...prev,
          [networkId]: [],
        }));
      }

      // Load PM conversations
      try {
        const pmList = await GetPrivateMessageConversations(networkId, true);
        if (pmList && Array.isArray(pmList)) {
          setPmConversations(prev => ({
            ...prev,
            [networkId]: pmList,
          }));
        } else {
          setPmConversations(prev => ({
            ...prev,
            [networkId]: [],
          }));
        }
      } catch (error) {
        console.error('Failed to load PM conversations:', error);
        setPmConversations(prev => ({
          ...prev,
          [networkId]: [],
        }));
      }
    });
  }, [expandedServers, servers]);

  // Auto-expand selected network and always refresh channels
  useEffect(() => {
    if (selectedServer !== null) {
      setExpandedServers(prev => new Set(prev).add(selectedServer));
      // Always reload channels for selected server to ensure we have the latest data
      GetOpenChannels(selectedServer).then(channelList => {
        if (channelList && Array.isArray(channelList)) {
          setChannels(prev => ({
            ...prev,
            [selectedServer]: channelList.map((c: Channel) => c.name),
          }));
          const channelMap: Record<string, Channel> = {};
          channelList.forEach((c: Channel) => {
            channelMap[c.name] = c;
          });
          setChannelData(prev => ({
            ...prev,
            [selectedServer]: channelMap,
          }));
        }
      }).catch(err => {
        console.error('Failed to load channels:', err);
      });
    }
  }, [selectedServer, servers]);

  // Listen for channels changed events for immediate sidebar updates
  useEffect(() => {
    const unsubscribe = EventsOn('channels-changed', (data: any) => {
      const networkId = data?.networkId;
      
      if (networkId && typeof networkId === 'number') {
        console.log('[ServerTree] Received channels-changed event for network:', networkId);
        // Refresh channels immediately for this network
        GetOpenChannels(networkId).then(channelList => {
          console.log('[ServerTree] Refreshed channel list after channels-changed:', channelList?.map((c: Channel) => c.name), 'count:', channelList?.length);
          if (channelList && Array.isArray(channelList)) {
            setChannels(prev => ({
              ...prev,
              [networkId]: channelList.map((c: Channel) => c.name),
            }));
            const channelMap: Record<string, Channel> = {};
            channelList.forEach((c: Channel) => {
              channelMap[c.name] = c;
            });
            setChannelData(prev => ({
              ...prev,
              [networkId]: channelMap,
            }));
          } else {
            setChannels(prev => ({
              ...prev,
              [networkId]: [],
            }));
          }
        }).catch(err => {
          console.error('[ServerTree] Failed to refresh channels after channels-changed:', err);
        });
      }
    });
    
    return () => unsubscribe();
  }, []);

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

  const toggleServer = (serverId: number) => {
    setExpandedServers(prev => {
      const next = new Set(prev);
      if (next.has(serverId)) {
        next.delete(serverId);
      } else {
        next.add(serverId);
      }
      return next;
    });
  };

  const handleContextMenu = async (e: React.MouseEvent, type: 'server' | 'channel' | 'pm', serverId?: number, channel?: string, user?: string) => {
    e.preventDefault();
    e.stopPropagation();
    // Prevent text selection
    if (window.getSelection) {
      window.getSelection()?.removeAllRanges();
    }
    
    // If it's a server context menu, load the network data
    if (type === 'server' && serverId) {
      const network = servers.find(n => n.id === serverId);
      if (network) {
        setContextMenuNetworkData(network);
      }
    }
    
    // If it's a channel context menu, load the channel data
    let channelData: Channel | null = null;
    if (type === 'channel' && serverId && channel) {
      try {
        const channelList = await GetChannels(serverId);
        const foundChannel = channelList?.find((c: Channel) => c.name === channel);
        if (foundChannel) {
          channelData = foundChannel;
        }
      } catch (error) {
        console.error('Failed to load channel data for context menu:', error);
      }
    }
    
    setContextMenuChannelData(channelData);
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      type,
      serverId,
      channel,
      user,
    });
  };

  const handleServerClick = (networkId: number) => {
    toggleServer(networkId);
    onSelectServer(networkId);
    onSelectChannel(networkId, 'status');
  };

  const handleChannelClick = (networkId: number, channel: string) => {
    onSelectServer(networkId);
    onSelectChannel(networkId, channel);
  };

  // Refresh PM conversations when channels change event is received
  useEffect(() => {
    const unsubscribe = EventsOn('channels-changed', async (data: any) => {
      const networkId = data?.networkId;
      if (networkId && expandedServers.has(networkId)) {
        try {
          const pmList = await GetPrivateMessageConversations(networkId, true);
          if (pmList && Array.isArray(pmList)) {
            setPmConversations(prev => ({
              ...prev,
              [networkId]: pmList,
            }));
          }
        } catch (error) {
          console.error('Failed to refresh PM conversations:', error);
        }
      }
    });
    return () => unsubscribe();
  }, [expandedServers]);

  // Refresh PM conversations when UI pane events are received (PM opened/closed)
  useEffect(() => {
    const unsubscribe = EventsOn('ui-pane-event', async (data: any) => {
      const networkId = data?.networkId;
      const paneType = data?.paneType;
      
      // Only refresh if it's a PM pane event and the network is expanded
      if (networkId && paneType === 'pm' && expandedServers.has(networkId)) {
        try {
          const pmList = await GetPrivateMessageConversations(networkId, true);
          if (pmList && Array.isArray(pmList)) {
            setPmConversations(prev => ({
              ...prev,
              [networkId]: pmList,
            }));
          } else {
            setPmConversations(prev => ({
              ...prev,
              [networkId]: [],
            }));
          }
        } catch (error) {
          console.error('Failed to refresh PM conversations after pane event:', error);
        }
      }
    });
    return () => unsubscribe();
  }, [expandedServers]);

  return (
    <div className="h-full flex flex-col relative">
        <div className="p-4 border-b border-border">
        <h2 className="font-semibold">Networks</h2>
      </div>

      <div className="flex-1 overflow-y-auto">
        {servers && servers.length > 0 ? (
          <div className="py-2">
            {servers.map((network) => {
              const isExpanded = expandedServers.has(network.id);
              const isConnected = connectionStatus[network.id] || false;
              const isSelected = selectedServer === network.id;
              const networkChannels = channels[network.id] || [];

              return (
                <div key={network.id}>
                  <div
                    className={`flex items-center p-2 cursor-pointer hover:bg-accent select-none ${
                      isSelected ? 'bg-accent border-l-2 border-primary' : ''
                    }`}
                    onClick={() => handleServerClick(network.id)}
                    onContextMenu={(e) => handleContextMenu(e, 'server', network.id)}
                    onMouseDown={(e) => {
                      // Prevent text selection on right-click drag
                      if (e.button === 2) {
                        e.preventDefault();
                      }
                    }}
                  >
                    <span className="mr-2 text-xs">
                      {isExpanded ? '▼' : '▶'}
                    </span>
                    <span className={`w-2 h-2 rounded-full mr-2 ${
                      isConnected ? 'bg-green-500' : 'bg-gray-400'
                    }`} title={isConnected ? 'Connected' : 'Disconnected'} />
                    <span className="flex-1 font-medium">{network.name}</span>
                  </div>
                  {isExpanded && (
                    <div className="pl-6">
                      {/* Status channel */}
                      <div
                        className={`p-2 cursor-pointer hover:bg-accent select-none ${
                          isSelected && selectedChannel === 'status' ? 'bg-accent border-l-2 border-primary' : ''
                        }`}
                        onClick={() => handleChannelClick(network.id, 'status')}
                        onMouseDown={(e) => {
                          if (e.button === 2) {
                            e.preventDefault();
                          }
                        }}
                      >
                        <span className="text-sm text-muted-foreground">Status</span>
                      </div>
                      {/* Regular channels */}
                      {networkChannels.map((channel) => {
                        const activityKey = `${network.id}:${channel}`;
                        const hasActivity = channelsWithActivity.has(activityKey);
                        return (
                          <div
                            key={channel}
                            className={`p-2 cursor-pointer hover:bg-accent select-none flex items-center justify-between ${
                              isSelected && selectedChannel === channel ? 'bg-accent border-l-2 border-primary' : ''
                            }`}
                            onClick={() => handleChannelClick(network.id, channel)}
                            onContextMenu={(e) => handleContextMenu(e, 'channel', network.id, channel)}
                            onMouseDown={(e) => {
                              if (e.button === 2) {
                                e.preventDefault();
                              }
                            }}
                          >
                            <span className={`text-sm ${hasActivity ? 'font-semibold' : ''}`}>{channel}</span>
                            {hasActivity && (
                              <span className="w-2 h-2 rounded-full bg-primary ml-2" title="Unread activity" />
                            )}
                          </div>
                        );
                      })}
                      {/* Private Message conversations */}
                      {(pmConversations[network.id] || []).length > 0 && (
                        <>
                          <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                            Private Messages
                          </div>
                          {(pmConversations[network.id] || []).map((user) => {
                            const pmKey = `pm:${user}`;
                            const activityKey = `${network.id}:${pmKey}`;
                            const hasActivity = channelsWithActivity.has(activityKey);
                            return (
                              <div
                                key={pmKey}
                                className={`p-2 cursor-pointer hover:bg-accent select-none flex items-center justify-between ${
                                  isSelected && selectedChannel === pmKey ? 'bg-accent border-l-2 border-primary' : ''
                                }`}
                                onClick={() => handleChannelClick(network.id, pmKey)}
                                onContextMenu={(e) => handleContextMenu(e, 'pm', network.id, undefined, user)}
                                onMouseDown={(e) => {
                                  if (e.button === 2) {
                                    e.preventDefault();
                                  }
                                }}
                              >
                                <span className={`text-sm ${hasActivity ? 'font-semibold' : ''}`}>💬 {user}</span>
                                {hasActivity && (
                                  <span className="w-2 h-2 rounded-full bg-primary ml-2" title="Unread activity" />
                                )}
                              </div>
                            );
                          })}
                        </>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        ) : (
          <div className="p-4 text-center text-muted-foreground text-sm">
            No networks configured
          </div>
        )}
      </div>

      {/* Context Menu */}
      {contextMenu.type && (
        <div
          ref={contextMenuRef}
          className="fixed border border-border rounded shadow-lg z-50 w-auto py-1"
          style={{ 
            left: contextMenu.x, 
            top: contextMenu.y,
            backgroundColor: 'var(--background)',
            backdropFilter: 'blur(8px)',
            minWidth: '140px',
            maxWidth: '200px',
          }}
        >
          {contextMenu.type === 'server' && contextMenu.serverId && (
            <>
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                Network
              </div>
              {connectionStatus[contextMenu.serverId] ? (
                <button
                  className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                  onClick={() => {
                    onDisconnect(contextMenu.serverId!);
                    setContextMenu({ x: 0, y: 0, type: null });
                  }}
                >
                  Disconnect
                </button>
              ) : (
                <button
                  className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground disabled:opacity-50 disabled:cursor-not-allowed"
                  disabled={contextMenu.serverId !== undefined && contextMenu.serverId in connectionStatus && connectionStatus[contextMenu.serverId]}
                  onClick={async () => {
                    if (!contextMenu.serverId) return;
                    if (connectionStatus[contextMenu.serverId]) {
                      setContextMenu({ x: 0, y: 0, type: null });
                      return; // Already connected
                    }
                    const network = servers.find(n => n.id === contextMenu.serverId);
                    // Close menu immediately
                    setContextMenu({ x: 0, y: 0, type: null });
                    if (network) {
                      try {
                        // Load server addresses from database
                        const dbServers = await GetServers(network.id);
                        const configData: any = {
                          name: network.name,
                          nickname: network.nickname,
                          username: network.username,
                          realname: network.realname,
                          password: network.password,
                          sasl_enabled: network.sasl_enabled || false,
                          sasl_mechanism: network.sasl_mechanism || '',
                          sasl_username: network.sasl_username || '',
                          sasl_password: network.sasl_password || '',
                          sasl_external_cert: network.sasl_external_cert || '',
                        };
                        
                        // Use servers from database if available, otherwise fall back to legacy fields
                        if (dbServers && dbServers.length > 0) {
                          configData.servers = dbServers.map(srv => ({
                            address: srv.address,
                            port: srv.port,
                            tls: srv.tls,
                            order: srv.order,
                          }));
                        } else {
                          // Fallback to legacy single address fields
                          configData.address = network.address;
                          configData.port = network.port;
                          configData.tls = network.tls;
                        }
                        
                        const config = main.NetworkConfig.createFrom(configData);
                        await onConnect(config);
                      } catch (error) {
                        console.error('Failed to connect:', error);
                        alert(`Failed to connect: ${error}`);
                      }
                    }
                  }}
                >
                  Connect
                </button>
              )}
              <div className="border-t border-border my-1" />
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (!contextMenu.serverId) return;
                  const serverId = contextMenu.serverId;
                  try {
                    console.log('Toggling auto-connect for network:', serverId);
                    await ToggleNetworkAutoConnect(serverId);
                    console.log('Toggle successful, reloading networks...');
                    // Reload networks to update the UI
                    const updatedNetwork = servers.find(n => n.id === serverId);
                    if (updatedNetwork) {
                      setContextMenuNetworkData(storage.Network.createFrom({ ...updatedNetwork, auto_connect: !updatedNetwork.auto_connect }));
                    }
                  } catch (error) {
                    console.error('Failed to toggle auto-connect:', error);
                    alert(`Failed to toggle auto-connect: ${error}`);
                  }
                  setContextMenu({ x: 0, y: 0, type: null });
                }}
              >
                {contextMenuNetworkData?.auto_connect
                  ? 'Disable Auto-Connect'
                  : 'Enable Auto-Connect'}
              </button>
              <div className="border-t border-border my-1" />
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-destructive hover:text-destructive-foreground text-foreground"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  const network = servers.find(n => n.id === contextMenu.serverId);
                  setContextMenu({ x: 0, y: 0, type: null });
                  if (network) {
                    setShowDeleteConfirm({ serverId: network.id, serverName: network.name });
                  }
                }}
              >
                Delete
              </button>
            </>
          )}
          {contextMenu.type === 'channel' && contextMenu.serverId && contextMenu.channel && (
            <>
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                Channel
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (!contextMenu.serverId || !contextMenu.channel) return;
                  const serverId = contextMenu.serverId;
                  const channelName = contextMenu.channel;
                  const currentAutoJoin = contextMenuChannelData?.auto_join || false;
                  try {
                    console.log('Toggling auto-join for channel:', channelName, 'on network:', serverId, 'current:', currentAutoJoin);
                    await ToggleChannelAutoJoin(serverId, channelName);
                    console.log('Toggle successful, reloading channels...');
                    // Reload channels to update the UI
                      const channelList = await GetOpenChannels(serverId);
                    console.log('Reloaded channels:', channelList);
                    if (channelList && Array.isArray(channelList)) {
                      const channelMap: Record<string, Channel> = {};
                      channelList.forEach((c: Channel) => {
                        channelMap[c.name] = c;
                      });
                      setChannelData(prev => ({
                        ...prev,
                        [serverId]: channelMap,
                      }));
                      // Also update the channels list
                      setChannels(prev => ({
                        ...prev,
                        [serverId]: channelList.map((c: Channel) => c.name),
                      }));
                      // Update context menu channel data
                      const updatedChannel = channelList.find((c: Channel) => c.name === channelName);
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
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.channel) {
                    const serverId = contextMenu.serverId; // Capture for TypeScript
                    const channelName = contextMenu.channel; // Capture for TypeScript
                    try {
                      await LeaveChannel(serverId, channelName);
                      // Manually refresh channels after leaving - try multiple times to ensure it works
                      const refreshAfterLeave = (attempt = 1) => {
                        GetOpenChannels(serverId).then(channelList => {
                          console.log(`[ServerTree] Manually refreshed after LeaveChannel (attempt ${attempt}):`, channelList?.map((c: Channel) => c.name), 'count:', channelList?.length);
                          if (channelList && Array.isArray(channelList)) {
                            setChannels(prev => ({
                              ...prev,
                              [serverId]: channelList.map((c: Channel) => c.name),
                            }));
                            const channelMap: Record<string, Channel> = {};
                            channelList.forEach((c: Channel) => {
                              channelMap[c.name] = c;
                            });
                            setChannelData(prev => ({
                              ...prev,
                              [serverId]: channelMap,
                            }));
                            
                            // If channel is still in list and this is first attempt, retry
                            const channelStillThere = channelList.some((c: Channel) => c.name === channelName);
                            if (channelStillThere && attempt < 3) {
                              console.log('[ServerTree] Channel still in list, retrying refresh...');
                              setTimeout(() => refreshAfterLeave(attempt + 1), 200);
                            }
                          } else {
                            setChannels(prev => ({
                              ...prev,
                              [serverId]: [],
                            }));
                          }
                        }).catch(err => {
                          console.error(`[ServerTree] Failed to manually refresh after leave (attempt ${attempt}):`, err);
                          if (attempt < 3) {
                            setTimeout(() => refreshAfterLeave(attempt + 1), 200);
                          }
                        });
                      };
                      
                      // Try immediately, then retry a couple times
                      setTimeout(() => refreshAfterLeave(1), 100);
                      setTimeout(() => refreshAfterLeave(2), 300);
                      setTimeout(() => refreshAfterLeave(3), 500);
                      // If we left the currently selected channel, switch to status
                      if (selectedServer === serverId && selectedChannel === channelName) {
                        onSelectChannel(serverId, 'status');
                      }
                    } catch (error) {
                      console.error('Failed to leave channel:', error);
                      alert(`Failed to leave channel: ${error}`);
                    }
                  }
                  setContextMenu({ x: 0, y: 0, type: null });
                }}
              >
                Leave Channel
              </button>
            </>
          )}
          {contextMenu.type === 'pm' && contextMenu.serverId && contextMenu.user && (
            <>
              <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">
                Private Message
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.user) {
                    const serverId = contextMenu.serverId;
                    const user = contextMenu.user;
                    const pmKey = `pm:${user}`;
                    try {
                      // Close the PM conversation
                      await SetPrivateMessageOpen(serverId, user, false);
                      // Clear focus
                      await ClearPaneFocus(serverId, 'pm', user);
                      // If this PM is currently selected, switch to status
                      if (selectedServer === serverId && selectedChannel === pmKey) {
                        onSelectChannel(serverId, 'status');
                      }
                      // Refresh PM conversations list
                      const pmList = await GetPrivateMessageConversations(serverId, true);
                      if (pmList && Array.isArray(pmList)) {
                        setPmConversations(prev => ({
                          ...prev,
                          [serverId]: pmList,
                        }));
                      } else {
                        setPmConversations(prev => ({
                          ...prev,
                          [serverId]: [],
                        }));
                      }
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
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={() => {
                  if (contextMenu.serverId && contextMenu.user) {
                    onShowUserInfo(contextMenu.serverId, contextMenu.user);
                    setContextMenu({ x: 0, y: 0, type: null });
                  }
                }}
              >
                Whois
              </button>
              <div className="border-t border-border my-1" />
              <div className="px-4 py-1 text-xs font-semibold text-muted-foreground uppercase">
                CTCP
              </div>
              <button
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.user) {
                    try {
                      await SendCommand(contextMenu.serverId, `/version ${contextMenu.user}`);
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
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.user) {
                    try {
                      await SendCommand(contextMenu.serverId, `/time ${contextMenu.user}`);
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
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.user) {
                    try {
                      await SendCommand(contextMenu.serverId, `/ping ${contextMenu.user}`);
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
                className="w-full text-left px-4 py-2 text-sm hover:bg-accent text-foreground"
                onClick={async () => {
                  if (contextMenu.serverId && contextMenu.user) {
                    try {
                      await SendCommand(contextMenu.serverId, `/clientinfo ${contextMenu.user}`);
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

      {/* Delete Confirmation Dialog */}
      {showDeleteConfirm && (
        <div 
          className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" 
          onClick={() => setShowDeleteConfirm(null)}
          style={{ backgroundColor: 'rgba(0, 0, 0, 0.5)' }}
        >
          <div 
            className="bg-background border border-border rounded-lg shadow-xl p-6 max-w-md w-full mx-4"
            onClick={(e) => e.stopPropagation()}
            style={{ backgroundColor: 'var(--background)' }}
          >
            <h3 className="text-lg font-semibold mb-2 text-foreground">Delete Network</h3>
            <p className="text-muted-foreground mb-6">
              Are you sure you want to delete "{showDeleteConfirm.serverName}"? This will also delete all associated channels and messages.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                className="px-4 py-2 text-sm border border-border rounded hover:bg-accent text-foreground"
                onClick={() => setShowDeleteConfirm(null)}
              >
                Cancel
              </button>
              <button
                className="px-4 py-2 text-sm bg-destructive text-destructive-foreground rounded hover:bg-destructive/90 font-medium"
                onClick={async () => {
                  const { serverId } = showDeleteConfirm;
                  setShowDeleteConfirm(null);
                  try {
                    console.log('Calling onDelete with serverId:', serverId);
                    await onDelete(serverId);
                    console.log('onDelete completed');
                  } catch (error) {
                    console.error('Failed to delete:', error);
                    alert(`Failed to delete server: ${error}`);
                  }
                }}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

