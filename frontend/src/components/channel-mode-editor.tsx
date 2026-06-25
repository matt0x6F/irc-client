import { useState, useEffect, useRef } from 'react';
import { SendCommand, GetNetworks, RequestChannelBans } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { main, storage } from '../../wailsjs/go/models';
import { describeBan } from '../lib/extban';

interface ChannelModeEditorProps {
  networkId: number;
  channelName: string;
  currentModes: string;
  capabilities?: main.ServerCapabilitiesInfo;
  onClose: () => void;
  onUpdate: () => void;
}

// Human-readable labels for common channel modes. Unknown letters fall back to "+<letter>".
const MODE_LABELS: Record<string, string> = {
  i: 'Invite only',
  m: 'Moderated',
  n: 'No external messages',
  p: 'Private',
  s: 'Secret',
  t: 'Topic locked to ops',
  k: 'Key (password)',
  l: 'User limit',
  C: 'Block CTCP',
  R: 'Registered users only',
  M: 'Must be registered to speak',
};

function modeLabel(letter: string): string {
  return MODE_LABELS[letter] || `Mode +${letter}`;
}

interface ModeFormState {
  flags: Set<string>;
  params: Record<string, string>;
}

// parseModes turns a canonical mode string ("+knt secret" or "+nt") into structured
// form state. paramLetters tells it which letters carry a trailing argument so the
// argument cursor stays aligned.
function parseModes(modeStr: string, paramLetters: Set<string>): ModeFormState {
  const flags = new Set<string>();
  const params: Record<string, string> = {};
  const trimmed = (modeStr || '').trim();
  if (!trimmed) return { flags, params };

  const parts = trimmed.split(/\s+/);
  let letters = parts[0];
  const args = parts.slice(1);
  if (letters.startsWith('+') || letters.startsWith('-')) {
    letters = letters.slice(1);
  }
  let ai = 0;
  for (const ch of letters) {
    if (paramLetters.has(ch)) {
      params[ch] = args[ai++] ?? '';
    } else {
      flags.add(ch);
    }
  }
  return { flags, params };
}

/**
 * computeModeCommands produces the minimal set of MODE command specs needed to turn
 * the channel's `initial` mode state into the `desired` state, plus the staged ban
 * additions and removals. Each returned string is the portion AFTER "MODE #channel ",
 * e.g. "+mt-i", "+k secret", "-l", "+b nick!*@*". The caller sends one IRC command per
 * returned spec, and short-circuits when the array is empty.
 *
 * Boolean flag toggles are batched into a single spec to stay friendly to the server's
 * rate limiter. Parameterized modes (key, limit) are emitted individually because each
 * carries an argument; clearing one uses "-<letter>" (keeping the old key as the
 * argument, which most servers require for "-k"). Ban deltas are one command per mask.
 */
function computeModeCommands(
  initial: ModeFormState,
  desired: ModeFormState,
  bansToAdd: string[],
  bansToRemove: string[],
): string[] {
  const specs: string[] = [];

  // Boolean (type D) flags: collect every toggle into one "+ab-cd" spec.
  const added: string[] = [];
  const removed: string[] = [];
  for (const f of desired.flags) {
    if (!initial.flags.has(f)) added.push(f);
  }
  for (const f of initial.flags) {
    if (!desired.flags.has(f)) removed.push(f);
  }
  let flagSpec = '';
  if (added.length) flagSpec += '+' + added.join('');
  if (removed.length) flagSpec += '-' + removed.join('');
  if (flagSpec) specs.push(flagSpec);

  // Parameterized (type B/C) modes: one command each, since they carry an argument.
  const paramLetters = new Set([...Object.keys(initial.params), ...Object.keys(desired.params)]);
  for (const letter of paramLetters) {
    const before = (initial.params[letter] || '').trim();
    const after = (desired.params[letter] || '').trim();
    if (before === after) continue;
    if (after) {
      specs.push(`+${letter} ${after}`);
    } else {
      // Cleared: "-k" conventionally still takes the old key as its argument.
      specs.push(before ? `-${letter} ${before}` : `-${letter}`);
    }
  }

  // Ban-list (type A) deltas: remove first, then add.
  for (const mask of bansToRemove) {
    specs.push(`-b ${mask}`);
  }
  for (const mask of bansToAdd) {
    specs.push(`+b ${mask}`);
  }

  return specs;
}

interface BanRow {
  mask: string;
  by?: string;
  time?: number;
}

// Tracks channels whose ban-list query is currently in flight, so we never send a
// duplicate MODE +b while one is already pending — e.g. React StrictMode's double-
// invoke of effects in dev, or a rapid editor reopen. The result event is broadcast,
// so a later listener still receives it even when its own send was skipped.
const banFetchInFlight = new Set<string>();

export function ChannelModeEditor({
  networkId,
  channelName,
  currentModes,
  capabilities,
  onClose,
  onUpdate,
}: ChannelModeEditorProps) {
  // Which letters belong to which CHANMODES class (with sensible fallbacks).
  const flagLetters = (capabilities?.chanmodes_d || 'imnpst').split('');
  const paramB = (capabilities?.chanmodes_b || 'k').split('');
  const paramC = (capabilities?.chanmodes_c || 'l').split('');
  const paramLetters = new Set([...paramB, ...paramC]);
  const supportsBans = (capabilities?.chanmodes_a || 'b').includes('b');
  // Ratified account-extban: the server advertised an EXTBAN prefix (e.g. "$")
  // and the account type 'a', so "$a" / "$a:account" ban masks are meaningful.
  const extbanPrefix = capabilities?.extban_prefix || '';
  const supportsAccountExtban = extbanPrefix !== '' && (capabilities?.extban_types || '').includes('a');

  const initialRef = useRef<ModeFormState>(parseModes(currentModes, paramLetters));
  const [flags, setFlags] = useState<Set<string>>(new Set(initialRef.current.flags));
  const [params, setParams] = useState<Record<string, string>>({ ...initialRef.current.params });

  const [bans, setBans] = useState<BanRow[]>([]);
  const [bansToAdd, setBansToAdd] = useState<string[]>([]);
  const [bansToRemove, setBansToRemove] = useState<string[]>([]);
  const [newBan, setNewBan] = useState('');
  const [bansLoading, setBansLoading] = useState(supportsBans);

  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const networksRef = useRef<storage.Network[]>([]);

  // Re-parse when the channel's modes change underneath us (e.g. a live MODE arrived).
  useEffect(() => {
    const parsed = parseModes(currentModes, paramLetters);
    initialRef.current = parsed;
    setFlags(new Set(parsed.flags));
    setParams({ ...parsed.params });
    GetNetworks().then((nets) => {
      networksRef.current = nets || [];
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentModes]);

  // Fetch the ban list on open and listen for the asynchronous result. Always attach
  // the listener, but only send the query when one isn't already in flight for this
  // channel — so we don't spam the server with duplicate MODE +b queries.
  useEffect(() => {
    if (!supportsBans) return;
    const key = `${networkId}:${channelName.toLowerCase()}`;
    setBansLoading(true);

    if (!banFetchInFlight.has(key)) {
      banFetchInFlight.add(key);
      RequestChannelBans(networkId, channelName).catch((e) => {
        console.error('Failed to request ban list:', e);
        banFetchInFlight.delete(key);
        setBansLoading(false);
      });
    }

    const unsubscribe = EventsOn('message-event', (data: any) => {
      if (data?.type !== 'channel.banlist') return;
      const d = data?.data || {};
      if (Number(d.networkId) !== Number(networkId)) return;
      if ((d.channel || '').toLowerCase() !== channelName.toLowerCase()) return;
      banFetchInFlight.delete(key);
      const incoming: BanRow[] = (d.bans || []).map((b: any) => ({
        mask: b.mask,
        by: b.by,
        time: b.time,
      }));
      setBans(incoming);
      setBansLoading(false);
    });

    // Safety net: clear the in-flight flag and stop spinning if the server never answers.
    const timeout = window.setTimeout(() => {
      banFetchInFlight.delete(key);
      setBansLoading(false);
    }, 8000);

    return () => {
      window.clearTimeout(timeout);
      unsubscribe();
    };
  }, [networkId, channelName, supportsBans]);

  const toggleFlag = (letter: string) => {
    setFlags((prev) => {
      const next = new Set(prev);
      if (next.has(letter)) next.delete(letter);
      else next.add(letter);
      return next;
    });
  };

  const setParam = (letter: string, value: string) => {
    setParams((prev) => ({ ...prev, [letter]: value }));
  };

  const addBan = () => {
    const mask = newBan.trim();
    if (!mask) return;
    setBans((prev) => [...prev, { mask }]);
    setBansToAdd((prev) => [...prev, mask]);
    setNewBan('');
  };

  const removeBan = (mask: string) => {
    setBans((prev) => prev.filter((b) => b.mask !== mask));
    // If it was only just staged for addition, cancel that; otherwise stage a removal.
    setBansToAdd((prev) => {
      if (prev.includes(mask)) return prev.filter((m) => m !== mask);
      setBansToRemove((r) => [...r, mask]);
      return prev;
    });
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);

    const specs = computeModeCommands(
      initialRef.current,
      { flags, params },
      bansToAdd,
      bansToRemove,
    );

    if (specs.length === 0) {
      // Nothing to do.
      onUpdate();
      onClose();
      return;
    }

    let resolved = false;
    let timeoutId: number;
    const cleanup = () => {
      if (timeoutId) clearTimeout(timeoutId);
      errorUnsub();
      modeUnsub();
    };

    const currentNetwork = () => networksRef.current.find((n) => n.id === networkId);

    const errorUnsub = EventsOn('message-event', (data: any) => {
      if (resolved || data?.type !== 'error') return;
      const d = data?.data || {};
      const net = currentNetwork();
      if (net && d.network === net.address && (!d.channel || d.channel === channelName)) {
        resolved = true;
        setError(d.error || 'The server rejected the mode change');
        setSaving(false);
        cleanup();
      }
    });

    const modeUnsub = EventsOn('message-event', (data: any) => {
      if (resolved || data?.type !== 'channel.mode') return;
      const d = data?.data || {};
      const net = currentNetwork();
      if (net && d.network === net.address && d.channel === channelName) {
        resolved = true;
        onUpdate();
        onClose();
        cleanup();
      }
    });

    try {
      for (const spec of specs) {
        await SendCommand(networkId, `/MODE ${channelName} ${spec}`);
      }
      timeoutId = setTimeout(() => {
        if (!resolved) {
          resolved = true;
          onUpdate();
          onClose();
          cleanup();
        }
      }, 2000);
    } catch (err) {
      console.error('Failed to set modes:', err);
      setError(`Failed to set modes: ${err}`);
      setSaving(false);
      cleanup();
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={onClose}>
      <div
        data-testid="channel-mode-editor"
        className="border border-border rounded-lg w-full max-w-lg p-6 max-h-[85vh] overflow-y-auto"
        style={{ backgroundColor: 'var(--background)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold mb-1">Channel Modes</h2>
        <p className="text-xs text-muted-foreground mb-4">{channelName}</p>

        {error && (
          <div className="mb-4 p-3 bg-destructive/10 border border-destructive rounded text-sm text-destructive">
            {error}
          </div>
        )}

        <form onSubmit={handleSave} className="space-y-5">
          {/* Boolean (type D) flags */}
          {flagLetters.length > 0 && (
            <div>
              <label className="block text-sm font-medium mb-2">Settings</label>
              <div className="grid grid-cols-2 gap-2">
                {flagLetters.map((letter) => (
                  <label key={letter} className="flex items-center gap-2 text-sm cursor-pointer">
                    <input
                      type="checkbox"
                      data-testid={`mode-flag-${letter}`}
                      checked={flags.has(letter)}
                      onChange={() => toggleFlag(letter)}
                    />
                    <span>
                      {modeLabel(letter)} <span className="text-muted-foreground">(+{letter})</span>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )}

          {/* Parameterized (type B/C) modes */}
          {[...paramB, ...paramC].length > 0 && (
            <div className="space-y-3">
              <label className="block text-sm font-medium">Parameters</label>
              {[...paramB, ...paramC].map((letter) => (
                <div key={letter}>
                  <label className="block text-xs text-muted-foreground mb-1">
                    {modeLabel(letter)} (+{letter})
                  </label>
                  <input
                    type="text"
                    data-testid={`mode-param-${letter}`}
                    value={params[letter] || ''}
                    onChange={(e) => setParam(letter, e.target.value)}
                    className="w-full px-3 py-2 text-sm border border-border rounded"
                    placeholder={letter === 'l' ? 'e.g. 50' : letter === 'k' ? 'channel key' : ''}
                  />
                </div>
              ))}
            </div>
          )}

          {/* Ban list (type A) */}
          {supportsBans && (
            <div>
              <label className="block text-sm font-medium mb-2">Ban list</label>
              {bansLoading ? (
                <p className="text-xs text-muted-foreground">Loading bans…</p>
              ) : bans.length === 0 ? (
                <p className="text-xs text-muted-foreground mb-2">No active bans.</p>
              ) : (
                <ul className="mb-2 space-y-1">
                  {bans.map((b) => {
                    const desc = describeBan(b.mask, extbanPrefix);
                    const titleParts = [];
                    if (desc.kind !== 'mask') titleParts.push(b.mask);
                    if (b.by) titleParts.push(`set by ${b.by}`);
                    return (
                      <li key={b.mask} className="flex items-center justify-between text-sm gap-2">
                        <span className="flex items-center gap-1.5 min-w-0">
                          {desc.kind === 'account' && (
                            <span className="text-[10px] uppercase tracking-wide px-1 py-0.5 rounded bg-accent text-accent-foreground shrink-0">
                              account
                            </span>
                          )}
                          <span className="font-mono truncate" title={titleParts.join(' · ') || undefined}>
                            {desc.label}
                          </span>
                        </span>
                        <button
                          type="button"
                          data-testid={`mode-ban-remove-${b.mask}`}
                          onClick={() => removeBan(b.mask)}
                          className="text-xs text-destructive hover:underline shrink-0"
                        >
                          Remove
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
              <div className="flex gap-2">
                <input
                  type="text"
                  data-testid="mode-ban-input"
                  value={newBan}
                  onChange={(e) => setNewBan(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      addBan();
                    }
                  }}
                  className="flex-1 px-3 py-2 text-sm border border-border rounded"
                  placeholder={supportsAccountExtban ? `nick!user@host or ${extbanPrefix}a:account` : 'nick!user@host'}
                />
                <button
                  type="button"
                  data-testid="mode-ban-add"
                  onClick={addBan}
                  className="px-3 py-2 text-sm border border-border rounded hover:bg-accent"
                >
                  Add ban
                </button>
              </div>
            </div>
          )}

          <div className="flex gap-2 justify-end pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm border border-border rounded hover:bg-accent"
              disabled={saving}
            >
              Cancel
            </button>
            <button
              type="submit"
              data-testid="mode-save-button"
              className="px-4 py-2 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
              disabled={saving}
            >
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
