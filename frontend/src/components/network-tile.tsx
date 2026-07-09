import { useEffect, useState } from 'react';
import { storage } from '../../wailsjs/go/models';
import { GetNetworkIcon } from '../../wailsjs/go/main/App';
import { networkMonogram } from '../lib/network-monogram';
import { resolveNetworkColor } from '../lib/network-color';

const SQUIRCLE = "path('M22 0 C38 0 44 6 44 22 C44 38 38 44 22 44 C6 44 0 38 0 22 C0 6 6 0 22 0 Z')";

interface NetworkTileProps {
  network: storage.Network;
  selected: boolean;
  connected: boolean;
  connecting: boolean;
  unread: number;
  onSelect: (id: number) => void;
  onContextMenu: (e: React.MouseEvent, id: number) => void;
}

export function NetworkTile({
  network, selected, connected, connecting, unread, onSelect, onContextMenu,
}: NetworkTileProps) {
  const [iconUrl, setIconUrl] = useState<string>('');
  useEffect(() => {
    let live = true;
    // iconPath present => fetch the processed data URL; else clear.
    if (network.iconPath) {
      setIconUrl(''); // clear any prior network's icon before fetching this one
      GetNetworkIcon(network.id)
        .then((url) => { if (live) setIconUrl(url || ''); })
        .catch(() => { if (live) setIconUrl(''); });
    } else {
      setIconUrl('');
    }
    return () => { live = false; };
    // network.updated_at is bumped by UpdateNetworkIcon even when a replaced
    // icon keeps the same iconPath string, so it must be in the deps to
    // force a re-fetch on replace (otherwise the tile keeps showing the old
    // image until an unrelated remount).
  }, [network.id, network.iconPath, network.updated_at]);

  const { bg, fg } = resolveNetworkColor(network.color, network.name);

  return (
    <div className="relative flex items-center" style={{ width: 44 }}>
      {selected && (
        <span className="absolute" style={{ left: -10, top: '50%', transform: 'translateY(-50%)', width: 4, height: 32, borderRadius: '0 4px 4px 0', background: 'var(--foreground)' }} />
      )}
      <button
        type="button"
        data-testid="network-tile"
        data-network-id={network.id}
        aria-label={network.name}
        onClick={() => onSelect(network.id)}
        onContextMenu={(e) => onContextMenu(e, network.id)}
        className={`relative select-none ${connecting ? 'animate-pulse' : ''}`}
        style={{
          width: 44, height: 44, clipPath: SQUIRCLE,
          background: iconUrl ? undefined : bg,
          color: fg,
          boxShadow: selected ? '0 0 0 2px var(--background), 0 0 0 4px var(--foreground)' : undefined,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 15, fontWeight: 500,
        }}
      >
        {iconUrl
          ? <img src={iconUrl} alt="" style={{ width: 44, height: 44, objectFit: 'cover' }} draggable={false} />
          : networkMonogram(network.name)}
      </button>
      <span
        className="absolute"
        title={connected ? 'Connected' : 'Disconnected'}
        data-testid="network-status-indicator"
        data-connected={connected ? 'true' : 'false'}
        style={{ right: 0, bottom: 0, width: 12, height: 12, borderRadius: '50%', border: '2.5px solid var(--background)', background: connected ? 'var(--presence-online)' : 'var(--presence-offline)' }}
      />
      {unread > 0 && (
        <span
          className="absolute"
          title="Unread mentions"
          style={{ right: -4, top: -4, minWidth: 18, height: 18, padding: '0 4px', borderRadius: 9, background: 'var(--primary)', color: 'var(--primary-foreground)', fontSize: 11, fontWeight: 500, display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 0 0 2px var(--background)' }}
        >
          {unread > 99 ? '99+' : unread}
        </span>
      )}
    </div>
  );
}
