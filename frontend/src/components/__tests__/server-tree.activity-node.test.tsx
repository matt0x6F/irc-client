import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetChannels: vi.fn().mockResolvedValue([]),
  GetJoinedChannels: vi.fn().mockResolvedValue([]),
  GetOpenChannels: vi.fn().mockResolvedValue([]),
  GetServers: vi.fn().mockResolvedValue([]),
  LeaveChannel: vi.fn().mockResolvedValue(undefined),
  CloseChannel: vi.fn().mockResolvedValue(undefined),
  ToggleChannelAutoJoin: vi.fn().mockResolvedValue(undefined),
  ToggleNetworkAutoConnect: vi.fn().mockResolvedValue(undefined),
  SetChannelOpen: vi.fn().mockResolvedValue(undefined),
  GetPrivateMessageConversations: vi.fn().mockResolvedValue([]),
  SendCommand: vi.fn().mockResolvedValue(undefined),
  SetPrivateMessageOpen: vi.fn().mockResolvedValue(undefined),
  ClearPaneFocus: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
}));

import { useNetworkStore } from '../../stores/network';
import { ServerTree } from '../server-tree';

const item = (o: Partial<any>) => ({
  id: 1, network_id: 1, source_type: 'highlight', target: '#dev', actor: 'alice',
  preview: 'hi', msgid: 'm1', keyword: '', seen: false, timestamp: '2026-07-06T12:00:00Z',
  trusted: false, expires_at: null, ...o,
});

const baseProps = {
  servers: [],
  selectedServer: null,
  selectedChannel: null,
  onSelectServer: vi.fn(),
  onSelectChannel: vi.fn(),
  onConnect: vi.fn(),
  onDisconnect: vi.fn(),
  onDelete: vi.fn(),
  connectionStatus: {},
  unreadCounts: new Map(),
  onShowUserInfo: vi.fn(),
};

describe('ServerTree activity node', () => {
  beforeEach(() => {
    useNetworkStore.setState({ activityItems: [] });
  });

  it('shows an Activity node with an unseen badge', () => {
    useNetworkStore.setState({ activityItems: [item({ id: 1 })] });
    render(<ServerTree {...baseProps} />);
    expect(screen.getByText('Activity')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();
  });

  it('calls selectActivityInbox when clicked', () => {
    useNetworkStore.setState({ activityItems: [item({ id: 1 })] });
    const spy = vi.spyOn(useNetworkStore.getState(), 'selectActivityInbox').mockResolvedValue(undefined);
    render(<ServerTree {...baseProps} />);
    fireEvent.click(screen.getByText('Activity'));
    expect(spy).toHaveBeenCalled();
  });

  it('does not show a badge when there is nothing unseen', () => {
    render(<ServerTree {...baseProps} />);
    expect(screen.getByText('Activity')).toBeInTheDocument();
    expect(screen.queryByText('0')).not.toBeInTheDocument();
  });
});
