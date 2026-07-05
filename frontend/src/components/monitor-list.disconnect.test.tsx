import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

const getMonitorList = vi.fn().mockResolvedValue([{ nick: 'vdamewood', online: true }]);
vi.mock('../../wailsjs/go/main/App', () => ({
  GetMonitorList: (...a: unknown[]) => getMonitorList(...a),
  AddMonitor: vi.fn().mockResolvedValue(undefined),
  RemoveMonitor: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from '../stores/network';
import { MonitorList } from './monitor-list';

describe('MonitorList presence while disconnected', () => {
  beforeEach(() => {
    useNetworkStore.setState({ connectionStatus: {}, monitor: {}, userMeta: {}, caseMapping: {} });
  });

  it('renders a live "Online" dot while the network is connected', async () => {
    useNetworkStore.setState({ connectionStatus: { 1: true } });
    render(<MonitorList networkId={1} />);
    await waitFor(() => expect(screen.getByText('vdamewood')).toBeInTheDocument());
    expect(screen.getByTitle('Online')).toBeInTheDocument();
    expect(screen.getByText('Buddies (1/1 online)')).toBeInTheDocument();
  });

  it('renders a neutral "unknown" dot while the network is disconnected', async () => {
    useNetworkStore.setState({ connectionStatus: { 1: false } });
    render(<MonitorList networkId={1} />);
    await waitFor(() => expect(screen.getByText('vdamewood')).toBeInTheDocument());
    expect(screen.getByTitle('Presence unknown — disconnected')).toBeInTheDocument();
    expect(screen.queryByTitle('Online')).not.toBeInTheDocument();
    // Header drops the online-count claim we can't back while the socket is down.
    expect(screen.getByText('Buddies (1)')).toBeInTheDocument();
  });
});
