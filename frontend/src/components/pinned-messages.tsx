import { useMemo } from 'react';
import { X } from 'lucide-react';
import { useNetworkStore } from '../stores/network';
import { useNicknameColors } from '../hooks/useNicknameColors';

interface PinnedMessagesProps {
  networkId: number | null;
}

export function PinnedMessages({ networkId }: PinnedMessagesProps) {
  const pinnedMessages = useNetworkStore((s) => s.pinnedMessages);
  const jumpToMessage = useNetworkStore((s) => s.jumpToMessage);
  const unpinMessage = useNetworkStore((s) => s.unpinMessage);

  const nicknames = useMemo(
    () => pinnedMessages.map((p) => p.user).filter((u) => u && u !== '*'),
    [pinnedMessages]
  );
  const nicknameColors = useNicknameColors(networkId, nicknames);

  if (pinnedMessages.length === 0) {
    return (
      <div className="p-4 text-sm text-muted-foreground text-center">
        No pinned messages
      </div>
    );
  }

  return (
    <div className="p-2 space-y-1" data-testid="pinned-messages-list">
      {pinnedMessages.map((pin) => (
        <div
          key={pin.id}
          className="group flex items-start gap-2 rounded-md px-2 py-1.5 hover:bg-accent/50 cursor-pointer transition-colors"
          onClick={() => jumpToMessage(pin.id)}
          title="Jump to message"
          data-testid="pinned-message-item"
        >
          <div className="flex-1 min-w-0">
            <div className="flex items-baseline gap-2">
              <span
                className="text-sm font-medium truncate"
                style={{ color: nicknameColors.get(pin.user) || undefined }}
              >
                {pin.user}
              </span>
              <span className="text-[10px] text-muted-foreground/60 font-mono flex-shrink-0">
                {new Date(pin.timestamp).toLocaleString()}
              </span>
            </div>
            <div className="text-sm text-muted-foreground truncate">{pin.message}</div>
          </div>
          <button
            onClick={(e) => {
              e.stopPropagation();
              unpinMessage(pin.id);
            }}
            className="opacity-0 group-hover:opacity-100 focus:opacity-100 flex-shrink-0 p-0.5 rounded text-muted-foreground hover:text-foreground transition-opacity cursor-pointer"
            title="Unpin message"
            aria-label="Unpin message"
          >
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  );
}
