import { useState, useEffect, useCallback } from 'react';
import { ConnectNetwork, GetNetworks, SendMessage, SendCommand, GetMessages, ListPlugins, GetConnectionStatus, DisconnectNetwork, DeleteNetwork, GetServers, GetChannelIDByName, GetChannelInfo } from '../wailsjs/go/main/App';
import { main, storage } from '../wailsjs/go/models';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { ServerTree } from './components/server-tree';
import { MessageView } from './components/message-view';
import { InputArea } from './components/input-area';
import { SettingsModal } from './components/settings-modal';
import { ChannelInfo } from './components/channel-info';
import { TopicEditModal } from './components/topic-edit-modal';
import { ModeEditModal } from './components/mode-edit-modal';

function App() {
  const [networks, setNetworks] = useState<storage.Network[]>([]);
  const [selectedNetwork, setSelectedNetwork] = useState<number | null>(null);
  const [selectedChannel, setSelectedChannel] = useState<string | null>(null);
  const [messages, setMessages] = useState<storage.Message[]>([]);
  const [showSettings, setShowSettings] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<Record<number, boolean>>({});
  const [channelInfo, setChannelInfo] = useState<main.ChannelInfo | null>(null);
  const [showTopicModal, setShowTopicModal] = useState(false);
  const [showModeModal, setShowModeModal] = useState(false);

  useEffect(() => {
    loadNetworks();
    // Refresh networks periodically to catch changes from other instances
    const interval = setInterval(() => {
      loadNetworks();
    }, 5000);
    
    // Listen for menu events to open settings
    const unsubscribe = EventsOn('open-settings', () => {
      console.log('[App] Received open-settings event from menu');
      setShowSettings(true);
    });
    
    return () => {
      clearInterval(interval);
      unsubscribe();
    };
  }, []);

  const loadNetworks = async () => {
    try {
      const networkList = await GetNetworks();
      setNetworks(networkList || []);
      
      // Load connection status for all networks
      if (networkList && networkList.length > 0) {
        const statusPromises = networkList.map(async (network) => {
          try {
            const connected = await GetConnectionStatus(network.id);
            return { networkId: network.id, connected };
          } catch (error) {
            console.error(`Failed to load connection status for network ${network.id}:`, error);
            return { networkId: network.id, connected: false };
          }
        });
        
        const statuses = await Promise.all(statusPromises);
        const statusMap: Record<number, boolean> = {};
        statuses.forEach(({ networkId, connected }) => {
          statusMap[networkId] = connected;
        });
        setConnectionStatus(prev => ({ ...prev, ...statusMap }));
      }
    } catch (error) {
      console.error('Failed to load networks:', error);
      setNetworks([]);
    }
  };

  const loadMessages = useCallback(async () => {
    if (selectedNetwork === null) return;
    try {
      // If selectedChannel is "status" (string), pass null to get status messages
      // Otherwise, look up channel ID by name
      let channelId: number | null = null;
      if (selectedChannel !== 'status' && selectedChannel !== null) {
        try {
          const id = await GetChannelIDByName(selectedNetwork, selectedChannel);
          // GetChannelIDByName returns a number (the channel ID) or throws if not found
          channelId = id as number;
        } catch (error) {
          console.error('Failed to get channel ID:', error);
          // If channel not found, show empty messages
          setMessages([]);
          return;
        }
      }
      const msgs = await GetMessages(selectedNetwork, channelId, 100);
      setMessages(msgs || []);
    } catch (error) {
      console.error('Failed to load messages:', error);
      setMessages([]);
    }
  }, [selectedNetwork, selectedChannel]);

  const loadConnectionStatus = async () => {
    if (selectedNetwork === null) return;
    try {
      const connected = await GetConnectionStatus(selectedNetwork);
      setConnectionStatus(prev => ({ ...prev, [selectedNetwork]: connected }));
    } catch (error) {
      console.error('Failed to load connection status:', error);
    }
  };

  const loadChannelInfo = useCallback(async () => {
    if (selectedNetwork === null || selectedChannel === null || selectedChannel === 'status') {
      setChannelInfo(null);
      return;
    }
    try {
      const info = await GetChannelInfo(selectedNetwork, selectedChannel);
      setChannelInfo(info);
    } catch (error) {
      console.error('Failed to load channel info:', error);
      setChannelInfo(null);
    }
  }, [selectedNetwork, selectedChannel]);

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
  }, [selectedNetwork, selectedChannel, loadMessages, loadChannelInfo]);

  // Listen for connection status events
  useEffect(() => {
    const unsubscribe = EventsOn('connection-status', (data: any) => {
      const networkId = data?.networkId;
      const connected = data?.connected;
      
      if (networkId !== undefined && typeof connected === 'boolean') {
        console.log('[App] Received connection status event:', { networkId, connected });
        setConnectionStatus(prev => ({ ...prev, [networkId]: connected }));
      }
    });
    
    return () => unsubscribe();
  }, []);

  // Listen for message events for real-time updates
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      // Only refresh if the event is for the currently selected network/channel
      if (selectedNetwork === null) return;
      
      const eventType = data?.type;
      const eventData = data?.data || {};
      const network = eventData.network;
      const target = eventData.target || eventData.channel;
      
      // Check if this event is relevant to the current view
      const currentNetwork = networks.find(n => n.id === selectedNetwork);
      if (currentNetwork && network === currentNetwork.address) {
        // Handle message received events
        if (eventType === 'message.received') {
          // For received messages, check if we're viewing that channel
          if (target && selectedChannel === target) {
            console.log('[App] Received message event for current channel, refreshing messages');
            loadMessages();
          }
        } else if (eventType === 'message.sent') {
          // For sent messages, check if we're viewing that channel
          if (target && selectedChannel === target) {
            console.log('[App] Received sent message event for current channel, refreshing messages');
            loadMessages();
          }
        } else if (target && selectedChannel === target) {
          // For other events (join/part/quit), refresh messages
          loadMessages();
          // For user join/part events, also trigger a channel info refresh
          if (eventType === 'user.joined' || eventType === 'user.parted' || eventType === 'user.quit') {
            // Channel info will auto-refresh via its own polling, but we can force it
            // by triggering a re-render or state change
          }
        } else if (eventData.channel === null && selectedChannel === 'status') {
          // Status message
          loadMessages();
        }
      }
    });
    
    return () => unsubscribe();
  }, [selectedNetwork, selectedChannel, networks, loadMessages]);

  // Listen for topic and mode change events
  useEffect(() => {
    const unsubscribe = EventsOn('message-event', (data: any) => {
      if (selectedNetwork === null || selectedChannel === null || selectedChannel === 'status') return;
      
      const eventType = data?.type;
      const eventData = data?.data || {};
      const network = eventData.network;
      const channel = eventData.channel;
      
      // Check if this event is relevant to the current view
      const currentNetwork = networks.find(n => n.id === selectedNetwork);
      if (currentNetwork && network === currentNetwork.address && channel === selectedChannel) {
        if (eventType === 'channel.topic' || eventType === 'channel.mode') {
          console.log('[App] Received topic/mode change event, refreshing channel info');
          loadChannelInfo();
        }
      }
    });
    
    return () => unsubscribe();
  }, [selectedNetwork, selectedChannel, networks, loadChannelInfo]);

  const handleConnect = async (config: main.NetworkConfig) => {
    console.log('[handleConnect] Called with config:', config);
    // Check if already connected to this network
    const existingNetwork = networks.find(n => 
      (n.address === config.address && n.port === config.port) || n.name === config.name
    );
    if (existingNetwork && connectionStatus[existingNetwork.id]) {
      console.log('[handleConnect] Already connected to this network, skipping');
      return;
    }
    
    try {
      console.log('[handleConnect] Calling ConnectNetwork...');
      await ConnectNetwork(config);
      console.log('[handleConnect] ConnectNetwork completed, loading networks...');
      await loadNetworks();
      // Refresh connection status after connecting
      if (existingNetwork) {
        await loadConnectionStatus();
      }
    } catch (error) {
      console.error('[handleConnect] Failed to connect:', error);
      alert(`Failed to connect: ${error}`);
    }
  };

  const handleDisconnect = async (networkId: number) => {
    try {
      await DisconnectNetwork(networkId);
      await loadNetworks();
      await loadConnectionStatus();
    } catch (error) {
      console.error('Failed to disconnect:', error);
      alert(`Failed to disconnect: ${error}`);
    }
  };

  const handleDelete = async (networkId: number) => {
    console.log('handleDelete called with networkId:', networkId);
    try {
      console.log('Calling DeleteNetwork API with networkId:', networkId);
      await DeleteNetwork(networkId);
      console.log('DeleteNetwork API call completed');
      await loadNetworks();
      console.log('Networks reloaded');
      if (selectedNetwork === networkId) {
        setSelectedNetwork(null);
        setSelectedChannel(null);
      }
      console.log('Delete operation completed successfully');
    } catch (error) {
      console.error('Failed to delete:', error);
      alert(`Failed to delete: ${error}`);
      throw error;
    }
  };

  const handleSendMessage = async (message: string) => {
    if (selectedNetwork === null || selectedChannel === null) return;
    
    // Optimistic UI update: show the message immediately
    const currentNetwork = networks.find(n => n.id === selectedNetwork);
    if (currentNetwork && selectedChannel !== 'status') {
      let channelId: number | undefined = undefined;
      
      // Try to get channel ID for optimistic message
      try {
        const id = await GetChannelIDByName(selectedNetwork, selectedChannel);
        channelId = id as number;
      } catch (error) {
        // Channel ID lookup failed, will be corrected on refresh
      }
      
      // Create optimistic message using the proper factory method
      const optimisticMessage = storage.Message.createFrom({
        id: Date.now(), // Temporary ID
        network_id: selectedNetwork,
        channel_id: channelId,
        user: currentNetwork.nickname || 'You',
        message: message,
        message_type: 'privmsg',
        timestamp: new Date().toISOString(),
        raw_line: '',
      });
      
      // Add optimistic message to the list immediately
      setMessages(prev => [...prev, optimisticMessage]);
    }
    
    try {
      // If status window, use SendCommand instead
      if (selectedChannel === 'status') {
        await SendCommand(selectedNetwork, message);
        // For status, refresh immediately since we don't have optimistic UI
        await loadMessages();
      } else {
        await SendMessage(selectedNetwork, selectedChannel, message);
        // Don't refresh immediately - optimistic message is already showing
        // The event system will trigger a refresh once the message is in the database
        // Use a small delay to allow the buffer to flush, then refresh
        setTimeout(() => {
          loadMessages();
        }, 100);
      }
    } catch (error) {
      console.error('Failed to send message:', error);
      // On error, remove the optimistic message and refresh
      await loadMessages();
    }
  };

  return (
    <div className="flex h-screen bg-background">
      {/* Network Tree Sidebar */}
      <div className="w-64 border-r border-border resize-x overflow-auto min-w-[200px] max-w-[400px]">
        <ServerTree
          servers={networks}
          selectedServer={selectedNetwork}
          selectedChannel={selectedChannel}
          onSelectServer={setSelectedNetwork}
          onSelectChannel={(networkId, channel) => {
            setSelectedNetwork(networkId);
            setSelectedChannel(channel);
          }}
          onConnect={handleConnect}
          onDisconnect={handleDisconnect}
          onDelete={handleDelete}
          connectionStatus={connectionStatus}
        />
      </div>

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <div className="border-b border-border">
          <div className="h-12 flex items-center justify-between px-4">
            <div className="flex items-center gap-2">
              {selectedNetwork !== null && (
                <>
                  <span className="font-semibold">
                    {networks.find(n => n.id === selectedNetwork)?.name || 'Unknown'}
                  </span>
                  <span className={`w-2 h-2 rounded-full ${
                    connectionStatus[selectedNetwork] ? 'bg-green-500' : 'bg-gray-400'
                  }`} title={connectionStatus[selectedNetwork] ? 'Connected' : 'Disconnected'} />
                  {selectedChannel && selectedChannel !== 'status' && (
                    <span className="ml-2 text-muted-foreground">#{selectedChannel}</span>
                  )}
                  {selectedChannel === 'status' && (
                    <span className="ml-2 text-muted-foreground">Status</span>
                  )}
                </>
              )}
            </div>
          </div>
          {selectedChannel && selectedChannel !== 'status' && channelInfo?.channel && (
            <div className="px-4 pb-2 flex items-center gap-4 text-sm">
              {channelInfo.channel.modes && (
                <button
                  onClick={() => setShowModeModal(true)}
                  className="text-muted-foreground hover:text-foreground cursor-pointer"
                  title="Click to edit modes"
                >
                  Modes: {channelInfo.channel.modes}
                </button>
              )}
              <button
                onClick={() => setShowTopicModal(true)}
                className="text-muted-foreground hover:text-foreground cursor-pointer italic flex-1 text-left truncate"
                title="Click to edit topic"
              >
                {channelInfo.channel.topic || 'No topic set'}
              </button>
            </div>
          )}
        </div>

        {/* Content Area with Messages and Channel Info */}
        <div className="flex-1 flex overflow-hidden">
          {/* Message View */}
          <div className="flex-1 overflow-y-auto">
            {selectedNetwork !== null ? (
              <MessageView messages={messages} />
            ) : (
              <div className="flex items-center justify-center h-full text-muted-foreground">
                Select a network to start chatting
              </div>
            )}
          </div>

          {/* Channel Info Sidebar */}
          <ChannelInfo networkId={selectedNetwork} channelName={selectedChannel} />
        </div>

        {/* Input Area */}
        {selectedNetwork !== null && selectedChannel !== null && (
          <InputArea 
            onSendMessage={handleSendMessage}
            placeholder={selectedChannel === 'status' ? 'Type a command (e.g., /join #channel, /msg user message) or raw IRC command...' : 'Type a message...'}
          />
        )}
      </div>

      {/* Settings Modal */}
      {showSettings && (
        <SettingsModal
          onClose={() => setShowSettings(false)}
          onServerUpdate={() => {
            loadNetworks();
            loadConnectionStatus();
          }}
        />
      )}

      {/* Topic Edit Modal */}
      {showTopicModal && selectedNetwork !== null && selectedChannel !== null && selectedChannel !== 'status' && channelInfo?.channel && (
        <TopicEditModal
          networkId={selectedNetwork}
          channelName={selectedChannel}
          currentTopic={channelInfo.channel.topic || ''}
          onClose={() => setShowTopicModal(false)}
          onUpdate={loadChannelInfo}
        />
      )}

      {/* Mode Edit Modal */}
      {showModeModal && selectedNetwork !== null && selectedChannel !== null && selectedChannel !== 'status' && channelInfo?.channel && (
        <ModeEditModal
          networkId={selectedNetwork}
          channelName={selectedChannel}
          currentModes={channelInfo.channel.modes || ''}
          onClose={() => setShowModeModal(false)}
          onUpdate={loadChannelInfo}
        />
      )}
    </div>
  );
}

export default App;
