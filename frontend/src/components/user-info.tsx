import { useState, useEffect } from 'react';
import { SendCommand } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useNetworkStore } from '../stores/network';
import { casefold } from '../lib/casefold';
import { Modal } from './ui/modal';

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
  away: string;
  network: string;
  is_bot: boolean;
}

interface UserInfoProps {
  networkId: number | null;
  nickname: string;
  onClose: () => void;
}

export function UserInfo({ networkId, nickname, onClose }: UserInfoProps) {
  const [whoisInfo, setWhoisInfo] = useState<WhoisInfo | null>(null);
  const [loading, setLoading] = useState(true);

  // Live roster attributes (away-notify / account-notify / chghost). WHOIS gives
  // a point-in-time snapshot; this stays current as the user goes away/back or
  // logs in/out while the panel is open.
  const meta = useNetworkStore((s) =>
    networkId !== null
      ? s.userMeta[networkId]?.[casefold(s.caseMapping?.[networkId] ?? '', nickname)]
      : undefined
  );

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
      <Modal title="User Info" onClose={onClose} size="sm">
        <div data-testid="user-info-panel" className="text-sm text-muted-foreground">
          Loading user information...
        </div>
      </Modal>
    );
  }

  if (!whoisInfo) {
    return (
      <Modal title="User Info" onClose={onClose} size="sm">
        <div data-testid="user-info-panel" className="text-sm text-muted-foreground">
          No information available
        </div>
      </Modal>
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
    <Modal title="User Info" onClose={onClose} size="sm">
      <div data-testid="user-info-panel" className="min-w-0 space-y-4 text-sm">
        <div>
          <div className="font-semibold text-lg mb-2 flex items-center gap-2">
            {whoisInfo.nickname}
            {whoisInfo.is_bot && (
              <span
                className="text-[10px] uppercase font-semibold tracking-wide px-1.5 py-0.5 rounded bg-primary text-primary-foreground"
                title="This user is a bot (IRCv3 bot mode)"
              >
                bot
              </span>
            )}
            {(meta?.away || whoisInfo.away) && (
              <span
                className="text-[10px] uppercase font-semibold tracking-wide px-1.5 py-0.5 rounded bg-accent text-muted-foreground"
                title={
                  meta?.away_message || whoisInfo.away
                    ? `Away: ${meta?.away_message || whoisInfo.away}`
                    : 'Away'
                }
              >
                away
              </span>
            )}
          </div>
          {(whoisInfo.account_name || meta?.account) && (
            <div className="text-muted-foreground break-words">
              Account: <span className="text-foreground">{whoisInfo.account_name || meta?.account}</span>
            </div>
          )}
          {(meta?.away_message || whoisInfo.away) && (
            <div className="text-muted-foreground break-words">
              Away: <span className="text-foreground">{meta?.away_message || whoisInfo.away}</span>
            </div>
          )}
        </div>

        <div>
          <div className="font-semibold mb-1">Hostmask</div>
          <div className="text-muted-foreground font-mono text-xs break-all">
            {whoisInfo.username}@{whoisInfo.hostmask}
          </div>
        </div>

        {(meta?.realname || whoisInfo.real_name) && (
          <div>
            <div className="font-semibold mb-1">Real Name</div>
            <div className="text-muted-foreground break-words">{meta?.realname || whoisInfo.real_name}</div>
          </div>
        )}

        {whoisInfo.server && (
          <div>
            <div className="font-semibold mb-1">Server</div>
            <div className="text-muted-foreground break-words">{whoisInfo.server}</div>
            {whoisInfo.server_info && (
              <div className="text-muted-foreground text-xs mt-1 break-words">{whoisInfo.server_info}</div>
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
                  className="text-xs bg-accent px-2 py-1 rounded text-muted-foreground break-all"
                >
                  {channel}
                </span>
              ))}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

