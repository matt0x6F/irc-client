import { useLayoutEffect, useEffect, useRef, useState } from 'react';
import { main, storage } from '../../wailsjs/go/models';
import {
  GetServers,
  ToggleNetworkAutoConnect,
  SetNetworkColor,
  SetNetworkIcon,
  RemoveNetworkIcon,
} from '../../wailsjs/go/main/App';
import { useUIStore } from '../stores/ui';
import { NETWORK_COLORS } from '../lib/network-color';

interface NetworkContextMenuProps {
  x: number;
  y: number;
  network: storage.Network;
  connected: boolean;
  connecting: boolean;
  onConnect: (config: main.NetworkConfig) => Promise<void>;
  onDisconnect: (id: number) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  onReloadNetworks: () => Promise<void>;
  onClose: () => void;
}

/** Read a File as raw base64 (no `data:...;base64,` prefix). The backend's
 *  SetNetworkIcon expects the raw payload, so strip the data-URL header. */
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(',')[1] ?? '');
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

/**
 * Right-click menu for a network tile on the rail. Owns its own color-grid and
 * delete-confirmation UI; App drives it with a single `{x, y, networkId}` open
 * state and closes it via `onClose`. Moved out of the retired network tree.
 */
export function NetworkContextMenu({
  x,
  y,
  network,
  connected,
  connecting,
  onConnect,
  onDisconnect,
  onDelete,
  onReloadNetworks,
  onClose,
}: NetworkContextMenuProps) {
  const [showColorGrid, setShowColorGrid] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const networkId = network.id;

  // Clamp the fixed-position menu into the viewport. It renders at the raw click
  // coordinates, so a right-click near the window's bottom would push its items
  // off-screen where they cannot be clicked. Re-runs when the color grid expands
  // the height.
  useLayoutEffect(() => {
    const el = menuRef.current;
    if (!el || confirmDelete) return;
    const margin = 8;
    const rect = el.getBoundingClientRect();
    const left = Math.min(x, window.innerWidth - rect.width - margin);
    const top = Math.min(y, window.innerHeight - rect.height - margin);
    el.style.left = `${Math.max(margin, left)}px`;
    el.style.top = `${Math.max(margin, top)}px`;
  }, [x, y, showColorGrid, confirmDelete]);

  // Close on outside click / Escape — but not while the delete dialog is up (it
  // owns its own overlay dismissal).
  useEffect(() => {
    if (confirmDelete) return;
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
  }, [confirmDelete, onClose]);

  const handleConnectClick = async () => {
    // Refuse if already connected or a connect is already in flight — a
    // duplicate attempt races the first and causes connect-then-drop churn.
    if (connected || connecting) {
      onClose();
      return;
    }
    onClose();
    try {
      // Load server addresses from the database; the backend resolves keychain
      // secrets on connect, so passwords stay empty here.
      const dbServers = await GetServers(networkId);
      const configData: any = {
        name: network.name,
        nickname: network.nickname,
        username: network.username,
        realname: network.realname,
        password: '',
        sasl_enabled: network.sasl_enabled || false,
        sasl_mechanism: network.sasl_mechanism || '',
        sasl_username: network.sasl_username || '',
        sasl_password: '',
        sasl_external_cert: network.sasl_external_cert || '',
      };
      if (dbServers && dbServers.length > 0) {
        configData.servers = dbServers.map((srv) => ({
          address: srv.address,
          port: srv.port,
          tls: srv.tls,
          order: srv.order,
        }));
      } else {
        configData.address = network.address;
        configData.port = network.port;
        configData.tls = network.tls;
      }
      await onConnect(main.NetworkConfig.createFrom(configData));
    } catch (error) {
      console.error('Failed to connect:', error);
      alert(`Failed to connect: ${error}`);
    }
  };

  const handleSetColor = async (key: string) => {
    try {
      await SetNetworkColor(networkId, key);
      await onReloadNetworks();
    } catch (error) {
      console.error('Failed to set network color:', error);
      alert(`Failed to set color: ${error}`);
    }
    onClose();
  };

  const handleIconFile = async (file: File | undefined) => {
    if (!file) return;
    try {
      const b64 = await fileToBase64(file);
      await SetNetworkIcon(networkId, b64);
      await onReloadNetworks();
    } catch (error) {
      console.error('Failed to set network icon:', error);
      alert(`Failed to set icon: ${error}`);
    }
    onClose();
  };

  const handleRemoveIcon = async () => {
    try {
      await RemoveNetworkIcon(networkId);
      await onReloadNetworks();
    } catch (error) {
      console.error('Failed to remove network icon:', error);
      alert(`Failed to remove icon: ${error}`);
    }
    onClose();
  };

  const itemClass =
    'w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground';

  if (confirmDelete) {
    return (
      <div
        className="fixed inset-0 bg-black/50 flex items-center justify-center z-50"
        onClick={onClose}
        style={{ backgroundColor: 'rgba(0, 0, 0, 0.5)' }}
      >
        <div
          className="bg-card border border-border rounded-lg shadow-[var(--shadow-xl)] p-6 max-w-md w-full mx-4"
          onClick={(e) => e.stopPropagation()}
          style={{ backgroundColor: 'var(--card)' }}
        >
          <h3 className="text-lg font-semibold mb-2 text-foreground">Delete Network</h3>
          <p className="text-muted-foreground mb-6">
            Are you sure you want to delete "{network.name}"? This will also delete all associated
            channels and messages.
          </p>
          <div className="flex gap-3 justify-end">
            <button
              className="px-4 py-2 text-sm border border-border cursor-pointer transition-all hover:bg-accent hover:border-l-4 hover:border-primary text-foreground"
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              data-testid="confirm-delete-network-button"
              className="px-4 py-2 text-sm bg-destructive text-destructive-foreground rounded hover:bg-destructive/90 font-medium"
              onClick={async () => {
                try {
                  await onDelete(networkId);
                } catch (error) {
                  console.error('Failed to delete:', error);
                  alert(`Failed to delete network: ${error}`);
                }
                onClose();
              }}
            >
              Delete
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={menuRef}
      data-testid="network-context-menu"
      className="fixed border border-border rounded-lg shadow-[var(--shadow-lg)] z-50 w-auto py-1 bg-card/95 backdrop-blur-md"
      style={{
        left: x,
        top: y,
        backgroundColor: 'var(--card)',
        minWidth: '160px',
        maxWidth: '220px',
        transition: 'var(--transition-base)',
      }}
    >
      <div className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase">Network</div>

      {connecting ? (
        <button
          data-testid="network-connecting-button"
          disabled
          className="w-full text-left px-4 py-2 text-sm text-muted-foreground cursor-not-allowed opacity-70"
          style={{ transition: 'var(--transition-base)' }}
        >
          Connecting…
        </button>
      ) : connected ? (
        <button
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={() => {
            void onDisconnect(networkId);
            onClose();
          }}
        >
          Disconnect
        </button>
      ) : (
        <button
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={handleConnectClick}
        >
          Connect
        </button>
      )}

      {connected && (
        <button
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={() => {
            useUIStore.getState().openChannelList(networkId);
            onClose();
          }}
        >
          Browse Channels
        </button>
      )}

      <div className="border-t border-border my-1" />

      <button
        data-testid="toggle-auto-connect-button"
        data-auto-connect={network.auto_connect ? 'true' : 'false'}
        className={itemClass}
        style={{ transition: 'var(--transition-base)' }}
        onClick={async () => {
          try {
            await ToggleNetworkAutoConnect(networkId);
            await onReloadNetworks();
          } catch (error) {
            console.error('Failed to toggle auto-connect:', error);
            alert(`Failed to toggle auto-connect: ${error}`);
          }
          onClose();
        }}
      >
        {network.auto_connect ? 'Disable Auto-Connect' : 'Enable Auto-Connect'}
      </button>

      <div className="border-t border-border my-1" />

      {/* Set color… — expands a swatch grid inline */}
      <button
        data-testid="set-color-button"
        className={itemClass}
        style={{ transition: 'var(--transition-base)' }}
        onClick={() => setShowColorGrid((s) => !s)}
      >
        Set color…
      </button>
      {showColorGrid && (
        <div className="px-3 py-2">
          <div className="grid grid-cols-5 gap-1.5">
            {NETWORK_COLORS.map((c) => (
              <button
                key={c.key}
                type="button"
                title={c.key}
                aria-label={c.key}
                data-testid="network-color-swatch"
                data-color={c.key}
                onClick={() => void handleSetColor(c.key)}
                className="rounded-md border border-border/60 hover:ring-2 hover:ring-primary"
                style={{ width: 24, height: 24, background: c.bg }}
              />
            ))}
          </div>
          <button
            type="button"
            data-testid="network-color-default"
            className="mt-2 w-full text-left text-xs text-muted-foreground hover:text-foreground"
            onClick={() => void handleSetColor('')}
          >
            Default
          </button>
        </div>
      )}

      {/* Set icon… — drives a hidden file input */}
      <button
        data-testid="set-icon-button"
        className={itemClass}
        style={{ transition: 'var(--transition-base)' }}
        onClick={() => fileInputRef.current?.click()}
      >
        Set icon…
      </button>
      {network.iconPath && (
        <button
          data-testid="remove-icon-button"
          className={itemClass}
          style={{ transition: 'var(--transition-base)' }}
          onClick={() => void handleRemoveIcon()}
        >
          Remove icon
        </button>
      )}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/png,image/jpeg,image/gif"
        className="hidden"
        onChange={(e) => void handleIconFile(e.target.files?.[0])}
      />

      <div className="border-t border-border my-1" />

      <button
        className="w-full text-left px-4 py-2 text-sm cursor-pointer transition-all hover:bg-destructive hover:text-destructive-foreground text-foreground"
        style={{ transition: 'var(--transition-base)' }}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setConfirmDelete(true);
        }}
      >
        Delete
      </button>
    </div>
  );
}
