import { useState, useEffect } from 'react';

interface ChannelListProps {
  serverId: number;
  selectedChannel: string | null;
  onSelectChannel: (channel: string | null) => void;
  isConnected: boolean;
}

export function ChannelList({ serverId, selectedChannel, onSelectChannel, isConnected }: ChannelListProps) {
  const [channels, setChannels] = useState<string[]>([]);
  const [newChannel, setNewChannel] = useState('');

  // For now, we'll use a simple list. In a full implementation,
  // this would fetch channels from the backend
  useEffect(() => {
    // Placeholder - would fetch from backend
    setChannels([]);
  }, [serverId]);

  const handleJoinChannel = () => {
    if (newChannel.trim() && !channels.includes(newChannel.trim())) {
      setChannels([...channels, newChannel.trim()]);
      setNewChannel('');
      onSelectChannel(newChannel.trim());
    }
  };

  return (
    <div className="h-full flex flex-col">
      <div className="p-4 border-b border-border">
        <h2 className="font-semibold mb-2">Channels</h2>
        <div className="flex space-x-2">
          <input
            type="text"
            placeholder="#channel"
            value={newChannel}
            onChange={(e) => setNewChannel(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && handleJoinChannel()}
            className="flex-1 px-2 py-1 text-sm border border-border rounded"
          />
          <button
            onClick={handleJoinChannel}
            className="px-3 py-1 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
          >
            Join
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {/* Status channel - always shown */}
        <div
          onClick={() => onSelectChannel('status')}
          className={`p-3 cursor-pointer hover:bg-accent flex items-center justify-between ${
            selectedChannel === 'status' ? 'bg-accent border-l-2 border-primary' : ''
          }`}
        >
          <span>Status</span>
          <span className={`w-2 h-2 rounded-full ${
            isConnected ? 'bg-green-500' : 'bg-gray-400'
          }`} title={isConnected ? 'Connected' : 'Disconnected'} />
        </div>
        
        {/* Regular channels */}
        {channels.map((channel) => (
          <div
            key={channel}
            onClick={() => onSelectChannel(channel)}
            className={`p-3 cursor-pointer hover:bg-accent ${
              selectedChannel === channel ? 'bg-accent border-l-2 border-primary' : ''
            }`}
          >
            {channel}
          </div>
        ))}
      </div>
    </div>
  );
}

