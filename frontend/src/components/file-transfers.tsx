import { useMemo, useState } from 'react';
import {
  AlertTriangle,
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  Clock3,
  ExternalLink,
  File,
  FileQuestion,
  FolderOpen,
  MoreHorizontal,
  RefreshCw,
  Search,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react';
import { dcc } from '../../wailsjs/go/models';
import { useFileTransfersStore, type TransferFilter } from '../stores/file-transfers';

const activeStates = new Set(['offered', 'queued', 'negotiating', 'connecting', 'transferring']);

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 1) return value === 0 ? '0 B' : '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const amount = value / Math.pow(1024, index);
  return `${amount.toFixed(index === 0 || amount >= 10 ? 0 : 1)} ${units[index]}`;
}

function formatDate(value: unknown): string {
  const date = new Date(value as string);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }).format(date);
}

function statusLabel(status: string): string {
  return ({
    offered: 'Waiting for you', queued: 'Queued', negotiating: 'Offering', connecting: 'Connecting',
    transferring: 'Transferring', completed: 'Completed', failed: 'Needs attention', canceled: 'Canceled',
    declined: 'Declined', resumable: 'Ready to resume',
  } as Record<string, string>)[status] ?? status;
}

function Progress({ transfer }: { transfer: dcc.View }) {
  const percent = transfer.totalBytes === 0
    ? (transfer.status === 'completed' ? 100 : 0)
    : Math.max(0, Math.min(100, transfer.transferredBytes / transfer.totalBytes * 100));
  return (
    <div className="space-y-1.5">
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary transition-[width] duration-300" style={{ width: `${percent}%` }} />
      </div>
      <div className="flex justify-between gap-3 text-xs text-muted-foreground">
        <span>{formatBytes(transfer.transferredBytes)} of {formatBytes(transfer.totalBytes)}</span>
        <span>
          {transfer.speedBps > 0 ? `${formatBytes(transfer.speedBps)}/s` : ''}
          {transfer.etaSeconds > 0 ? ` · ${transfer.etaSeconds < 60 ? `${transfer.etaSeconds}s` : `${Math.ceil(transfer.etaSeconds / 60)}m`} left` : ''}
        </span>
      </div>
    </div>
  );
}

function TransferCard({ transfer }: { transfer: dcc.View }) {
  const store = useFileTransfersStore();
  const incoming = transfer.direction === 'incoming';
  const offered = transfer.status === 'offered';
  const cancelable = activeStates.has(transfer.status) && !offered;
  const retryable = transfer.status === 'failed' && !transfer.resumable;
  const resumeable = transfer.status === 'resumable' || transfer.resumable;

  return (
    <article className={`rounded-xl border bg-card/70 p-4 shadow-[var(--shadow-sm)] ${offered ? 'border-primary/50' : 'border-border'}`}>
      <div className="flex items-start gap-3">
        <div className={`mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl ${incoming ? 'bg-primary/10 text-primary' : 'bg-accent text-foreground'}`}>
          {incoming ? <ArrowDownToLine size={19} /> : <ArrowUpFromLine size={19} />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <h3 className="truncate font-medium" title={transfer.filename}>{transfer.filename}</h3>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {incoming ? `From ${transfer.peer}` : `To ${transfer.peer}`} · {transfer.networkName} · {formatBytes(transfer.totalBytes)}
              </p>
            </div>
            <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${transfer.status === 'failed' ? 'bg-destructive/10 text-destructive' : offered ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
              {statusLabel(transfer.status)}
            </span>
          </div>

          {offered && (
            <div className="mt-3 rounded-lg bg-amber-500/10 px-3 py-2.5 text-sm text-amber-900 dark:text-amber-200">
              <div className="flex gap-2"><ShieldAlert className="mt-0.5 shrink-0" size={16} /><p>This connects directly to {transfer.peer}. Your IP address will be visible, and the transfer is not encrypted.</p></div>
            </div>
          )}
          {(transfer.status === 'transferring' || transfer.status === 'connecting' || transfer.status === 'queued' || transfer.status === 'negotiating') && <div className="mt-3"><Progress transfer={transfer} /></div>}
          {transfer.error && <p className="mt-3 flex gap-1.5 text-sm text-destructive"><AlertTriangle className="mt-0.5 shrink-0" size={15} />{transfer.error}</p>}

          <div className="mt-3 flex flex-wrap justify-end gap-2">
            {offered && <button className="rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:bg-accent" onClick={() => void store.decline(transfer.id)}>Decline</button>}
            {offered && <button className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90" onClick={() => void store.accept(transfer.id)}>Choose where to save…</button>}
            {cancelable && <button className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent" onClick={() => void store.cancel(transfer.id)}>Cancel</button>}
            {retryable && <button className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground" onClick={() => void store.retry(transfer.id)}><RefreshCw size={14} />Retry</button>}
            {resumeable && <button className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground" onClick={() => void store.accept(transfer.id)}>Resume</button>}
            {resumeable && <button className="rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:bg-accent" onClick={() => void store.discard(transfer.id)}>Discard partial download</button>}
          </div>
        </div>
      </div>
    </article>
  );
}

function HistoryRow({ transfer }: { transfer: dcc.View }) {
  const store = useFileTransfersStore();
  const [menu, setMenu] = useState(false);
  const incoming = transfer.direction === 'incoming';
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b border-border/60 px-4 py-3 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_140px_150px_auto]">
      <div className="flex min-w-0 items-center gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground"><File size={17} /></div>
        <div className="min-w-0">
          <button disabled={!transfer.fileAvailable} onClick={() => void store.open(transfer.id)} className="block max-w-full truncate text-left text-sm font-medium enabled:hover:underline disabled:cursor-default" title={transfer.filename}>{transfer.filename}</button>
          <div className="mt-0.5 flex flex-wrap gap-x-1 text-xs text-muted-foreground">
            <span>{incoming ? `From ${transfer.peer}` : `To ${transfer.peer}`}</span><span>·</span><span>{transfer.networkName}</span><span>·</span><span>{formatBytes(transfer.totalBytes)}</span>
          </div>
          {!transfer.fileAvailable && transfer.status === 'completed' && <button className="mt-1 inline-flex items-center gap-1 text-xs text-amber-700 hover:underline dark:text-amber-300" onClick={() => void store.locate(transfer.id)}><FileQuestion size={12} />File no longer found · Locate file…</button>}
        </div>
      </div>
      <span className="hidden text-sm text-muted-foreground sm:block">{incoming ? 'Received' : 'Sent'}</span>
      <div className="hidden sm:block">
        <div className="text-sm">{statusLabel(transfer.status)}</div>
        <div className="text-xs text-muted-foreground">{formatDate(transfer.completedAt ?? transfer.updatedAt)}</div>
      </div>
      <div className="relative justify-self-end">
        <button aria-label={`Actions for ${transfer.filename}`} className="rounded-md p-2 text-muted-foreground hover:bg-accent hover:text-foreground" onClick={() => setMenu(!menu)}><MoreHorizontal size={17} /></button>
        {menu && (
          <div className="absolute right-0 top-9 z-20 w-44 rounded-lg border border-border bg-card p-1 shadow-[var(--shadow-lg)]">
            {transfer.fileAvailable && <button className="flex w-full items-center gap-2 rounded px-2.5 py-2 text-sm hover:bg-accent" onClick={() => { setMenu(false); void store.open(transfer.id); }}><ExternalLink size={14} />Open</button>}
            {transfer.fileAvailable && <button className="flex w-full items-center gap-2 rounded px-2.5 py-2 text-sm hover:bg-accent" onClick={() => { setMenu(false); void store.reveal(transfer.id); }}><FolderOpen size={14} />Show in folder</button>}
            {!transfer.fileAvailable && transfer.status === 'completed' && <button className="flex w-full items-center gap-2 rounded px-2.5 py-2 text-sm hover:bg-accent" onClick={() => { setMenu(false); void store.locate(transfer.id); }}><FileQuestion size={14} />Locate file…</button>}
            <button className="flex w-full items-center gap-2 rounded px-2.5 py-2 text-sm text-destructive hover:bg-destructive/10" onClick={() => { setMenu(false); void store.remove(transfer.id); }}><Trash2 size={14} />Remove from history</button>
          </div>
        )}
      </div>
    </div>
  );
}

export function FileTransfers() {
  const store = useFileTransfersStore();
  const [tab, setTab] = useState<'progress' | 'history'>('progress');
  const [confirmClear, setConfirmClear] = useState(false);
  const current = useMemo(() => store.live.filter((row) => activeStates.has(row.status) || row.status === 'failed' || row.status === 'resumable'), [store.live]);
  const filters: Array<[TransferFilter, string]> = [['all', 'All'], ['incoming', 'Received'], ['outgoing', 'Sent']];

  return (
    <section className="mx-auto flex h-full w-full max-w-6xl flex-col overflow-hidden" aria-label="File Transfers">
      <header className="shrink-0 px-5 pb-0 pt-6 sm:px-8">
        <div className="flex items-start justify-between gap-4">
          <div><h1 className="text-2xl font-semibold tracking-tight">File Transfers</h1><p className="mt-1 text-sm text-muted-foreground">Send files directly to people you trust.</p></div>
          {!store.settings?.enabled && <span className="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">Turned off</span>}
        </div>
        <div className="mt-6 flex gap-5 border-b border-border">
          {([['progress', 'In Progress'], ['history', 'History']] as const).map(([id, label]) => (
            <button key={id} onClick={() => setTab(id)} className={`-mb-px border-b-2 px-1 pb-3 text-sm font-medium ${tab === id ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}>{label}{id === 'progress' && current.length > 0 ? ` ${current.length}` : ''}</button>
          ))}
        </div>
      </header>

      <div className="flex-1 overflow-y-auto px-5 py-5 sm:px-8">
        {store.error && <div className="mb-4 flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"><AlertTriangle size={15} />{store.error}</div>}
        {tab === 'progress' ? (
          current.length > 0 ? <div className="grid gap-3">{current.map((transfer) => <TransferCard key={transfer.id} transfer={transfer} />)}</div> : (
            <div className="flex min-h-[360px] flex-col items-center justify-center text-center">
              <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-muted text-muted-foreground"><Check size={24} /></div>
              <h2 className="font-medium">Nothing transferring right now</h2>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">To send something, open a person’s menu and choose “Send a file…”. Incoming requests will appear here.</p>
            </div>
          )
        ) : (
          <div className="space-y-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="inline-flex self-start rounded-lg border border-border bg-muted/30 p-0.5">
                {filters.map(([id, label]) => <button key={id} onClick={() => void store.setFilter(id)} className={`rounded-md px-3 py-1.5 text-sm ${store.filter === id ? 'bg-card font-medium shadow-[var(--shadow-sm)]' : 'text-muted-foreground hover:text-foreground'}`}>{label}</button>)}
              </div>
              <form className="flex gap-2" onSubmit={(event) => { event.preventDefault(); void store.loadHistory(); }}>
                <label className="relative min-w-0 flex-1 sm:w-64"><Search className="absolute left-2.5 top-2.5 text-muted-foreground" size={15} /><input value={store.search} onChange={(event) => store.setSearch(event.target.value)} placeholder="Search files or people" className="h-9 w-full rounded-md border border-border bg-background pl-8 pr-3 text-sm outline-none focus:border-primary" /></label>
                <button className="rounded-md border border-border px-3 text-sm hover:bg-accent">Search</button>
              </form>
            </div>
            {store.history.length ? (
              <div className="overflow-visible rounded-xl border border-border bg-card/50">
                {store.history.map((transfer) => <HistoryRow key={transfer.id} transfer={transfer} />)}
              </div>
            ) : !store.loading && (
              <div className="flex min-h-[280px] flex-col items-center justify-center text-center text-muted-foreground"><Clock3 size={30} className="mb-3 opacity-50" /><p className="text-sm">No transfer history yet.</p></div>
            )}
            {store.loading && <p className="py-4 text-center text-sm text-muted-foreground">Loading transfers…</p>}
            <div className="flex items-center justify-between">
              <div>{store.nextCursor && <button className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent" onClick={() => void store.loadHistory(true)}>Load more</button>}</div>
              <button className="text-sm text-muted-foreground hover:text-destructive" onClick={() => setConfirmClear(true)}>Clear transfer history…</button>
            </div>
          </div>
        )}
      </div>

      {confirmClear && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4" role="dialog" aria-modal="true" aria-labelledby="clear-transfer-title">
          <div className="w-full max-w-sm rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-lg)]">
            <div className="flex justify-between gap-4"><h2 id="clear-transfer-title" className="font-semibold">Clear transfer history?</h2><button aria-label="Close" onClick={() => setConfirmClear(false)}><X size={18} /></button></div>
            <p className="mt-2 text-sm text-muted-foreground">This removes Cascade’s history. Files you sent or received will stay on your computer.</p>
            <div className="mt-5 flex justify-end gap-2"><button className="rounded-md px-3 py-2 text-sm hover:bg-accent" onClick={() => setConfirmClear(false)}>Cancel</button><button className="rounded-md bg-destructive px-3 py-2 text-sm text-destructive-foreground" onClick={() => { setConfirmClear(false); void store.clearHistory(); }}>Clear history</button></div>
          </div>
        </div>
      )}
    </section>
  );
}
