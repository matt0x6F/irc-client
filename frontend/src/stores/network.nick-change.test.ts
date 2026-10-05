import { beforeEach, describe, expect, it } from 'vitest';
import { useNetworkStore } from './network';

beforeEach(() => useNetworkStore.setState({
  selectedNetwork: 1,
  selectedChannel: 'pm:Alice',
  pmPaneRename: null,
  caseMapping: {},
  messages: [],
  channelContextByPane: new Map([['pm:Alice', '#autumn']]),
  unreadCounts: new Map([['1:pm:Alice', 2], ['2:pm:Alice', 3]]),
  viewMode: 'anchored',
  anchoredMessageId: 42,
}));

describe('observed private-message nick changes', () => {
  it('moves the open pane and reply context while retaining its reading position', () => {
    useNetworkStore.getState().renamePrivateMessage(1, 'alice', 'spookyAlice');
    const state = useNetworkStore.getState();
    expect(state.selectedChannel).toBe('pm:spookyAlice');
    expect(state.pmPaneRename).toEqual({ networkId: 1, from: 'pm:Alice', to: 'pm:spookyAlice' });
    expect(state.channelContextByPane.get('pm:spookyAlice')).toBe('#autumn');
    expect(state.channelContextByPane.has('pm:Alice')).toBe(false);
    expect(state.viewMode).toBe('anchored');
    expect(state.anchoredMessageId).toBe(42);
    expect([...state.unreadCounts]).toEqual([['2:pm:Alice', 3], ['1:pm:spookyAlice', 2]]);
  });

  it('does not switch a conversation on another network', () => {
    useNetworkStore.getState().renamePrivateMessage(2, 'Alice', 'spookyAlice');
    expect(useNetworkStore.getState().selectedChannel).toBe('pm:Alice');
    expect(useNetworkStore.getState().channelContextByPane.has('pm:Alice')).toBe(true);
    expect(useNetworkStore.getState().unreadCounts.get('2:pm:spookyAlice')).toBe(3);
  });

  it('combines unread counts when there is already a pane for the new nick', () => {
    useNetworkStore.setState({ unreadCounts: new Map([['1:pm:Alice', 2], ['1:pm:spookyAlice', 4]]) });
    useNetworkStore.getState().renamePrivateMessage(1, 'Alice', 'spookyAlice');
    expect([...useNetworkStore.getState().unreadCounts]).toEqual([['1:pm:spookyAlice', 6]]);
  });
});
