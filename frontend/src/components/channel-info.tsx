import { useState, useEffect, useRef, useCallback } from 'react';
import { GetChannelInfo } from '../../wailsjs/go/main/App';
import { main, storage } from '../../wailsjs/go/models';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { UserInfo } from './user-info';
import { useNicknameColors } from '../hooks/useNicknameColors';
import { Shield, Crown, Star, Mic, ShieldCheck } from 'lucide-react';

interface ChannelInfoProps {
  networkId: number | null;
  channelName: string | null;
  currentNickname: string | null;
  onSendCommand: (command: string) => Promise<void>;
}

interface ContextMenu {
  x: number;
  y: number;
  user: storage.ChannelUser | null;
}

export function ChannelInfo({ networkId, channelName, currentNickname, onSendCommand }: ChannelInfoProps) {
  const [channelInfo, setChannelInfo] = useState<main.ChannelInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [contextMenu, setContextMenu] = useState<ContextMenu | null>(null);
  const [showUserInfo, setShowUserInfo] = useState<{ nickname: string } | null>(null);
  const contextMenuRef = useRef<HTMLDivElement>(null);
  // Use refs to avoid stale closures in event listener
  const networkIdRef = useRef<number | null>(networkId);
  const channelNameRef = useRef<string | null>(channelName);
  // Track if we're waiting for NAMES response after a join
  const waitingForNamesRef = useRef<boolean>(false);
  const namesPollIntervalRef = useRef<number | null>(null);
  
  // Update refs when props change
  useEffect(() => {
    networkIdRef.current = networkId;
    channelNameRef.current = channelName;
  }, [networkId, channelName]);

  // Get users list for nickname colors (must be called before any conditional returns)
  const users = (channelInfo?.users || []) as storage.ChannelUser[];
  const nicknameColors = useNicknameColors(
    networkId, 
    users.map(u => u.nickname)
  );

  const loadChannelInfo = useCallback(async () => {
    if (networkId === null || channelName === null || channelName === 'status') {
      setChannelInfo(null);
      return;
    }

    setLoading(true);
    try {
      const info = await GetChannelInfo(networkId, channelName);
      setChannelInfo(info);
    } catch (error) {
      console.error('Failed to load channel info:', error);
      setChannelInfo(null);
    } finally {
      setLoading(false);
    }
  }, [networkId, channelName]);

  useEffect(() => {
    if (networkId === null || channelName === null || channelName === 'status') {
      setChannelInfo(null);
      return;
    }

    // Load initial channel info
    loadChannelInfo();
    
    // Normalize channel names for comparison (case-insensitive, ensure # prefix)
    const normalizeChannel = (ch: string | null | undefined): string => {
      if (!ch) return '';
      const normalized = ch.toLowerCase().trim();
      return normalized.startsWith('#') || normalized.startsWith('&') ? normalized : '#' + normalized;
    };
    
    const currentChannelNormalized = normalizeChannel(channelName);
    
    // Listen for user events to refresh immediately - this is the ONLY update mechanism
    const unsubscribe = EventsOn('message-event', (data: any) => {
      const eventType = data?.type;
      const eventData = data?.data || {};
      
      // Get current values from refs to avoid stale closures
      const currentNetworkId = networkIdRef.current;
      const currentChannelName = channelNameRef.current;
      
      // Skip if we don't have valid network/channel
      if (currentNetworkId === null || currentChannelName === null || currentChannelName === 'status') {
        return;
      }
      
      // Refresh on user join/part/quit/kick/nick events or when NAMES list completes
      if (eventType === 'user.joined' || eventType === 'user.parted' || eventType === 'user.quit' || 
          eventType === 'user.kicked' || eventType === 'user.nick' || eventType === 'channel.names.complete') {
        const eventNetworkId = eventData.networkId;
        
        // Check network ID match first (convert to number for comparison)
        const networkMatch = Number(eventNetworkId) === Number(currentNetworkId);
        if (!networkMatch) {
          return; // Not for this network
        }
        
        // For channel.names.complete, always refresh if it's for this network
        // The channel name matching might fail due to case differences, so we refresh anyway
        if (eventType === 'channel.names.complete') {
          console.log('[ChannelInfo] Refreshing on channel.names.complete', {
            eventChannel: eventData.channel,
            currentChannel: currentChannelName,
            networkId: currentNetworkId
          });
          // Stop polling since we got the NAMES response
          if (namesPollIntervalRef.current) {
            clearInterval(namesPollIntervalRef.current);
            namesPollIntervalRef.current = null;
          }
          waitingForNamesRef.current = false;
          loadChannelInfo();
          return;
        }
        
        // If we joined and are waiting for NAMES, start polling
        if (eventType === 'user.joined') {
          const target = eventData.channel || eventData.target;
          if (target) {
            const eventChannelNormalized = normalizeChannel(target);
            const currentChannelNormalized = normalizeChannel(currentChannelName);
            // If it's a join to the current channel, refresh immediately and start polling
            if (eventChannelNormalized === currentChannelNormalized) {
              // Refresh immediately
              loadChannelInfo();
              // If it might be our own join, start polling for NAMES response
              // We'll poll for up to 60 seconds, refreshing every 2 seconds
              if (namesPollIntervalRef.current) {
                clearInterval(namesPollIntervalRef.current);
              }
              waitingForNamesRef.current = true;
              let pollCount = 0;
              const maxPolls = 30; // 30 * 2 seconds = 60 seconds max
              namesPollIntervalRef.current = setInterval(() => {
                pollCount++;
                if (!waitingForNamesRef.current || pollCount >= maxPolls) {
                  if (namesPollIntervalRef.current) {
                    clearInterval(namesPollIntervalRef.current);
                    namesPollIntervalRef.current = null;
                  }
                  waitingForNamesRef.current = false;
                  return;
                }
                console.log('[ChannelInfo] Polling for user list update after join (attempt', pollCount, ')');
                loadChannelInfo();
              }, 2000);
            }
          }
        }
        
        // For channel-specific events, check if it's for the current channel
        const target = eventData.channel || eventData.target;
        
        if (target) {
          const eventChannelNormalized = normalizeChannel(target);
          const currentChannelNormalized = normalizeChannel(currentChannelName);
          
          // Only refresh if it's for the current channel (or NICK which affects all channels)
          if (eventType === 'user.nick' || eventChannelNormalized === currentChannelNormalized) {
            loadChannelInfo();
          }
        } else if (eventType === 'user.nick' || eventType === 'user.quit') {
          // NICK and QUIT events affect all channels, so always refresh
          loadChannelInfo();
        }
      }
    });
    
    return () => {
      unsubscribe();
      // Clean up polling interval
      if (namesPollIntervalRef.current) {
        clearInterval(namesPollIntervalRef.current);
        namesPollIntervalRef.current = null;
      }
      waitingForNamesRef.current = false;
    };
  }, [networkId, channelName, loadChannelInfo]);

  // Close context menu on outside click
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (contextMenuRef.current && !contextMenuRef.current.contains(event.target as Node)) {
        setContextMenu(null);
      }
    };

    if (contextMenu) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => {
        document.removeEventListener('mousedown', handleClickOutside);
      };
    }
  }, [contextMenu]);

  // Close context menu on Escape key
  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setContextMenu(null);
      }
    };

    if (contextMenu) {
      document.addEventListener('keydown', handleEscape);
      return () => {
        document.removeEventListener('keydown', handleEscape);
      };
    }
  }, [contextMenu]);

  if (networkId === null || channelName === null || channelName === 'status') {
    return null;
  }

  if (loading && !channelInfo) {
    return (
      <div className="w-64 border-l border-border p-4">
        <div className="text-sm text-muted-foreground">Loading channel info...</div>
      </div>
    );
  }

  if (!channelInfo || !channelInfo.channel) {
    return (
      <div className="w-64 border-l border-border p-4">
        <div className="text-sm text-muted-foreground">Channel not found</div>
      </div>
    );
  }

  const channel = channelInfo.channel;
  // users is already defined above for the hook

  // Group users by mode prefix
  const usersByMode: Record<string, storage.ChannelUser[]> = {
    '@': [], // ops
    '%': [], // halfops
    '&': [], // admins
    '~': [], // owners
    '+': [], // voiced
    '': [],  // regular users
  };

  users.forEach(user => {
    const mode = user.modes || '';
    if (mode.includes('@')) {
      usersByMode['@'].push(user);
    } else if (mode.includes('%')) {
      usersByMode['%'].push(user);
    } else if (mode.includes('&')) {
      usersByMode['&'].push(user);
    } else if (mode.includes('~')) {
      usersByMode['~'].push(user);
    } else if (mode.includes('+')) {
      usersByMode['+'].push(user);
    } else {
      usersByMode[''].push(user);
    }
  });

  // Sort users within each group
  Object.keys(usersByMode).forEach(key => {
    usersByMode[key].sort((a, b) => a.nickname.localeCompare(b.nickname));
  });

  // Find current user in the user list
  const currentUser = currentNickname 
    ? users.find(u => u.nickname.toLowerCase() === currentNickname.toLowerCase())
    : null;

  // Get server capabilities
  const capabilities = channelInfo?.capabilities;
  const prefixMap = capabilities?.prefix || {};
  const chanModes = capabilities?.chanmodes || '';

  // Helper function to get mode letter from prefix character
  const getModeFromPrefix = (prefixChar: string): string | null => {
    return prefixMap[prefixChar] || null;
  };

  // Helper function to check if user has a specific mode
  const userHasMode = (user: storage.ChannelUser, modeLetter: string): boolean => {
    if (!user.modes) return false;
    // Check if user has any prefix that maps to this mode
    for (const prefixChar of user.modes) {
      if (getModeFromPrefix(prefixChar) === modeLetter) {
        return true;
      }
    }
    return false;
  };

  // Get highest mode letter for current user
  const getCurrentUserHighestMode = (): string | null => {
    if (!currentUser || !currentUser.modes) return null;
    
    // Common mode hierarchy (higher to lower): q, a, o, h, v
    const modeHierarchy = ['q', 'a', 'o', 'h', 'v'];
    
    for (const mode of modeHierarchy) {
      if (userHasMode(currentUser, mode)) {
        return mode;
      }
    }
    
    return null;
  };

  // Permission check functions using server capabilities
  // Fallback: if capabilities not available, check for '@' prefix (op) directly
  const canKick = (): boolean => {
    if (!currentUser) return false;
    
    // Fallback: if no capabilities, check for '@' prefix directly
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || false;
    }
    
    const highestMode = getCurrentUserHighestMode();
    // Typically requires op (o) or higher, but varies by server
    return highestMode === 'q' || highestMode === 'a' || highestMode === 'o';
  };

  const canBan = (): boolean => {
    if (!currentUser) return false;
    
    // Fallback: if no capabilities, check for '@' prefix directly
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || false;
    }
    
    const highestMode = getCurrentUserHighestMode();
    // Ban modes typically require op or higher
    return highestMode === 'q' || highestMode === 'a' || highestMode === 'o';
  };

  const canOp = (): boolean => {
    if (!currentUser) return false;
    
    // Fallback: if no capabilities, check for '@' prefix directly
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || false;
    }
    
    const highestMode = getCurrentUserHighestMode();
    // Setting op mode requires op or higher
    return highestMode === 'q' || highestMode === 'a' || highestMode === 'o';
  };

  const canVoice = (): boolean => {
    if (!currentUser) return false;
    
    // Fallback: if no capabilities, check for '@' or '%' prefix directly
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || currentUser.modes?.includes('%') || false;
    }
    
    const highestMode = getCurrentUserHighestMode();
    // Voice mode may be set by op, halfop, or higher (varies by server)
    return highestMode === 'q' || highestMode === 'a' || highestMode === 'o' || highestMode === 'h';
  };

  const canInvite = (): boolean => {
    if (!currentUser) return false;
    
    // Fallback: if no capabilities, check for '@' prefix directly
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || false;
    }
    
    const highestMode = getCurrentUserHighestMode();
    // INVITE typically requires op or higher, or voice in invite-only channels
    return highestMode === 'q' || highestMode === 'a' || highestMode === 'o' || highestMode === 'h' || highestMode === 'v';
  };

  // Context menu handler
  const handleContextMenu = (e: React.MouseEvent, user: storage.ChannelUser) => {
    e.preventDefault();
    e.stopPropagation();
    
    // Prevent text selection
    if (window.getSelection) {
      window.getSelection()?.removeAllRanges();
    }
    
    setContextMenu({
      x: e.clientX,
      y: e.clientY,
      user,
    });
  };

  // Command handlers
  const handleKick = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/kick ${channelName} ${nickname}`);
    setContextMenu(null);
  };

  const handleKickBan = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/kick ${channelName} ${nickname}`);
    // Ban mask: nickname!*@*
    await onSendCommand(`/ban ${channelName} ${nickname}!*@*`);
    setContextMenu(null);
  };

  const handleBan = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/ban ${channelName} ${nickname}!*@*`);
    setContextMenu(null);
  };

  const handleUnban = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/unban ${channelName} ${nickname}!*@*`);
    setContextMenu(null);
  };

  const handleOp = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/op ${channelName} ${nickname}`);
    setContextMenu(null);
  };

  const handleDeop = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/deop ${channelName} ${nickname}`);
    setContextMenu(null);
  };

  const handleVoice = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/voice ${channelName} ${nickname}`);
    setContextMenu(null);
  };

  const handleDevoice = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/devoice ${channelName} ${nickname}`);
    setContextMenu(null);
  };

  const handleInvite = async (nickname: string) => {
    if (!channelName) return;
    await onSendCommand(`/invite ${nickname} ${channelName}`);
    setContextMenu(null);
  };

  // Check if target user is self
  const isSelf = (user: storage.ChannelUser | null): boolean => {
    if (!user || !currentNickname) return false;
    return user.nickname.toLowerCase() === currentNickname.toLowerCase();
  };

  return (
    <div className="w-full flex flex-col h-full bg-card/30">
      {/* Channel Header */}
      <div className="p-4 border-b border-border bg-card/50">
        <h3 className="font-semibold text-base">{channel.name}</h3>
      </div>

      {/* Users List */}
      <div className="flex-1 overflow-y-auto p-4">
        <div className="text-xs font-semibold text-muted-foreground mb-3 uppercase tracking-wider">
          Users ({users.length})
        </div>
        
        {/* Owners */}
        {usersByMode['~'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Owners</div>
            {usersByMode['~'].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <Crown className="inline w-3.5 h-3.5 text-purple-600" />
                <span 
                  className="ml-1.5 font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Admins */}
        {usersByMode['&'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Admins</div>
            {usersByMode['&'].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <ShieldCheck className="inline w-3.5 h-3.5 text-red-600" />
                <span 
                  className="ml-1.5 font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Operators */}
        {usersByMode['@'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Operators</div>
            {usersByMode['@'].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <Shield className="inline w-3.5 h-3.5 text-red-500" />
                <span 
                  className="ml-1.5 font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Halfops */}
        {usersByMode['%'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Halfops</div>
            {usersByMode['%'].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <Star className="inline w-3.5 h-3.5 text-orange-500" />
                <span 
                  className="ml-1.5 font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Voiced */}
        {usersByMode['+'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Voiced</div>
            {usersByMode['+'].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <Mic className="inline w-3.5 h-3.5 text-blue-500" />
                <span 
                  className="ml-1.5 font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Regular Users */}
        {usersByMode[''].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Regular Users</div>
            {usersByMode[''].map(user => (
              <div 
                key={user.id} 
                className="text-sm py-1.5 px-2 cursor-pointer hover:bg-accent/70 rounded-md transition-all"
                style={{ transition: 'var(--transition-base)' }}
                onContextMenu={(e) => handleContextMenu(e, user)}
              >
                <span
                  className="font-medium"
                  style={{ color: nicknameColors.get(user.nickname) || undefined }}
                  title={nicknameColors.get(user.nickname) ? `Color: ${nicknameColors.get(user.nickname)}` : 'No color'}
                >
                  {user.nickname}
                </span>
              </div>
            ))}
          </div>
        )}

        {users.length === 0 && (
          <div className="text-sm text-muted-foreground">No users found</div>
        )}
      </div>

      {/* Context Menu */}
      {contextMenu && contextMenu.user && (
        <div
          ref={contextMenuRef}
          className="fixed z-50 bg-card border border-border rounded-lg shadow-[var(--shadow-lg)] min-w-[180px] backdrop-blur-md"
          style={{
            left: `${contextMenu.x}px`,
            top: `${contextMenu.y}px`,
            backgroundColor: 'var(--card)',
            transition: 'var(--transition-base)',
          }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="py-1">
            {/* Only show commands if user has permissions and target is not self */}
            {!isSelf(contextMenu.user) && (
              <>
                {/* Moderation Section */}
                {canKick() && (
                  <>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleKick(contextMenu.user!.nickname)}
                    >
                      Kick
                    </button>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleKickBan(contextMenu.user!.nickname)}
                    >
                      Kick & Ban
                    </button>
                    <div className="border-t border-border my-1" />
                  </>
                )}

                {/* Bans Section */}
                {canBan() && (
                  <>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleBan(contextMenu.user!.nickname)}
                    >
                      Ban
                    </button>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleUnban(contextMenu.user!.nickname)}
                    >
                      Unban
                    </button>
                    <div className="border-t border-border my-1" />
                  </>
                )}

                {/* Permissions Section */}
                {canOp() && (
                  <>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleOp(contextMenu.user!.nickname)}
                    >
                      Op
                    </button>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleDeop(contextMenu.user!.nickname)}
                    >
                      Deop
                    </button>
                  </>
                )}

                {canVoice() && (
                  <>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleVoice(contextMenu.user!.nickname)}
                    >
                      Voice
                    </button>
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleDevoice(contextMenu.user!.nickname)}
                    >
                      Devoice
                    </button>
                  </>
                )}

                {/* Other Section */}
                {canInvite() && (
                  <>
                    {(canOp() || canVoice()) && <div className="border-t border-border my-1" />}
                    <button
                      className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
                      style={{ transition: 'var(--transition-base)' }}
                      onClick={() => handleInvite(contextMenu.user!.nickname)}
                    >
                      Invite
                    </button>
                  </>
                )}

                {/* Show message if no commands available */}
                {!canKick() && !canBan() && !canOp() && !canVoice() && !canInvite() && (
                  <div className="px-4 py-2 text-sm text-muted-foreground">
                    No operator commands available
                  </div>
                )}
              </>
            )}

            {/* Show message if trying to operate on self */}
            {isSelf(contextMenu.user) && (
              <div className="px-4 py-2 text-sm text-muted-foreground">
                Cannot operate on yourself
              </div>
            )}

            {/* User Info & CTCP options - available for all users */}
            <div className="border-t border-border my-1" />
            <button
              className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
              style={{ transition: 'var(--transition-base)' }}
              onClick={() => {
                if (contextMenu.user) {
                  setShowUserInfo({ nickname: contextMenu.user.nickname });
                  setContextMenu(null);
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
              className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
              style={{ transition: 'var(--transition-base)' }}
              onClick={() => {
                if (contextMenu.user && networkId !== null) {
                  onSendCommand(`/version ${contextMenu.user.nickname}`);
                  setContextMenu(null);
                }
              }}
            >
              CTCP Version
            </button>
            <button
              className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
              style={{ transition: 'var(--transition-base)' }}
              onClick={() => {
                if (contextMenu.user && networkId !== null) {
                  onSendCommand(`/time ${contextMenu.user.nickname}`);
                  setContextMenu(null);
                }
              }}
            >
              CTCP Time
            </button>
            <button
              className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
              style={{ transition: 'var(--transition-base)' }}
              onClick={() => {
                if (contextMenu.user && networkId !== null) {
                  onSendCommand(`/ping ${contextMenu.user.nickname}`);
                  setContextMenu(null);
                }
              }}
            >
              CTCP Ping
            </button>
            <button
              className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground "
              style={{ transition: 'var(--transition-base)' }}
              onClick={() => {
                if (contextMenu.user && networkId !== null) {
                  onSendCommand(`/clientinfo ${contextMenu.user.nickname}`);
                  setContextMenu(null);
                }
              }}
            >
              CTCP ClientInfo
            </button>
          </div>
        </div>
      )}

      {/* User Info Panel */}
      {showUserInfo && (
        <UserInfo
          networkId={networkId}
          nickname={showUserInfo.nickname}
          onClose={() => setShowUserInfo(null)}
        />
      )}
    </div>
  );
}

