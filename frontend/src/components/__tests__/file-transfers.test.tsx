import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

vi.mock('../../../wailsjs/go/main/App', () => ({
  AcceptFileTransfer: vi.fn(), CancelFileTransfer: vi.fn(), ClearFileTransferHistory: vi.fn(),
  DeclineFileTransfer: vi.fn(), DiscardPartialFileTransfer: vi.fn(), GetActiveFileTransfers: vi.fn(),
  GetFileTransferSettings: vi.fn(), ListFileTransferHistory: vi.fn(), LocateFileTransfer: vi.fn(),
  OpenFileTransfer: vi.fn(), RemoveFileTransferHistory: vi.fn(), RetryFileTransfer: vi.fn(), RevealFileTransfer: vi.fn(),
}));

import { dcc } from '../../../wailsjs/go/models';
import { FileTransfers } from '../file-transfers';
import { useFileTransfersStore } from '../../stores/file-transfers';

describe('FileTransfers', () => {
  beforeEach(() => useFileTransfersStore.setState({
    live: [dcc.View.createFrom({ id: 'offer', networkId: 1, networkName: 'Libera', peer: 'Alice', direction: 'incoming', filename: 'holiday photos.zip', totalBytes: 2048, transferredBytes: 0, status: 'offered', createdAt: '2026-07-14T10:00:00Z', updatedAt: '2026-07-14T10:00:00Z', speedBps: 0, etaSeconds: 0, fileAvailable: false, resumable: false })],
    history: [], settings: dcc.Settings.createFrom({ enabled: true, downloadDirectory: '/tmp', historyRetention: 'forever', connectionMode: 'automatic', advertisedAddress: '', portMin: 0, portMax: 0 }), error: '',
  }));

  it('uses human-facing direct-connection safety copy for incoming offers', () => {
    render(<FileTransfers />);
    expect(screen.getByRole('heading', { name: 'File Transfers' })).toBeInTheDocument();
    expect(screen.getByText('holiday photos.zip')).toBeInTheDocument();
    expect(screen.getByText(/connects directly to Alice/i)).toBeInTheDocument();
    expect(screen.getByText(/IP address will be visible/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Choose where to save…' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Decline' })).toBeInTheDocument();
  });
});
