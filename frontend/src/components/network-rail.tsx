import { useState } from 'react';
import { Bell, Plus, Settings } from 'lucide-react';
import { storage } from '../../wailsjs/go/models';
import { OpenSettings } from '../../wailsjs/go/main/App';
import { NetworkTile } from './network-tile';
import { RailTooltip } from './rail-tooltip';
import { networkUnreadTotal } from '../lib/network-unread';
import { unseenGroupCount } from '../lib/activity-inbox';

const SQUIRCLE = "path('M22 0 C38 0 44 6 44 22 C44 38 38 44 22 44 C6 44 0 38 0 22 C0 6 6 0 22 0 Z')";

interface NetworkRailProps {
  networks: storage.Network[];
  selectedNetwork: number | null;
  activityActive: boolean;
  connectionStatus: Record<number, boolean>;
  connectingNetworks: Record<number, boolean>;
  unreadCounts: Map<string, number>;
  activityItems: Parameters<typeof unseenGroupCount>[0];
  onSelectNetwork: (id: number) => void;
  onSelectActivity: () => void;
  onAddNetwork: () => void;
  onNetworkContextMenu: (e: React.MouseEvent, id: number) => void;
  onReordered: (orderedIds: number[]) => void;
}

export function NetworkRail(props: NetworkRailProps) {
  const {
    networks, selectedNetwork, activityActive, connectionStatus, connectingNetworks,
    unreadCounts, activityItems, onSelectNetwork, onSelectActivity, onAddNetwork,
    onNetworkContextMenu, onReordered,
  } = props;

  const [dragId, setDragId] = useState<number | null>(null);
  const unseen = unseenGroupCount(activityItems);

  const handleDrop = (targetId: number) => {
    if (dragId === null || dragId === targetId) return;
    const ids = networks.map((n) => n.id);
    const from = ids.indexOf(dragId);
    const to = ids.indexOf(targetId);
    if (from < 0 || to < 0) return;
    ids.splice(to, 0, ids.splice(from, 1)[0]);
    setDragId(null);
    onReordered(ids);
  };

  return (
    <div
      data-testid="network-rail"
      className="h-full flex flex-col items-center py-2.5 bg-card/30 border-r border-border"
      style={{ width: 64, flexShrink: 0 }}
    >
      <RailTooltip label="Activity">
        <button
          type="button"
          data-testid="rail-activity"
          data-active={activityActive ? 'true' : 'false'}
          aria-label="Activity"
          onClick={onSelectActivity}
          className="relative mb-2"
          style={{
            width: 44, height: 44, clipPath: SQUIRCLE,
            background: activityActive ? 'var(--primary)' : 'var(--accent)',
            color: activityActive ? 'var(--primary-foreground)' : 'var(--accent-foreground)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}
        >
          <Bell size={20} />
          {unseen > 0 && (
            <span className="absolute" style={{ right: -4, top: -4, minWidth: 18, height: 18, padding: '0 4px', borderRadius: 9, background: 'var(--primary)', color: 'var(--primary-foreground)', fontSize: 11, fontWeight: 500, display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 0 0 2px var(--background)' }}>
              {unseen > 99 ? '99+' : unseen}
            </span>
          )}
        </button>
      </RailTooltip>

      <div style={{ width: 32, height: 1, background: 'var(--border)', margin: '2px 0 8px' }} />

      {/* 4px top padding gives the first tile's badge (top: -4) headroom inside
          this overflow-clipped column; the divider margin above shrank to match. */}
      <div data-testid="rail-tiles" className="flex flex-col items-center gap-3 overflow-y-auto flex-1 w-full" style={{ scrollbarWidth: 'none', paddingTop: 4 }}>
        {networks.map((n) => (
          <RailTooltip key={n.id} label={n.name}>
            <div
              draggable
              onDragStart={() => setDragId(n.id)}
              onDragEnd={() => setDragId(null)}
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => { e.preventDefault(); handleDrop(n.id); }}
            >
              <NetworkTile
                network={n}
                selected={!activityActive && selectedNetwork === n.id}
                connected={connectionStatus[n.id] || false}
                connecting={connectingNetworks[n.id] || false}
                unread={networkUnreadTotal(unreadCounts, n.id)}
                onSelect={onSelectNetwork}
                onContextMenu={onNetworkContextMenu}
              />
            </div>
          </RailTooltip>
        ))}
      </div>

      <div className="flex flex-col items-center gap-2 pt-2">
        <RailTooltip label="Add network">
          <button
            type="button"
            data-testid="rail-add-network"
            aria-label="Add network"
            onClick={onAddNetwork}
            className="relative text-muted-foreground hover:text-foreground"
            style={{ width: 44, height: 44 }}
          >
            <svg width="44" height="44" viewBox="0 0 44 44" style={{ display: 'block' }}>
              <path d="M22 0 C38 0 44 6 44 22 C44 38 38 44 22 44 C6 44 0 38 0 22 C0 6 6 0 22 0 Z" fill="var(--card)" stroke="var(--border)" strokeWidth="1.5" strokeDasharray="4 3" />
            </svg>
            <Plus size={20} style={{ position: 'absolute', inset: 0, margin: 'auto' }} />
          </button>
        </RailTooltip>
        <RailTooltip label="Settings">
          <button
            type="button"
            data-testid="rail-settings"
            aria-label="Settings"
            onClick={() => void OpenSettings()}
            className="text-muted-foreground hover:text-foreground"
            style={{ width: 44, height: 44, clipPath: SQUIRCLE, background: 'var(--card)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          >
            <Settings size={20} />
          </button>
        </RailTooltip>
      </div>
    </div>
  );
}
