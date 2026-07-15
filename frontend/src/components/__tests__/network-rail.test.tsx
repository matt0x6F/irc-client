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
    fileTransfersActive: false, fileTransferAttention: 0,
    connectionStatus: { 1: true, 2: false }, connectingNetworks: {},
    unreadCounts: new Map<string, number>(), activityItems: [],
    onSelectNetwork: vi.fn(), onSelectActivity: vi.fn(), onSelectFileTransfers: vi.fn(), onAddNetwork: vi.fn(),
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

  it('opens File Transfers and badges requests needing attention', () => {
    const b = base({ fileTransferAttention: 2 });
    render(<NetworkRail {...b} />);
    fireEvent.click(screen.getByTestId('rail-file-transfers'));
    expect(b.onSelectFileTransfers).toHaveBeenCalled();
    expect(screen.getByTestId('rail-file-transfers-badge')).toHaveTextContent('2');
  });

  it('shows an unseen badge on the activity tile when there is unseen activity', () => {
    render(<NetworkRail {...base({ activityItems: [activityItem({ id: 1, seen: false })] })} />);
    expect(screen.getByTestId('rail-activity-badge')).toHaveTextContent('1');
  });

  it('renders the unseen badge outside the clip-path\'d activity button', () => {
    render(<NetworkRail {...base({ activityItems: [activityItem({ id: 1, seen: false })] })} />);
    const badge = screen.getByTestId('rail-activity-badge');
    // clip-path clips ALL descendants (unlike overflow on a non-positioned
    // ancestor), so the badge must be a sibling of the squircle button, not
    // a child — otherwise it is sliced to the squircle's curve.
    expect(screen.getByTestId('rail-activity')).not.toContainElement(badge);
  });

  it('does not show a badge when nothing is unseen', () => {
    render(<NetworkRail {...base({ activityItems: [activityItem({ id: 1, seen: true })] })} />);
    expect(screen.queryByTestId('rail-activity-badge')).not.toBeInTheDocument();
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

  it('gives the tile column top headroom so the first tile badge is not clipped', () => {
    render(<NetworkRail {...base()} />);
    // The tile column is overflow-clipped (scroll container); the unread badge
    // overhangs each tile by 4px, so the column needs 4px of top padding.
    expect(screen.getByTestId('rail-tiles')).toHaveStyle({ paddingTop: '4px' });
  });

  it('shows a tooltip with the network name on tile hover', () => {
    render(<NetworkRail {...base()} />);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    fireEvent.mouseOver(screen.getAllByTestId('network-tile')[0]);
    expect(screen.getByRole('tooltip')).toHaveTextContent('Libera');
    fireEvent.mouseOut(screen.getAllByTestId('network-tile')[0]);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('shows tooltips for the fixed rail buttons', () => {
    render(<NetworkRail {...base()} />);
    fireEvent.mouseOver(screen.getByTestId('rail-activity'));
    expect(screen.getByRole('tooltip')).toHaveTextContent('Activity');
    fireEvent.mouseOut(screen.getByTestId('rail-activity'));
    fireEvent.mouseOver(screen.getByTestId('rail-add-network'));
    expect(screen.getByRole('tooltip')).toHaveTextContent('Add network');
  });

  it('hides the tooltip when a tile drag starts', () => {
    render(<NetworkRail {...base()} />);
    const tile = screen.getAllByTestId('network-tile')[0];
    fireEvent.mouseOver(tile);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.dragStart(tile.closest('[draggable]')!);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
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
