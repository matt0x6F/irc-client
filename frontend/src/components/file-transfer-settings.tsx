import { useEffect, useState } from 'react';
import { FolderOpen, Shield, Trash2, TriangleAlert } from 'lucide-react';
import { dcc } from '../../wailsjs/go/models';
import {
  ChooseFileTransferDirectory,
  ClearFileTransferHistory,
  GetFileTransferSettings,
  UpdateFileTransferSettings,
} from '../../wailsjs/go/main/App';

function Switch({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  return <button type="button" role="switch" aria-label="Allow file transfers" aria-checked={checked} onClick={() => onChange(!checked)} className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${checked ? 'bg-primary' : 'bg-muted-foreground/30'}`}><span className={`h-4 w-4 rounded-full bg-white shadow transition-transform ${checked ? 'translate-x-[18px]' : 'translate-x-0.5'}`} /></button>;
}

export function FileTransferSettings() {
  const [settings, setSettings] = useState<dcc.Settings | null>(null);
  const [disablePrompt, setDisablePrompt] = useState(false);
  const [clearPrompt, setClearPrompt] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => { void GetFileTransferSettings().then(setSettings).catch((e) => setError(String(e))); }, []);

  const persist = async (next: dcc.Settings, cancelActive = false) => {
    setError('');
    try {
      await UpdateFileTransferSettings(next, cancelActive);
      setSettings(next);
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1600);
      return true;
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e);
      if (!next.enabled && text.toLowerCase().includes('active')) setDisablePrompt(true);
      else setError(text);
      return false;
    }
  };

  if (!settings) return <div className="text-sm text-muted-foreground">Loading File Transfers settings…</div>;

  return (
    <div className="mb-6 max-w-3xl">
      <div className="mb-5"><h3 className="text-md font-semibold">Privacy &amp; safety</h3><p className="mt-1 text-sm text-muted-foreground">Control features that connect directly to other people.</p></div>

      {error && <div className="mb-4 flex gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"><TriangleAlert className="mt-0.5 shrink-0" size={15} />{error}</div>}

      <section className="overflow-hidden rounded-xl border border-border bg-card/50 shadow-[var(--shadow-sm)]">
        <div className="flex items-start justify-between gap-5 p-5">
          <div className="flex gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Shield size={17} /></div>
            <div><div className="text-sm font-medium">Allow file transfers</div><p className="mt-1 max-w-xl text-xs leading-relaxed text-muted-foreground">Let people offer you files and add “Send a file…” to user menus. You always choose whether to accept an incoming file.</p></div>
          </div>
          <Switch checked={settings.enabled} onChange={(enabled) => void persist(dcc.Settings.createFrom({ ...settings, enabled }))} />
        </div>

        <div className={`border-t border-border p-5 ${!settings.enabled ? 'pointer-events-none opacity-50' : ''}`}>
          <label className="text-sm font-medium">Received files</label>
          <p className="mt-1 text-xs text-muted-foreground">Cascade suggests this folder when you accept a file. You can still choose somewhere else.</p>
          <div className="mt-3 flex gap-2"><div className="min-w-0 flex-1 truncate rounded-md border border-border bg-muted/30 px-3 py-2 text-sm" title={settings.downloadDirectory}>{settings.downloadDirectory}</div><button className="inline-flex items-center gap-2 rounded-md border border-border px-3 py-2 text-sm hover:bg-accent" onClick={() => void ChooseFileTransferDirectory().then((path) => { if (path) void persist(dcc.Settings.createFrom({ ...settings, downloadDirectory: path })); })}><FolderOpen size={15} />Choose…</button></div>
        </div>

        <div className="border-t border-border p-5">
          <label htmlFor="transfer-retention" className="text-sm font-medium">Keep transfer history</label>
          <p className="mt-1 text-xs text-muted-foreground">History stores names and transfer details, never file contents. Removing history does not delete completed files.</p>
          <select id="transfer-retention" value={settings.historyRetention} onChange={(event) => void persist(dcc.Settings.createFrom({ ...settings, historyRetention: event.target.value }))} className="mt-3 h-9 rounded-md border border-border bg-background px-3 text-sm">
            <option value="forever">Forever</option><option value="30_days">30 days</option><option value="none">Don’t keep</option>
          </select>
          <div className="mt-4"><button className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-destructive" onClick={() => setClearPrompt(true)}><Trash2 size={14} />Clear transfer history…</button></div>
        </div>

        <details className="border-t border-border p-5">
          <summary className="cursor-pointer text-sm font-medium">Connection compatibility (DCC)</summary>
          <div className="mt-4 space-y-4 text-sm">
            <div className="rounded-lg bg-amber-500/10 p-3 text-xs leading-relaxed text-amber-900 dark:text-amber-200">File transfers connect directly and are not encrypted. Your IP address is visible to the other person. Cascade does not contact STUN services or change router settings.</div>
            <label className="block"><span className="text-xs font-medium text-muted-foreground">Connection mode</span><select value={settings.connectionMode} onChange={(e) => setSettings(dcc.Settings.createFrom({ ...settings, connectionMode: e.target.value }))} className="mt-1 block h-9 w-full rounded-md border border-border bg-background px-3"><option value="automatic">Automatic (recommended)</option><option value="classic">Classic</option><option value="passive">Passive / reverse</option></select></label>
            <label className="block"><span className="text-xs font-medium text-muted-foreground">Address to advertise (optional)</span><input value={settings.advertisedAddress} onChange={(e) => setSettings(dcc.Settings.createFrom({ ...settings, advertisedAddress: e.target.value }))} placeholder="203.0.113.10" className="mt-1 block h-9 w-full rounded-md border border-border bg-background px-3 outline-none focus:border-primary" /></label>
            <div><span className="text-xs font-medium text-muted-foreground">Local port range (optional)</span><div className="mt-1 flex items-center gap-2"><input aria-label="First port" type="number" min="0" max="65535" value={settings.portMin || ''} onChange={(e) => setSettings(dcc.Settings.createFrom({ ...settings, portMin: Number(e.target.value) }))} placeholder="Start" className="h-9 min-w-0 flex-1 rounded-md border border-border bg-background px-3" /><span className="text-muted-foreground">to</span><input aria-label="Last port" type="number" min="0" max="65535" value={settings.portMax || ''} onChange={(e) => setSettings(dcc.Settings.createFrom({ ...settings, portMax: Number(e.target.value) }))} placeholder="End" className="h-9 min-w-0 flex-1 rounded-md border border-border bg-background px-3" /></div></div>
            <div className="flex items-center gap-3"><button className="rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground" onClick={() => void persist(settings)}>Save compatibility settings</button>{saved && <span className="text-xs text-green-700 dark:text-green-400">Saved</span>}</div>
          </div>
        </details>
      </section>

      {disablePrompt && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"><div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-lg)]"><h2 className="font-semibold">Turn off file transfers?</h2><p className="mt-2 text-sm text-muted-foreground">Files are still transferring. Turning this off will cancel them and close direct connections.</p><div className="mt-5 flex justify-end gap-2"><button className="rounded-md px-3 py-2 text-sm hover:bg-accent" onClick={() => setDisablePrompt(false)}>Keep transfers on</button><button className="rounded-md bg-destructive px-3 py-2 text-sm text-destructive-foreground" onClick={() => { setDisablePrompt(false); void persist(dcc.Settings.createFrom({ ...settings, enabled: false }), true); }}>Turn off and cancel</button></div></div></div>}
      {clearPrompt && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"><div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-lg)]"><h2 className="font-semibold">Clear transfer history?</h2><p className="mt-2 text-sm text-muted-foreground">Completed files will stay where they are.</p><div className="mt-5 flex justify-end gap-2"><button className="rounded-md px-3 py-2 text-sm hover:bg-accent" onClick={() => setClearPrompt(false)}>Cancel</button><button className="rounded-md bg-destructive px-3 py-2 text-sm text-destructive-foreground" onClick={() => { setClearPrompt(false); void ClearFileTransferHistory(); }}>Clear history</button></div></div></div>}
    </div>
  );
}
