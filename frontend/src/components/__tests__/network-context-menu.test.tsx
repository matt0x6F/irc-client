import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { storage } from '../../../wailsjs/go/models';
import { NetworkContextMenu } from '../network-context-menu';
import { useNetworkStore } from '../../stores/network';
import { networkUnreadTotal } from '../../lib/network-unread';

describe('NetworkContextMenu', () => {
  it.each([true, false])('marks all network conversations read when connected=%s', (connected) => {
    useNetworkStore.setState({
      selectedNetwork: 2, selectedChannel: '#elsewhere',
      unreadCounts: new Map([
        ['1:#cascade', 3], ['1:#python', 2], ['1:pm:bob', 1],
        ['2:#cascade', 4], ['10:#cascade', 5],
      ]),
    });
    const onClose = vi.fn();
    render(<NetworkContextMenu x={0} y={0}
      network={storage.Network.createFrom({ id: 1, name: 'Libera Chat' })}
      connected={connected} connecting={false}
      onConnect={vi.fn()} onDisconnect={vi.fn()} onDelete={vi.fn()}
      onReloadNetworks={vi.fn()} onClose={onClose} />);

    fireEvent.click(screen.getByRole('button', { name: 'Mark as read' }));

    const state = useNetworkStore.getState();
    expect(networkUnreadTotal(state.unreadCounts, 1)).toBe(0);
    expect(state.unreadCounts).toEqual(new Map([
      ['2:#cascade', 4], ['10:#cascade', 5],
    ]));
    expect(state.selectedNetwork).toBe(2);
    expect(state.selectedChannel).toBe('#elsewhere');
    expect(onClose).toHaveBeenCalledOnce();
  });
});
