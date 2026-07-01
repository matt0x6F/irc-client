import { useEffect, useMemo, useRef, useState } from 'react';
import { GetJoinedChannels, SendCommand } from '../../wailsjs/go/main/App';
import { storage } from '../../wailsjs/go/models';
import { Modal } from './ui/modal';

interface InviteToChannelModalProps {
  networkId: number;
  nick: string;
  currentChannel: string | null;
  onClose: () => void;
}

export function InviteToChannelModal({ networkId, nick, currentChannel, onClose }: InviteToChannelModalProps) {
  const [channels, setChannels] = useState<storage.Channel[]>([]);
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let cancelled = false;
    void GetJoinedChannels(networkId)
      .then((chs) => {
        if (!cancelled) setChannels(chs.filter((c) => c.name !== currentChannel));
      })
      .catch(() => {
        if (!cancelled) setChannels([]);
      });
    return () => {
      cancelled = true;
    };
  }, [networkId, currentChannel]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? channels.filter((c) => c.name.toLowerCase().includes(q)) : channels;
  }, [channels, query]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  const invite = (channel: string) => {
    void SendCommand(networkId, `/invite ${nick} ${channel}`);
    onClose();
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, filtered.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const ch = filtered[activeIndex];
      if (ch) invite(ch.name);
    }
  };

  return (
    <Modal title={`Invite ${nick} to…`} onClose={onClose} size="sm">
      <input
        ref={inputRef}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder="Search channels…"
        className="w-full mb-2 rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
      />
      {filtered.length === 0 ? (
        <div className="px-1 py-2 text-sm text-muted-foreground">
          {channels.length === 0
            ? "You're not in any other channels to invite to."
            : 'No channels match.'}
        </div>
      ) : (
        <ul className="max-h-72 overflow-y-auto">
          {filtered.map((ch, i) => (
            <li key={ch.name}>
              <button
                onClick={() => invite(ch.name)}
                onMouseEnter={() => setActiveIndex(i)}
                className={`w-full min-w-0 truncate rounded-md px-3 py-2 text-left text-sm ${
                  i === activeIndex ? 'bg-accent' : 'hover:bg-accent'
                }`}
              >
                {ch.name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Modal>
  );
}
