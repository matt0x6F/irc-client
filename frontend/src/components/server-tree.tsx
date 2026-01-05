import { useState, useEffect, useRef } from 'react';
import { main, storage } from '../../wailsjs/go/models';
import { GetChannels, GetServers, LeaveChannel, ToggleChannelAutoJoin, ToggleNetworkAutoConnect } from '../../wailsjs/go/main/App';

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
}

interface ContextMenu {
  x: number;
  y: number;
  type: 'server' | 'channel' | null;
  serverId?: number;
  channel?: string;
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
}: ServerTreeProps) {
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());
  const [channels, setChannels] = useState<Record<number, string[]>>({});
  const [channelData, setChannelData] = useState<Record<number, Record<string, Channel>>>({});
  const [contextMenu, setContextMenu] = useState<ContextMenu>({ x: 0, y: 0, type: null });
  const [contextMenuChannelData, setContextMenuChannelData] = useState<Channel | null>(null);
  const [contextMenuNetworkData, setContextMenuNetworkData] = useState<storage.Network | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState<{ serverId: number; serverName: string } | null>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);

  // Load channels for expanded networks
  useEffect(() => {
    expandedServers.forEach(async (networkId) => {
      // Always reload channels when network is expanded to ensure we have the latest data
      try {
        const channelList = await GetChannels(networkId);
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
    });
  }, [expandedServers, servers]);

  // Auto-expand selected network and always refresh channels
  useEffect(() => {
    if (selectedServer !== null) {
      setExpandedServers(prev => new Set(prev).add(selectedServer));
      // Always reload channels for selected server to ensure we have the latest data
      GetChannels(selectedServer).then(channelList => {
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

  const handleContextMenu = async (e: React.MouseEvent, type: 'server' | 'channel', serverId?: number, channel?: string) => {
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
                      {networkChannels.map((channel) => (
                        <div
                          key={channel}
                          className={`p-2 cursor-pointer hover:bg-accent select-none ${
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
                          <span className="text-sm">{channel}</span>
                        </div>
                      ))}
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
          className="fixed border border-border rounded shadow-lg z-50 min-w-[150px] py-1"
          style={{ 
            left: contextMenu.x, 
            top: contextMenu.y,
            backgroundColor: 'var(--background)',
            backdropFilter: 'blur(8px)',
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
                    const channelList = await GetChannels(serverId);
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
                    try {
                      await LeaveChannel(contextMenu.serverId, contextMenu.channel);
                      // Refresh channels after leaving
                      const channelList = await GetChannels(contextMenu.serverId);
                      if (channelList && Array.isArray(channelList)) {
                        setChannels(prev => ({
                          ...prev,
                          [contextMenu.serverId!]: channelList.map((c: Channel) => c.name),
                        }));
                        const channelMap: Record<string, Channel> = {};
                        channelList.forEach((c: Channel) => {
                          channelMap[c.name] = c;
                        });
                        setChannelData(prev => ({
                          ...prev,
                          [contextMenu.serverId!]: channelMap,
                        }));
                      }
                      // If we left the currently selected channel, switch to status
                      if (selectedServer === contextMenu.serverId && selectedChannel === contextMenu.channel) {
                        onSelectChannel(contextMenu.serverId, 'status');
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

