import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../wailsjs/go/main/App', () => ({
  GetJoinedChannels: vi.fn(),
  SendCommand: vi.fn().mockResolvedValue(undefined),
}));

import { GetJoinedChannels, SendCommand } from '../../wailsjs/go/main/App';
import { InviteToChannelModal } from './invite-to-channel-modal';

const chans = (...names: string[]) => names.map((name) => ({ name })) as any;

describe('InviteToChannelModal', () => {
  beforeEach(() => vi.clearAllMocks());

  it('lists joined channels except the current one and filters on typing', async () => {
    (GetJoinedChannels as any).mockResolvedValue(chans('#ai', '#arduino', '#go-nuts', '##here'));
    render(<InviteToChannelModal networkId={1} nick="alice" currentChannel="##here" onClose={() => {}} />);

    await waitFor(() => expect(screen.getByText('#arduino')).toBeInTheDocument());
    expect(screen.queryByText('##here')).not.toBeInTheDocument(); // current channel excluded

    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'ard' } });
    expect(screen.getByText('#arduino')).toBeInTheDocument();
    expect(screen.queryByText('#ai')).not.toBeInTheDocument();
  });

  it('sends /invite and closes on click', async () => {
    (GetJoinedChannels as any).mockResolvedValue(chans('#ai'));
    const onClose = vi.fn();
    render(<InviteToChannelModal networkId={1} nick="alice" currentChannel={null} onClose={onClose} />);
    await waitFor(() => expect(screen.getByText('#ai')).toBeInTheDocument());

    fireEvent.click(screen.getByText('#ai'));
    expect(SendCommand).toHaveBeenCalledWith(1, '/invite alice #ai');
    expect(onClose).toHaveBeenCalled();
  });

  it('invites the active row on Enter after ArrowDown', async () => {
    (GetJoinedChannels as any).mockResolvedValue(chans('#ai', '#arduino'));
    render(<InviteToChannelModal networkId={1} nick="alice" currentChannel={null} onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText('#arduino')).toBeInTheDocument());

    const input = screen.getByPlaceholderText(/search/i);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(SendCommand).toHaveBeenCalledWith(1, '/invite alice #arduino');
  });

  it('shows an empty state when there are no other channels', async () => {
    (GetJoinedChannels as any).mockResolvedValue(chans('##here'));
    render(<InviteToChannelModal networkId={1} nick="alice" currentChannel="##here" onClose={() => {}} />);
    await waitFor(() =>
      expect(screen.getByText(/not in any other channels/i)).toBeInTheDocument()
    );
  });
});
