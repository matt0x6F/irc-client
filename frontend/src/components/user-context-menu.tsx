import { useEffect, useRef, useState } from 'react';
import { main, storage } from '../../wailsjs/go/models';
import { GetChannelInfo } from '../../wailsjs/go/main/App';
import { useNetworkStore } from '../stores/network';
import { useUIStore } from '../stores/ui';

interface UserContextMenuProps {
  networkId: number;
  // The pane the menu was opened from. Only real IRC channels (# / &) carry the
  // membership + capabilities needed for moderation; PMs, the status/invites panes,
  // and a null pane fall back to the always-available Whois/Monitor/CTCP entries.
  channelName: string | null;
  targetNick: string;
  currentNickname: string | null;
  x: number;
  y: number;
  onClose: () => void;
  onSendCommand: (command: string) => Promise<void> | void;
  // Whois has a different home per call site (the sidebar's own panel vs. the
  // app-level overlay), so the host decides where it renders.
  onShowUserInfo: (nick: string) => void;
}

function isRealChannel(name: string | null): name is string {
  return !!name && name !== 'status' && name !== 'invites' && !name.startsWith('pm:');
}

/**
 * The nickname context menu shared by the channel user list and the message
 * buffer. It is self-contained: given a network, the current pane, and a target
 * nick, it fetches a fresh permission snapshot (self's channel modes + server
 * capabilities) and the joined-channel list so the moderation, invite, and
 * private-message sections match wherever it is opened.
 */
export function UserContextMenu({
  networkId,
  channelName,
  targetNick,
  currentNickname,
  x,
  y,
  onClose,
  onSendCommand,
  onShowUserInfo,
}: UserContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null);
  const addMonitorNick = useNetworkStore((s) => s.addMonitorNick);
  const selectPane = useNetworkStore((s) => s.selectPane);
  const setChannelContext = useNetworkStore((s) => s.setChannelContext);

  const [channelInfo, setChannelInfo] = useState<main.ChannelInfo | null>(null);

  // Snapshot membership + capabilities (for permission gating) when the menu
  // opens. A one-shot fetch keeps this component usable from anywhere without
  // the caller pre-loading state.
  useEffect(() => {
    if (!isRealChannel(channelName)) {
      setChannelInfo(null);
      return;
    }
    let cancelled = false;
    void GetChannelInfo(networkId, channelName)
      .then((info) => {
        if (!cancelled) setChannelInfo(info);
      })
      .catch(() => {
        if (!cancelled) setChannelInfo(null);
      });
    return () => {
      cancelled = true;
    };
  }, [networkId, channelName]);

  // Dismiss on outside click or Escape.
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose();
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [onClose]);

  const users = (channelInfo?.users || []) as storage.ChannelUser[];
  const capabilities = channelInfo?.capabilities;
  const prefixMap = capabilities?.prefix || {};
  const currentUser = currentNickname
    ? users.find((u) => u.nickname.toLowerCase() === currentNickname.toLowerCase())
    : null;
  const isSelf = !!currentNickname && targetNick.toLowerCase() === currentNickname.toLowerCase();

  const userHasMode = (user: storage.ChannelUser, modeLetter: string): boolean => {
    if (!user.modes) return false;
    for (const prefixChar of user.modes) {
      if ((prefixMap[prefixChar] || null) === modeLetter) return true;
    }
    return false;
  };

  const getCurrentUserHighestMode = (): string | null => {
    if (!currentUser || !currentUser.modes) return null;
    for (const mode of ['q', 'a', 'o', 'h', 'v']) {
      if (userHasMode(currentUser, mode)) return mode;
    }
    return null;
  };

  // Permission checks using server capabilities. Fallback: when capabilities are
  // unknown, check for the '@' (op) / '%' (halfop) prefix directly.
  const canKick = (): boolean => {
    if (!currentUser) return false;
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || false;
    }
    const highest = getCurrentUserHighestMode();
    return highest === 'q' || highest === 'a' || highest === 'o';
  };
  const canBan = canKick;
  const canOp = canKick;
  const canVoice = (): boolean => {
    if (!currentUser) return false;
    if (!capabilities || !capabilities.prefix_string) {
      return currentUser.modes?.includes('@') || currentUser.modes?.includes('%') || false;
    }
    const highest = getCurrentUserHighestMode();
    return highest === 'q' || highest === 'a' || highest === 'o' || highest === 'h';
  };

  const send = (command: string) => {
    void Promise.resolve(onSendCommand(command));
    onClose();
  };
  const sendMany = async (...commands: string[]) => {
    for (const command of commands) {
      await onSendCommand(command);
    }
    onClose();
  };

  const itemClass =
    'w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground ';

  return (
    <div
      ref={menuRef}
      className="fixed z-50 bg-card border border-border rounded-lg shadow-[var(--shadow-lg)] min-w-[180px] backdrop-blur-md"
      style={{
        left: `${x}px`,
        top: `${y}px`,
        backgroundColor: 'var(--card)',
        transition: 'var(--transition-base)',
      }}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="py-1">
        {/* Moderation is only meaningful against another user in a real channel. */}
        {!isSelf && (
          <>
            {canKick() && (
              <>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => void sendMany(`/kick ${channelName} ${targetNick}`)}>
                  Kick
                </button>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => void sendMany(`/kick ${channelName} ${targetNick}`, `/ban ${channelName} ${targetNick}!*@*`)}>
                  Kick & Ban
                </button>
                <div className="border-t border-border my-1" />
              </>
            )}

            {canBan() && (
              <>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/ban ${channelName} ${targetNick}!*@*`)}>
                  Ban
                </button>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/unban ${channelName} ${targetNick}!*@*`)}>
                  Unban
                </button>
                <div className="border-t border-border my-1" />
              </>
            )}

            {canOp() && (
              <>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/op ${channelName} ${targetNick}`)}>
                  Op
                </button>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/deop ${channelName} ${targetNick}`)}>
                  Deop
                </button>
              </>
            )}

            {canVoice() && (
              <>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/voice ${channelName} ${targetNick}`)}>
                  Voice
                </button>
                <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/devoice ${channelName} ${targetNick}`)}>
                  Devoice
                </button>
              </>
            )}

            {/* Invite to another channel — opens the searchable picker modal */}
            {isRealChannel(channelName) && (
              <>
                {(canOp() || canVoice()) && <div className="border-t border-border my-1" />}
                <button
                  className={itemClass}
                  style={{ transition: 'var(--transition-base)' }}
                  onClick={() => {
                    useUIStore.getState().setInviteTo({ networkId, nick: targetNick, channel: channelName });
                    onClose();
                  }}
                >
                  Invite to channel…
                </button>
              </>
            )}
          </>
        )}

        {isSelf && <div className="px-4 py-2 text-sm text-muted-foreground">Cannot operate on yourself</div>}

        {/* User info & CTCP — available for all users */}
        <div className="border-t border-border my-1" />
        <button
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={() => {
            onShowUserInfo(targetNick);
            onClose();
          }}
        >
          Whois
        </button>
        <button
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={() => {
            void addMonitorNick(networkId, targetNick);
            onClose();
          }}
        >
          Monitor this user
        </button>
        {isRealChannel(channelName) && !isSelf && (
          <button
            className={itemClass}
            style={{ transition: 'var(--transition-base)' }}
            onClick={() => {
              const pane = `pm:${targetNick}`;
              void selectPane(networkId, pane).then(() => setChannelContext(pane, channelName));
              onClose();
            }}
          >
            Message privately (re: {channelName})
          </button>
        )}
        <div className="border-t border-border my-1" />
        <div className="px-4 py-1 text-xs font-semibold text-muted-foreground uppercase">CTCP</div>
        <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/version ${targetNick}`)}>
          CTCP Version
        </button>
        <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/time ${targetNick}`)}>
          CTCP Time
        </button>
        <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/ping ${targetNick}`)}>
          CTCP Ping
        </button>
        <button className={itemClass} style={{ transition: 'var(--transition-base)' }} onClick={() => send(`/clientinfo ${targetNick}`)}>
          CTCP ClientInfo
        </button>
      </div>
    </div>
  );
}
