import { useState, useEffect } from 'react';
import { GetChannelInfo } from '../../wailsjs/go/main/App';
import { main, storage } from '../../wailsjs/go/models';
import { EventsOn } from '../../wailsjs/runtime/runtime';

interface ChannelInfoProps {
  networkId: number | null;
  channelName: string | null;
}

export function ChannelInfo({ networkId, channelName }: ChannelInfoProps) {
  const [channelInfo, setChannelInfo] = useState<main.ChannelInfo | null>(null);
  const [loading, setLoading] = useState(false);

  const loadChannelInfo = async () => {
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
  };

  useEffect(() => {
    if (networkId === null || channelName === null || channelName === 'status') {
      setChannelInfo(null);
      return;
    }

    loadChannelInfo();
    // Refresh channel info periodically
    const interval = setInterval(loadChannelInfo, 5000);
    
    // Listen for user events to refresh immediately
    const unsubscribe = EventsOn('message-event', (data: any) => {
      const eventType = data?.type;
      const eventData = data?.data || {};
      
      // Refresh on user join/part/quit events or when NAMES list completes
      if (eventType === 'user.joined' || eventType === 'user.parted' || eventType === 'user.quit' || eventType === 'channel.names.complete') {
        const target = eventData.channel;
        const eventNetworkId = eventData.networkId;
        
        // Match both channel name and network ID
        // Network ID is now included in the event data for direct comparison
        if (target === channelName && eventNetworkId === networkId) {
          // Database write is verified before event emission, so refresh immediately
          loadChannelInfo();
        }
      }
    });
    
    return () => {
      clearInterval(interval);
      unsubscribe();
    };
  }, [networkId, channelName]);

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
  const users = (channelInfo.users || []) as storage.ChannelUser[];

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

  return (
    <div className="w-64 border-l border-border flex flex-col h-full bg-muted/30">
      {/* Channel Header */}
      <div className="p-4 border-b border-border">
        <h3 className="font-semibold text-sm">{channel.name}</h3>
      </div>

      {/* Users List */}
      <div className="flex-1 overflow-y-auto p-4">
        <div className="text-xs font-semibold text-muted-foreground mb-2 uppercase">
          Users ({users.length})
        </div>
        
        {/* Owners */}
        {usersByMode['~'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Owners</div>
            {usersByMode['~'].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                <span className="text-purple-600">~</span>
                <span className="ml-1">{user.nickname}</span>
              </div>
            ))}
          </div>
        )}

        {/* Admins */}
        {usersByMode['&'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Admins</div>
            {usersByMode['&'].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                <span className="text-red-600">&</span>
                <span className="ml-1">{user.nickname}</span>
              </div>
            ))}
          </div>
        )}

        {/* Operators */}
        {usersByMode['@'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Operators</div>
            {usersByMode['@'].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                <span className="text-red-500">@</span>
                <span className="ml-1">{user.nickname}</span>
              </div>
            ))}
          </div>
        )}

        {/* Halfops */}
        {usersByMode['%'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Halfops</div>
            {usersByMode['%'].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                <span className="text-orange-500">%</span>
                <span className="ml-1">{user.nickname}</span>
              </div>
            ))}
          </div>
        )}

        {/* Voiced */}
        {usersByMode['+'].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Voiced</div>
            {usersByMode['+'].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                <span className="text-blue-500">+</span>
                <span className="ml-1">{user.nickname}</span>
              </div>
            ))}
          </div>
        )}

        {/* Regular Users */}
        {usersByMode[''].length > 0 && (
          <div className="mb-3">
            <div className="text-xs font-medium text-muted-foreground mb-1">Users</div>
            {usersByMode[''].map(user => (
              <div key={user.id} className="text-sm py-0.5">
                {user.nickname}
              </div>
            ))}
          </div>
        )}

        {users.length === 0 && (
          <div className="text-sm text-muted-foreground">No users found</div>
        )}
      </div>
    </div>
  );
}

