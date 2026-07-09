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

const activityItem = (o: Partial<any> = {}) => ({
  id: 1, network_id: 1, source_type: 'highlight', target: '#dev', actor: 'alice',
  preview: 'hi', msgid: 'm1', keyword: '', seen: false, timestamp: '2026-07-06T12:00:00Z',
  trusted: false, expires_at: null, ...o,
});

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

  it('shows an unseen badge on the activity tile when there is unseen activity', () => {
    render(<NetworkRail {...base({ activityItems: [activityItem({ id: 1, seen: false })] })} />);
    const activity = screen.getByTestId('rail-activity');
    expect(activity).toHaveTextContent('1');
  });

  it('does not show a badge when nothing is unseen', () => {
    render(<NetworkRail {...base({ activityItems: [activityItem({ id: 1, seen: true })] })} />);
    const activity = screen.getByTestId('rail-activity');
    expect(activity).not.toHaveTextContent('1');
  });

  it('marks the activity tile active when activityActive', () => {
    render(<NetworkRail {...base({ activityActive: true })} />);
    expect(screen.getByTestId('rail-activity')).toHaveAttribute('data-active', 'true');
  });

  it('reorders networks on drag-and-drop', () => {
    const b = base();
    render(<NetworkRail {...b} />);
    const tiles = screen.getAllByTestId('network-tile');
    const firstWrapper = tiles[0].closest('[draggable]')!;
    const secondWrapper = tiles[1].closest('[draggable]')!;
    fireEvent.dragStart(firstWrapper);
    fireEvent.drop(secondWrapper);
    expect(b.onReordered).toHaveBeenCalledWith([2, 1]);
  });

  it('clears stale drag state on dragEnd so a later unrelated drop is a no-op', () => {
    const b = base();
    render(<NetworkRail {...b} />);
    const tiles = screen.getAllByTestId('network-tile');
    const firstWrapper = tiles[0].closest('[draggable]')!;
    const secondWrapper = tiles[1].closest('[draggable]')!;
    fireEvent.dragStart(firstWrapper);
    fireEvent.dragEnd(firstWrapper);
    fireEvent.drop(secondWrapper);
    expect(b.onReordered).not.toHaveBeenCalled();
  });
});
