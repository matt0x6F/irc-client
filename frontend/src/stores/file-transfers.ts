import { create } from 'zustand';
import { dcc, main } from '../../wailsjs/go/models';
import {
  AcceptFileTransfer,
  CancelFileTransfer,
  ClearFileTransferHistory,
  DeclineFileTransfer,
  DiscardPartialFileTransfer,
  GetActiveFileTransfers,
  GetFileTransferSettings,
  ListFileTransferHistory,
  LocateFileTransfer,
  OpenFileTransfer,
  RemoveFileTransferHistory,
  RetryFileTransfer,
  RevealFileTransfer,
} from '../../wailsjs/go/main/App';

export type TransferFilter = 'all' | 'incoming' | 'outgoing';

export interface FileTransferEvent {
  type: 'upsert' | 'remove' | 'reset';
  transfer?: dcc.View;
  id?: string;
}

interface FileTransfersState {
  live: dcc.View[];
  history: dcc.View[];
  settings: dcc.Settings | null;
  filter: TransferFilter;
  search: string;
  nextCursor: string;
  loading: boolean;
  error: string;
  initialize: () => Promise<void>;
  receiveEvent: (event: FileTransferEvent) => void;
  setFilter: (filter: TransferFilter) => Promise<void>;
  setSearch: (search: string) => void;
  loadHistory: (append?: boolean) => Promise<void>;
  refreshSettings: () => Promise<void>;
  accept: (id: string) => Promise<void>;
  decline: (id: string) => Promise<void>;
  cancel: (id: string) => Promise<void>;
  retry: (id: string) => Promise<void>;
  discard: (id: string) => Promise<void>;
  open: (id: string) => Promise<void>;
  reveal: (id: string) => Promise<void>;
  locate: (id: string) => Promise<void>;
  remove: (id: string) => Promise<void>;
  clearHistory: () => Promise<void>;
}

const newestFirst = (rows: dcc.View[]) =>
  [...rows].sort((a, b) => new Date(b.updatedAt as unknown as string).getTime() - new Date(a.updatedAt as unknown as string).getTime());

const message = (error: unknown) => error instanceof Error ? error.message : String(error);

export const useFileTransfersStore = create<FileTransfersState>((set, get) => ({
  live: [],
  history: [],
  settings: null,
  filter: 'all',
  search: '',
  nextCursor: '',
  loading: false,
  error: '',

  initialize: async () => {
    try {
      const [live, settings] = await Promise.all([GetActiveFileTransfers(), GetFileTransferSettings()]);
      set({ live: newestFirst(live ?? []), settings, error: '' });
      await get().loadHistory();
    } catch (error) {
      set({ error: message(error) });
    }
  },

  receiveEvent: (event) => {
    if (event.type === 'reset') {
      set({ live: [], history: [], nextCursor: '' });
      void get().initialize();
      return;
    }
    if (event.type === 'remove' && event.id) {
      set((state) => ({
        live: state.live.filter((row) => row.id !== event.id),
        history: state.history.filter((row) => row.id !== event.id),
      }));
      return;
    }
    if (!event.transfer) return;
    set((state) => {
      const row = event.transfer!;
      const live = newestFirst([row, ...state.live.filter((item) => item.id !== row.id)]);
      const history = row.status === 'completed' || row.status === 'failed' || row.status === 'canceled' || row.status === 'declined' || row.status === 'resumable'
        ? newestFirst([row, ...state.history.filter((item) => item.id !== row.id)])
        : state.history;
      return { live, history, error: '' };
    });
  },

  setFilter: async (filter) => {
    set({ filter, history: [], nextCursor: '' });
    await get().loadHistory();
  },
  setSearch: (search) => set({ search }),
  loadHistory: async (append = false) => {
    const state = get();
    set({ loading: true, error: '' });
    try {
      const direction = state.filter === 'all' ? '' : state.filter;
      const page: main.FileTransferPage = await ListFileTransferHistory(
        direction,
        state.search.trim(),
        append ? state.nextCursor : '',
        50,
      );
      set({
        history: append ? [...state.history, ...(page.transfers ?? [])] : (page.transfers ?? []),
        nextCursor: page.nextCursor ?? '',
        loading: false,
      });
    } catch (error) {
      set({ loading: false, error: message(error) });
    }
  },
  refreshSettings: async () => {
    try { set({ settings: await GetFileTransferSettings() }); } catch (error) { set({ error: message(error) }); }
  },

  accept: async (id) => { await AcceptFileTransfer(id); },
  decline: async (id) => { await DeclineFileTransfer(id); },
  cancel: async (id) => { await CancelFileTransfer(id); },
  retry: async (id) => { await RetryFileTransfer(id); },
  discard: async (id) => { await DiscardPartialFileTransfer(id); },
  open: async (id) => { await OpenFileTransfer(id); },
  reveal: async (id) => { await RevealFileTransfer(id); },
  locate: async (id) => { await LocateFileTransfer(id); await get().loadHistory(); },
  remove: async (id) => { await RemoveFileTransferHistory(id); set((state) => ({ history: state.history.filter((row) => row.id !== id), live: state.live.filter((row) => row.id !== id) })); },
  clearHistory: async () => { await ClearFileTransferHistory(); set({ history: [], nextCursor: '' }); },
}));

export function transferAttentionCount(rows: dcc.View[]): number {
  return rows.filter((row) => row.status === 'offered' || row.status === 'failed').length;
}
