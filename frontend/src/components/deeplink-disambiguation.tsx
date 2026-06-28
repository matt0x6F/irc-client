import { useNetworkStore } from '../stores/network';
import { useUIStore } from '../stores/ui';

interface Target { name: string; isNick: boolean; key: string }

// DeepLinkDisambiguation prompts the user to pick which saved network a deep
// link should act on when more than one matches the host. Renders nothing when
// there is no pending disambiguation.
export function DeepLinkDisambiguation() {
  const openOrJoinChannel = useNetworkStore((s) => s.openOrJoinChannel);
  const openQuery = useNetworkStore((s) => s.openQuery);
  const pending = useUIStore((s) => s.deepLinkDisambiguation);
  const clear = useUIStore((s) => s.setDeepLinkDisambiguation);

  if (!pending) return null;

  const choose = (networkId: number) => {
    for (const t of pending.targets as Target[]) {
      if (t.isNick) void openQuery(networkId, t.name);
      else void openOrJoinChannel(networkId, t.name);
    }
    clear(null);
  };

  return (
    <div
      className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={(e) => {
        if (e.target === e.currentTarget) clear(null);
      }}
    >
      <div className="bg-card border border-border rounded-xl shadow-2xl w-full max-w-sm mx-4">
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <h2 className="text-lg font-semibold">Open link on which network?</h2>
          <button
            type="button"
            onClick={() => clear(null)}
            className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            aria-label="Close"
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M18 6 6 18" />
              <path d="m6 6 12 12" />
            </svg>
          </button>
        </div>
        <div className="px-6 py-4">
          <ul className="space-y-2">
            {pending.candidates.map((c) => (
              <li key={c.networkId}>
                <button
                  type="button"
                  onClick={() => void choose(c.networkId)}
                  className="w-full text-left px-3 py-2 rounded-md hover:bg-accent/50 text-sm font-medium transition-colors cursor-pointer"
                >
                  {c.name}
                </button>
              </li>
            ))}
          </ul>
        </div>
        <div className="flex justify-end px-6 pb-4">
          <button
            type="button"
            onClick={() => clear(null)}
            className="px-4 py-2 text-sm text-muted-foreground hover:text-foreground rounded-md hover:bg-accent/50 transition-colors cursor-pointer"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}
