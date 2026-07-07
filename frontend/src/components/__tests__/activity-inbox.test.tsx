import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetActivityItems: vi.fn().mockResolvedValue([]),
  MarkActivitySeen: vi.fn().mockResolvedValue(undefined),
  MarkAllActivitySeen: vi.fn().mockResolvedValue(undefined),
  DismissActivity: vi.fn().mockResolvedValue(undefined),
  ClearSeenActivity: vi.fn().mockResolvedValue(undefined),
  ClearAllActivity: vi.fn().mockResolvedValue(undefined),
  GetMessageIDByMsgID: vi.fn().mockResolvedValue(0),
  SendCommand: vi.fn().mockResolvedValue(undefined),
  IgnoreActivitySender: vi.fn().mockResolvedValue(undefined),
}));

import { useNetworkStore } from '../../stores/network';
import { ActivityInbox } from '../activity-inbox';
import { DismissActivity, IgnoreActivitySender } from '../../../wailsjs/go/main/App';

const item = (o: Partial<any>) => ({ id: 1, network_id: 1, source_type: 'highlight', target: '#dev', actor: 'alice', preview: 'matt look here', msgid: 'm1', keyword: '', seen: false, timestamp: '2026-07-06T12:00:00Z', trusted: false, expires_at: null, ...o });

describe('ActivityInbox', () => {
  beforeEach(() => { vi.clearAllMocks(); useNetworkStore.setState({ activityItems: [] }); });

  it('shows the empty state when there is nothing', () => {
    render(<ActivityInbox />);
    expect(screen.getByText(/all caught up/i)).toBeInTheDocument();
  });

  it('renders a coalesced highlight row and dismisses it', async () => {
    useNetworkStore.setState({ activityItems: [item({ id: 1 }), item({ id: 2, timestamp: '2026-07-06T12:01:00Z' })] });
    render(<ActivityInbox />);
    expect(screen.getByText('#dev')).toBeInTheDocument();
    expect(screen.getByText(/2 highlights/i)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/dismiss/i));
    await waitFor(() => expect(DismissActivity).toHaveBeenCalled());
  });

  it('shows Join on invite rows', () => {
    useNetworkStore.setState({ activityItems: [item({ id: 3, source_type: 'invite', target: '#ops', msgid: '' })] });
    render(<ActivityInbox />);
    expect(screen.getByRole('button', { name: /join/i })).toBeInTheDocument();
  });

  it('ignores the sender from a pm row', () => {
    useNetworkStore.setState({ activityItems: [item({ id: 4, source_type: 'pm', network_id: 1, target: 'ChanServ', actor: 'ChanServ', msgid: '' })] });
    render(<ActivityInbox />);
    const btn = screen.getByLabelText('Ignore sender');
    fireEvent.click(btn);
    expect(IgnoreActivitySender).toHaveBeenCalledWith(1, 'ChanServ');
  });
});
