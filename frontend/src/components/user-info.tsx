import { useState, useEffect } from 'react';
import { SendCommand } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

interface WhoisInfo {
  nickname: string;
  username: string;
  hostmask: string;
  real_name: string;
  server: string;
  server_info: string;
  channels: string[];
  idle_time: number;
  sign_on_time: number;
  account_name: string;
  network: string;
}

interface UserInfoProps {
  networkId: number | null;
  nickname: string;
  onClose: () => void;
}

export function UserInfo({ networkId, nickname, onClose }: UserInfoProps) {
  const [whoisInfo, setWhoisInfo] = useState<WhoisInfo | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (networkId === null) return;

    // Send WHOIS command
    const sendWhois = async () => {
      try {
        await SendCommand(networkId, `/whois ${nickname}`);
        setLoading(true);
      } catch (error) {
        console.error('Failed to send WHOIS command:', error);
        setLoading(false);
      }
    };

    sendWhois();

    // Listen for WHOIS response
    const unsubscribe = EventsOn('whois-event', (data: any) => {
      const eventData = data?.data || {};
      const whois = eventData.whois as WhoisInfo;
      if (whois && whois.nickname.toLowerCase() === nickname.toLowerCase()) {
        setWhoisInfo(whois);
        setLoading(false);
      }
    });

    // Timeout after 5 seconds
    const timeout = setTimeout(() => {
      setLoading(false);
    }, 5000);

    return () => {
      unsubscribe();
      clearTimeout(timeout);
    };
  }, [networkId, nickname]);

  if (loading && !whoisInfo) {
    return (
      <div className="w-80 border-l border-border p-4">
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-semibold">User Info</h3>
          <button
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
          >
            ×
          </button>
        </div>
        <div className="text-sm text-muted-foreground">Loading user information...</div>
      </div>
    );
  }

  if (!whoisInfo) {
    return (
      <div className="w-80 border-l border-border p-4">
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-semibold">User Info</h3>
          <button
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground"
          >
            ×
          </button>
        </div>
        <div className="text-sm text-muted-foreground">No information available</div>
      </div>
    );
  }

  const formatIdleTime = (seconds: number): string => {
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
    return `${Math.floor(seconds / 86400)}d`;
  };

  const formatSignOnTime = (timestamp: number): string => {
    if (timestamp === 0) return 'Unknown';
    const date = new Date(timestamp * 1000);
    return date.toLocaleString();
  };

  return (
    <div className="w-80 border-l border-border p-4 overflow-y-auto">
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-semibold">User Info</h3>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
        >
          ×
        </button>
      </div>

      <div className="space-y-4 text-sm">
        <div>
          <div className="font-semibold text-lg mb-2">{whoisInfo.nickname}</div>
          {whoisInfo.account_name && (
            <div className="text-muted-foreground">
              Account: <span className="text-foreground">{whoisInfo.account_name}</span>
            </div>
          )}
        </div>

        <div>
          <div className="font-semibold mb-1">Hostmask</div>
          <div className="text-muted-foreground font-mono text-xs">
            {whoisInfo.username}@{whoisInfo.hostmask}
          </div>
        </div>

        {whoisInfo.real_name && (
          <div>
            <div className="font-semibold mb-1">Real Name</div>
            <div className="text-muted-foreground">{whoisInfo.real_name}</div>
          </div>
        )}

        {whoisInfo.server && (
          <div>
            <div className="font-semibold mb-1">Server</div>
            <div className="text-muted-foreground">{whoisInfo.server}</div>
            {whoisInfo.server_info && (
              <div className="text-muted-foreground text-xs mt-1">{whoisInfo.server_info}</div>
            )}
          </div>
        )}

        {whoisInfo.idle_time > 0 && (
          <div>
            <div className="font-semibold mb-1">Idle Time</div>
            <div className="text-muted-foreground">{formatIdleTime(whoisInfo.idle_time)}</div>
          </div>
        )}

        {whoisInfo.sign_on_time > 0 && (
          <div>
            <div className="font-semibold mb-1">Sign-On Time</div>
            <div className="text-muted-foreground">{formatSignOnTime(whoisInfo.sign_on_time)}</div>
          </div>
        )}

        {whoisInfo.channels && whoisInfo.channels.length > 0 && (
          <div>
            <div className="font-semibold mb-1">Channels ({whoisInfo.channels.length})</div>
            <div className="flex flex-wrap gap-1">
              {whoisInfo.channels.map((channel, idx) => (
                <span
                  key={idx}
                  className="text-xs bg-accent px-2 py-1 rounded text-muted-foreground"
                >
                  {channel}
                </span>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

