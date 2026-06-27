import { useNetworkStore } from '@/stores/network';

interface AuthBannerProps {
  networkId: number;
  onReconnect: (networkId: number) => void;
  onEditCredentials: (networkId: number) => void;
}

// AuthBanner renders a persistent warning strip when SASL/password authentication
// fails for a network. It disappears automatically once the network reconnects
// successfully (clearAuthFailed is called on the 'connected' connection-status
// event). The banner is purely presentational: reconnect and credential-editing
// actions are injected from App.tsx so the component stays decoupled from the
// Wails bindings.
export function AuthBanner({ networkId, onReconnect, onEditCredentials }: AuthBannerProps) {
  const auth = useNetworkStore((s) => s.authState[networkId]);
  if (!auth) return null;

  return (
    <div
      role="alert"
      className="flex items-center justify-between gap-3 px-4 py-2 text-sm border-t border-border/50"
      style={{ background: 'var(--presence-offline)', color: 'var(--foreground)' }}
    >
      <span className="flex items-center gap-1.5 min-w-0 truncate">
        <span aria-hidden="true">⚠</span>
        <span>
          Authentication failed{auth.reason ? `: ${auth.reason}` : ''}. You are not connected.
        </span>
      </span>
      <span className="flex gap-2 flex-shrink-0">
        <button
          type="button"
          onClick={() => onReconnect(networkId)}
          className="px-3 py-1 text-xs border border-border rounded-lg hover:bg-accent/60 transition-all cursor-pointer"
        >
          Reconnect
        </button>
        <button
          type="button"
          onClick={() => onEditCredentials(networkId)}
          className="px-3 py-1 text-xs bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-all font-medium cursor-pointer"
        >
          Edit credentials
        </button>
      </span>
    </div>
  );
}
