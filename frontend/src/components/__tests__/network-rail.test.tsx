import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { NetworkRail } from '../network-rail';
import { storage } from '../../../wailsjs/go/models';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetNetworkIcon: vi.fn().mockResolvedValue(''),
  OpenSettings: vi.fn().mockResolvedValue(undefined),
}));

const nets = [
  storage.Network.createFrom({ id: 1, name: 'Libera' }),
  storage.Network.createFrom({ id: 2, name: 'OFTC' }),
];

function base(over = {}) {
  return {
    networks: nets, selectedNetwork: 1, activityActive: false,
    connectionStatus: { 1: true, 2: false }, connectingNetworks: {},
    unreadCounts: new Map<string, number>(), activityItems: [],
    onSelectNetwork: vi.fn(), onSelectActivity: vi.fn(), onAddNetwork: vi.fn(),
    onNetworkContextMenu: vi.fn(), onReordered: vi.fn(), ...over,
  };
}

describe('NetworkRail', () => {
  it('renders one tile per network plus activity and add', () => {
    render(<NetworkRail {...base()} />);
    expect(screen.getAllByTestId('network-tile')).toHaveLength(2);
    expect(screen.getByTestId('rail-activity')).toBeInTheDocument();
    expect(screen.getByTestId('rail-add-network')).toBeInTheDocument();
  });

  it('fires activity + add callbacks', () => {
    const b = base();
    render(<NetworkRail {...b} />);
    fireEvent.click(screen.getByTestId('rail-activity'));
    expect(b.onSelectActivity).toHaveBeenCalled();
    fireEvent.click(screen.getByTestId('rail-add-network'));
    expect(b.onAddNetwork).toHaveBeenCalled();
  });

  it('marks the activity tile active when activityActive', () => {
    render(<NetworkRail {...base({ activityActive: true })} />);
    expect(screen.getByTestId('rail-activity')).toHaveAttribute('data-active', 'true');
  });
});
