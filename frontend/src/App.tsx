import { useState, useEffect, useCallback, useRef } from 'react';
import { ConnectNetwork, GetNetworks, SendMessage, SendCommand, GetMessages, ListPlugins, GetConnectionStatus, DisconnectNetwork, DeleteNetwork, GetServers, GetChannelIDByName, GetChannelInfo, SetChannelOpen, GetPrivateMessages, GetPrivateMessageConversations, SetPrivateMessageOpen, GetLastOpenPane, SetPaneFocus, ClearPaneFocus, GetOpenChannels } from '../wailsjs/go/main/App';
import { main, storage } from '../wailsjs/go/models';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { ServerTree } from './components/server-tree';
import { MessageView } from './components/message-view';
import { InputArea } from './components/input-area';
import { SettingsModal } from './components/settings-modal';
import { ChannelInfo } from './components/channel-info';
import { TopicEditModal } from './components/topic-edit-modal';
import { ModeEditModal } from './components/mode-edit-modal';
import { UserInfo } from './components/user-info';

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
  const [showUserInfo, setShowUserInfo] = useState<{ networkId: number; nickname: string } | null>(null);
  const pendingJoinChannelRef = useRef<{ networkId: number; channel: string } | null>(null);
  // Track channels with unread activity (key: `${networkId}:${channelName}`)
  const [channelsWithActivity, setChannelsWithActivity] = useState<Set<string>>(new Set());
  const hasRestoredPaneRef = useRef<boolean>(false);

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
      // If selectedChannel starts with "pm:", it's a private message conversation
      // Otherwise, look up channel ID by name
      let channelId: number | null = null;
      if (selectedChannel !== 'status' && selectedChannel !== null) {
        if (selectedChannel.startsWith('pm:')) {
          // Private message conversation
          const user = selectedChannel.substring(3); // Remove "pm:" prefix
          const msgs = await GetPrivateMessages(selectedNetwork, user, 100);
          setMessages(msgs || []);
          return;
        } else {
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

  // Restore last open pane after networks are loaded (only once on initial load)
  useEffect(() => {
    if (hasRestoredPaneRef.current || networks.length === 0) {
      return;
    }
    
    const restoreLastPane = async () => {
      hasRestoredPaneRef.current = true;
      try {
        console.log('[App] Attempting to restore last open pane...');
        const lastPane = await GetLastOpenPane();
        console.log('[App] GetLastOpenPane result:', JSON.stringify(lastPane, null, 2));
        
        if (!lastPane) {
          console.log('[App] No last open pane found - checking all networks for open channels...');
          // Debug: Check all networks for open channels
          for (const network of networks) {
            try {
              const openChannels = await GetOpenChannels(network.id);
              console.log(`[App] Network ${network.id} (${network.name}) has ${openChannels.length} open channels:`, openChannels.map(c => c.name));
            } catch (err) {
              console.error(`[App] Failed to get open channels for network ${network.id}:`, err);
            }
          }
        }
        
        if (lastPane) {
          console.log('[App] Restoring last open pane:', JSON.stringify(lastPane, null, 2));
          // Verify the network still exists
          const networkExists = networks.some(n => n.id === lastPane.network_id);
          console.log('[App] Network exists check:', networkExists, 'for network ID:', lastPane.network_id);
          
          if (networkExists) {
            if (lastPane.type === 'channel') {
              // Try to verify and restore the channel with retries
              // Channels might not be loaded immediately on startup
              let channelFound = false;
              const maxRetries = 5;
              const retryDelay = 300;
              
              for (let attempt = 0; attempt < maxRetries; attempt++) {
                try {
                  await GetChannelIDByName(lastPane.network_id, lastPane.name);
                  console.log('[App] Channel verified, restoring:', lastPane.name, `(attempt ${attempt + 1})`);
                  channelFound = true;
                  
                  // Use setTimeout to ensure state updates happen after render
                  setTimeout(() => {
                    setSelectedNetwork(lastPane.network_id);
                    setSelectedChannel(lastPane.name);
                  }, 0);
                  
                  // Set focus using event-based method
                  try {
                    await SetPaneFocus(lastPane.network_id, 'channel', lastPane.name);
                  } catch (error) {
                    console.error('[App] Failed to set focus on restored channel:', error);
                  }
                  break; // Success, exit retry loop
                } catch (error) {
                  if (attempt < maxRetries - 1) {
                    console.log(`[App] Channel not found yet, retrying in ${retryDelay}ms (attempt ${attempt + 1}/${maxRetries}):`, lastPane.name);
                    await new Promise(resolve => setTimeout(resolve, retryDelay));
                  } else {
                    console.log('[App] Last open channel not found after retries:', lastPane.name, error);
                    // Try to find the channel by checking all open channels (case-insensitive fallback)
                    try {
                      const openChannels = await GetOpenChannels(lastPane.network_id);
                      const normalizedTargetName = lastPane.name.toLowerCase();
                      const matchingChannel = openChannels.find(ch => 
                        ch.name.toLowerCase() === normalizedTargetName
                      );
                      
                      if (matchingChannel) {
                        console.log('[App] Found channel via case-insensitive match:', matchingChannel.name);
                        setTimeout(() => {
                          setSelectedNetwork(lastPane.network_id);
                          setSelectedChannel(matchingChannel.name);
                        }, 0);
                        try {
                          await SetPaneFocus(lastPane.network_id, 'channel', matchingChannel.name);
                        } catch (err) {
                          console.error('[App] Failed to set focus on matched channel:', err);
                        }
                        channelFound = true;
                      }
                    } catch (fallbackError) {
                      console.log('[App] Fallback channel search failed:', fallbackError);
                    }
                    
                    // If still not found, restore to status window
                    if (!channelFound) {
                      const network = networks.find(n => n.id === lastPane.network_id);
                      if (network) {
                        setTimeout(() => {
                          setSelectedNetwork(lastPane.network_id);
                          setSelectedChannel('status');
                        }, 0);
                        try {
                          await SetPaneFocus(lastPane.network_id, 'status', 'status');
                        } catch (err) {
                          console.error('[App] Failed to set focus on fallback status:', err);
                        }
                      }
                    }
                  }
                }
              }
            } else if (lastPane.type === 'pm') {
              console.log('[App] Restoring PM conversation:', lastPane.name);
              setTimeout(() => {
                setSelectedNetwork(lastPane.network_id);
                setSelectedChannel(`pm:${lastPane.name}`);
              }, 0);
              // Set focus using event-based method
              try {
                await SetPaneFocus(lastPane.network_id, 'pm', lastPane.name);
              } catch (error) {
                console.error('[App] Failed to set focus on restored PM:', error);
              }
            }
          } else {
            console.log('[App] Last open pane network not found, skipping restoration');
            // Fallback: restore to first network's status window
            if (networks.length > 0) {
              setTimeout(() => {
                setSelectedNetwork(networks[0].id);
                setSelectedChannel('status');
              }, 0);
            }
          }
        } else {
          console.log('[App] No last open pane found, restoring to first network status');
          // Fallback: restore to first network's status window
          if (networks.length > 0) {
            setTimeout(() => {
              setSelectedNetwork(networks[0].id);
              setSelectedChannel('status');
            }, 0);
            try {
              await SetPaneFocus(networks[0].id, 'status', 'status');
            } catch (error) {
              console.error('[App] Failed to set focus on fallback status window:', error);
            }
          }
        }
      } catch (error) {
        console.error('[App] Failed to restore last open pane:', error);
        // Fallback: restore to first network's status window
        if (networks.length > 0) {
          setTimeout(() => {
            setSelectedNetwork(networks[0].id);
            setSelectedChannel('status');
          }, 0);
          try {
            await SetPaneFocus(networks[0].id, 'status', 'status');
          } catch (error) {
            console.error('[App] Failed to set focus on fallback status window:', error);
          }
        }
      }
    };
    
    // Small delay to ensure networks state is fully set and component is rendered
    const timeoutId = setTimeout(() => {
      restoreLastPane();
    }, 200);
    
    return () => clearTimeout(timeoutId);
  }, [networks]);

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
      const eventType = data?.type;
      const eventData = data?.data || {};
      const network = eventData.network;
      const target = eventData.target || eventData.channel;
      
      // Track activity for channels and PM conversations that aren't currently focused
      if (eventType === 'message.received' || eventType === 'message.sent') {
        // Find the network by address
        const networkObj = networks.find(n => n.address === network);
        if (networkObj && target && target !== 'status') {
          // Check if target is a channel (starts with # or &) or a user (PM)
          const isChannel = target.startsWith('#') || target.startsWith('&');
          
          // For received PMs, the target is your nickname, but we need the sender's nickname
          // For sent PMs, the target is the recipient's nickname
          let pmUser: string | null = null;
          if (!isChannel) {
            if (eventType === 'message.received') {
              // For received PMs, use the sender (user field) as the PM conversation key
              pmUser = eventData.user || null;
            } else if (eventType === 'message.sent') {
              // For sent PMs, the target is the recipient
              pmUser = target;
            }
          }
          
          const pmKey = pmUser ? `pm:${pmUser}` : null;
          const activityKey = isChannel 
            ? `${networkObj.id}:${target}` 
            : pmKey ? `${networkObj.id}:${pmKey}` : null;
          
          if (activityKey) {
            // Check if this channel/PM is currently focused
            const isFocused = selectedNetwork === networkObj.id && 
              (isChannel ? selectedChannel === target : selectedChannel === pmKey);
            
            if (!isFocused) {
              // Mark this channel/PM as having activity
              setChannelsWithActivity(prev => new Set(prev).add(activityKey));
            }
          }
        }
      }
      
      // Handle channel join/part events for pending join channel switching
      if (eventType === 'user.joined' || eventType === 'user.parted') {
        // Find the network by address (check both primary address and server addresses)
        let networkObj = networks.find(n => n.address === network);
        
        // If not found by primary address, check server addresses
        if (!networkObj) {
          // We'll need to check server addresses, but for now just try to find by any matching
          // The network address in events should match the primary address or one of the server addresses
          networkObj = networks.find(n => {
            // Check if network address matches
            if (n.address === network) return true;
            // In the future, we could also check server addresses here
            return false;
          });
        }
        
        if (networkObj) {
          // Check if this is our own join/part by comparing user to network nickname
          const user = eventData.user;
          const channel = eventData.channel || target;
          
          console.log('[App] Join/part event received:', {
            eventType,
            user,
            channel,
            network: networkObj.address,
            networkId: networkObj.id,
            ourNickname: networkObj.nickname,
            pendingJoinChannel: pendingJoinChannelRef.current
          });
          
          // Note: Channel sidebar updates are now handled by the channels-changed event
          // We only need to handle pending join channel switching here
          if (user && networkObj.nickname && user.toLowerCase() === networkObj.nickname.toLowerCase()) {
            
            // If this is a join event and we have a pending join for this channel, switch to it
            const pendingJoin = pendingJoinChannelRef.current;
            if (eventType === 'user.joined' && channel && pendingJoin) {
              console.log('[App] Checking if we should switch to joined channel:', {
                pendingNetworkId: pendingJoin.networkId,
                currentNetworkId: networkObj.id,
                networkMatch: pendingJoin.networkId === networkObj.id,
                pendingChannel: pendingJoin.channel,
                eventChannel: channel,
                channelMatch: pendingJoin.channel.toLowerCase() === channel.toLowerCase()
              });
              
              // Normalize channel names for comparison (ensure both have # prefix)
              const normalizeChannel = (ch: string) => {
                if (!ch) return '';
                return ch.startsWith('#') || ch.startsWith('&') ? ch.toLowerCase() : '#' + ch.toLowerCase();
              };
              
              const pendingChannelNormalized = normalizeChannel(pendingJoin.channel);
              const eventChannelNormalized = normalizeChannel(channel);
              
              if (pendingJoin.networkId === networkObj.id && 
                  pendingChannelNormalized === eventChannelNormalized) {
                console.log('[App] Successful join detected, switching to channel:', channel);
                // Capture values before setTimeout to avoid TypeScript errors
                const networkId = networkObj.id;
                const channelName = channel;
                // Wait a bit longer to ensure channel is in database and sidebar is updated
                setTimeout(async () => {
                  try {
                    // Verify channel exists before switching
                    await GetChannelIDByName(networkId, channelName);
                    console.log('[App] Channel verified, switching now');
                    setSelectedNetwork(networkId);
                    setSelectedChannel(channelName);
                    // Set focus using event-based method
                    try {
                      await SetPaneFocus(networkId, 'channel', channelName);
                    } catch (error) {
                      console.error('[App] Failed to set focus on joined channel:', error);
                    }
                    pendingJoinChannelRef.current = null;
                  } catch (error) {
                    console.log('[App] Channel not ready yet, retrying in 500ms');
                    // Retry once more after a longer delay
                    setTimeout(async () => {
                      try {
                        await GetChannelIDByName(networkId, channelName);
                        setSelectedNetwork(networkId);
                        setSelectedChannel(channelName);
                        // Set focus using event-based method
                        try {
                          await SetPaneFocus(networkId, 'channel', channelName);
                        } catch (error) {
                          console.error('[App] Failed to set focus on joined channel:', error);
                        }
                        pendingJoinChannelRef.current = null;
                      } catch (err) {
                        console.error('[App] Failed to switch to channel after retry:', err);
                        pendingJoinChannelRef.current = null;
                      }
                    }, 500);
                  }
                }, 300);
              } else {
                console.log('[App] Join event but conditions not met - not switching', {
                  networkMatch: pendingJoin.networkId === networkObj.id,
                  channelMatch: pendingChannelNormalized === eventChannelNormalized,
                  pendingNormalized: pendingChannelNormalized,
                  eventNormalized: eventChannelNormalized
                });
              }
            }
          }
        }
      }
      
      // Only refresh messages if the event is for the currently selected network/channel
      if (selectedNetwork === null) return;
      
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
      // Clear activity indicators for all channels in this network
      setChannelsWithActivity(prev => {
        const next = new Set(prev);
        for (const key of prev) {
          if (key.startsWith(`${networkId}:`)) {
            next.delete(key);
          }
        }
        return next;
      });
      console.log('Delete operation completed successfully');
    } catch (error) {
      console.error('Failed to delete:', error);
      alert(`Failed to delete: ${error}`);
      throw error;
    }
  };

  const handleSendMessage = async (message: string) => {
    if (selectedNetwork === null || selectedChannel === null) return;
    
    // Check if this is a slash command - if so, route to SendCommand regardless of channel or DM
    const trimmedMessage = message.trim();
    if (trimmedMessage.startsWith('/')) {
      try {
        let commandToSend = trimmedMessage;
        
        // For /me command, prepend channel or user context
        if (trimmedMessage.toLowerCase().startsWith('/me ') && selectedChannel && selectedChannel !== 'status') {
          const parts = trimmedMessage.substring(4).trim();
          // If it's a private message, use the username; otherwise use the channel
          if (selectedChannel.startsWith('pm:')) {
            const user = selectedChannel.substring(3); // Remove "pm:" prefix
            commandToSend = `/me ${user} ${parts}`;
          } else {
            // Encode channel in command: /me #channel action text
            commandToSend = `/me ${selectedChannel} ${parts}`;
          }
        }
        // For /part and /leave commands, inject current channel if not specified
        else if ((trimmedMessage.toLowerCase().startsWith('/part') || trimmedMessage.toLowerCase().startsWith('/leave')) && selectedChannel && selectedChannel !== 'status') {
          const isPart = trimmedMessage.toLowerCase().startsWith('/part');
          const cmdLength = isPart ? 5 : 6; // '/part' or '/leave'
          const rest = trimmedMessage.substring(cmdLength).trim();
          const parts = rest ? rest.split(/\s+/) : [];
          
          // If no args or first arg doesn't look like a channel (doesn't start with # or &), inject current channel
          if (parts.length === 0 || (!parts[0].startsWith('#') && !parts[0].startsWith('&'))) {
            // No channel specified, use current channel
            const cmd = isPart ? '/part' : '/leave';
            if (parts.length === 0) {
              // Just /part or /leave - part from current channel
              commandToSend = `${cmd} ${selectedChannel}`;
            } else {
              // /part reason or /leave reason - part from current channel with reason
              const reason = parts.join(' ');
              commandToSend = `${cmd} ${selectedChannel} ${reason}`;
            }
          }
          // Otherwise, channel is specified, use as-is
        }
        // For /join command, track the channel to switch to after successful join
        let joinTargetChannel: string | null = null;
        if (trimmedMessage.toLowerCase().startsWith('/join ') || trimmedMessage.toLowerCase() === '/join') {
          const rest = trimmedMessage.substring(5).trim(); // '/join' = 5 chars
          const parts = rest ? rest.split(/\s+/) : [];
          if (parts.length > 0 && (parts[0].startsWith('#') || parts[0].startsWith('&'))) {
            joinTargetChannel = parts[0];
            // Store pending join to switch after successful join
            console.log('[App] Setting pending join:', { networkId: selectedNetwork, channel: joinTargetChannel });
            pendingJoinChannelRef.current = { networkId: selectedNetwork, channel: joinTargetChannel };
            
            // Also try a fallback approach: poll for the channel to appear and switch
            const pollForChannel = async (attempts = 0) => {
              if (attempts > 10) {
                console.log('[App] Polling timeout - channel did not appear');
                pendingJoinChannelRef.current = null;
                return;
              }
              
              try {
                await GetChannelIDByName(selectedNetwork, joinTargetChannel!);
                console.log('[App] Channel found via polling, switching now');
                setSelectedNetwork(selectedNetwork);
                setSelectedChannel(joinTargetChannel);
                // Set focus using event-based method
                if (joinTargetChannel) {
                  try {
                    await SetPaneFocus(selectedNetwork, 'channel', joinTargetChannel);
                  } catch (error) {
                    console.error('[App] Failed to set focus on joined channel:', error);
                  }
                }
                pendingJoinChannelRef.current = null;
              } catch (error) {
                // Channel not ready yet, try again
                setTimeout(() => pollForChannel(attempts + 1), 300);
              }
            };
            
            // Start polling after a short delay
            setTimeout(() => pollForChannel(), 500);
            
            // Clear pending join after 5 seconds if join doesn't complete (timeout/failure)
            setTimeout(() => {
              if (pendingJoinChannelRef.current && 
                  pendingJoinChannelRef.current.networkId === selectedNetwork && 
                  pendingJoinChannelRef.current.channel === joinTargetChannel) {
                console.log('[App] Clearing pending join due to timeout');
                pendingJoinChannelRef.current = null;
              }
            }, 5000);
          }
        }
        
        // For /close command, inject current channel if not specified and switch to status after closing
        let closeTargetChannel: string | null = null;
        if (trimmedMessage.toLowerCase().startsWith('/close') && selectedChannel && selectedChannel !== 'status') {
          const rest = trimmedMessage.substring(6).trim(); // '/close' = 6 chars
          const parts = rest ? rest.split(/\s+/) : [];
          
          // Determine target channel
          if (parts.length === 0 || (!parts[0].startsWith('#') && !parts[0].startsWith('&'))) {
            // No channel specified, use current channel
            closeTargetChannel = selectedChannel;
            commandToSend = `/close ${selectedChannel}`;
          } else {
            // Channel is specified
            closeTargetChannel = parts[0];
            // Use as-is
          }
        }
        
        // For /query command, track the nickname to switch to PM view after command
        let queryTargetNickname: string | null = null;
        if (trimmedMessage.toLowerCase().startsWith('/query ') || trimmedMessage.toLowerCase().startsWith('/q ')) {
          const cmdLength = trimmedMessage.toLowerCase().startsWith('/query ') ? 7 : 3; // '/query ' = 7 chars, '/q ' = 3 chars
          const rest = trimmedMessage.substring(cmdLength).trim();
          const parts = rest ? rest.split(/\s+/) : [];
          if (parts.length > 0) {
            queryTargetNickname = parts[0];
          }
        }
        
        await SendCommand(selectedNetwork, commandToSend);
        
        // If /close was used on the current channel, clear focus and switch to status window
        if (closeTargetChannel && closeTargetChannel === selectedChannel) {
          // Clear focus using event-based method
          try {
            await ClearPaneFocus(selectedNetwork, 'channel', closeTargetChannel);
          } catch (error) {
            console.error('[App] Failed to clear focus from closed channel:', error);
          }
          // Clear activity indicator for closed channel
          const activityKey = `${selectedNetwork}:${closeTargetChannel}`;
          setChannelsWithActivity(prev => {
            const next = new Set(prev);
            next.delete(activityKey);
            return next;
          });
          setSelectedChannel('status');
          // Set focus on status window
          try {
            await SetPaneFocus(selectedNetwork, 'status', 'status');
          } catch (error) {
            console.error('[App] Failed to set focus on status window:', error);
          }
        }
        
        // If /query was used, switch to PM view
        if (queryTargetNickname) {
          const pmKey = `pm:${queryTargetNickname}`;
          setSelectedChannel(pmKey);
          // Set focus using event-based method
          try {
            await SetPaneFocus(selectedNetwork, 'pm', queryTargetNickname);
          } catch (error) {
            console.error('[App] Failed to set focus on PM:', error);
          }
        }
        
        // Refresh messages after command
        await loadMessages();
      } catch (error) {
        console.error('Failed to send command:', error);
        await loadMessages();
      }
      return;
    }
    
    // Check if this is a private message conversation
    if (selectedChannel.startsWith('pm:')) {
      const user = selectedChannel.substring(3); // Remove "pm:" prefix
      // Send message to user
      try {
        await SendMessage(selectedNetwork, user, message);
        // Refresh messages after sending
        setTimeout(() => {
          loadMessages();
        }, 100);
      } catch (error) {
        console.error('Failed to send private message:', error);
      }
      return;
    }
    
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
          channelsWithActivity={channelsWithActivity}
          onShowUserInfo={(networkId, nickname) => setShowUserInfo({ networkId, nickname })}
          onNetworkUpdate={loadNetworks}
          onSelectChannel={async (networkId, channel) => {
            // When switching channels/PMs, use event-based focus tracking
            // NOTE: We don't clear focus from the previous pane when switching - 
            // we only clear focus when explicitly closing (e.g., /close command).
            // This allows multiple panes to remain "open" and we track which was last focused.
            
            setSelectedNetwork(networkId);
            setSelectedChannel(channel);
            
            // Clear activity indicator for the selected channel/PM
            if (channel !== null && channel !== 'status') {
              const activityKey = `${networkId}:${channel}`;
              setChannelsWithActivity(prev => {
                const next = new Set(prev);
                next.delete(activityKey);
                return next;
              });
            }
            
            // Set focus on new pane using events (this updates the last_focused timestamp)
            if (channel !== null) {
              try {
                if (channel === 'status') {
                  await SetPaneFocus(networkId, 'status', 'status');
                } else if (channel.startsWith('pm:')) {
                  // It's a PM conversation
                  const user = channel.substring(3); // Remove "pm:" prefix
                  await SetPaneFocus(networkId, 'pm', user);
                } else {
                  // It's a channel
                  await SetPaneFocus(networkId, 'channel', channel);
                }
              } catch (error) {
                console.error('[App] Failed to set focus on pane:', error);
              }
            }
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
                  {selectedChannel && selectedChannel !== 'status' && !selectedChannel.startsWith('pm:') && (
                    <span className="ml-2 text-muted-foreground">
                      {selectedChannel.startsWith('#') || selectedChannel.startsWith('&') ? selectedChannel : `#${selectedChannel}`}
                    </span>
                  )}
                  {selectedChannel && selectedChannel.startsWith('pm:') && (
                    <span className="ml-2 text-muted-foreground">PM: {selectedChannel.substring(3)}</span>
                  )}
                  {selectedChannel === 'status' && (
                    <span className="ml-2 text-muted-foreground">Status</span>
                  )}
                </>
              )}
            </div>
          </div>
          {selectedChannel && selectedChannel !== 'status' && !selectedChannel.startsWith('pm:') && channelInfo?.channel && (
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
              <MessageView messages={messages} networkId={selectedNetwork} selectedChannel={selectedChannel} />
            ) : (
              <div className="flex items-center justify-center h-full text-muted-foreground">
                Select a network to start chatting
              </div>
            )}
          </div>

          {/* Channel Info Sidebar - only show for channels, not PMs or status */}
          {selectedChannel && selectedChannel !== 'status' && !selectedChannel.startsWith('pm:') && (
            <ChannelInfo 
              networkId={selectedNetwork} 
              channelName={selectedChannel}
              currentNickname={selectedNetwork !== null ? networks.find(n => n.id === selectedNetwork)?.nickname || null : null}
              onSendCommand={async (command: string) => {
                if (selectedNetwork !== null) {
                  await SendCommand(selectedNetwork, command);
                }
              }}
            />
          )}

          {/* User Info Panel - show when user info is requested */}
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
            placeholder={selectedChannel === 'status' ? 'Type a command (e.g., /join #channel, /msg user message) or raw IRC command...' : 'Type a message...'}
            networkId={selectedNetwork}
            channelName={selectedChannel}
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
