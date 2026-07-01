import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../wailsjs/go/main/App', () => ({ SendCommand: vi.fn().mockResolvedValue(undefined) }));
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: (_e: string, cb: (d: any) => void) => {
    // Emit a whois payload on the next tick.
    setTimeout(() => cb({ data: { whois: {
      nickname: 'alice', username: 'a', hostmask: 'very.long.host.example.invalid',
      real_name: 'Alice', server: 'irc.example', server_info: 'info',
      channels: Array.from({ length: 40 }, (_, i) => `#chan-${i}`),
      idle_time: 0, sign_on_time: 0, account_name: 'alice-acct', away: '', network: 'n', is_bot: false,
    } } }), 0);
    return () => {};
  },
}));

import { UserInfo } from './user-info';

describe('UserInfo (Whois modal)', () => {
  beforeEach(() => vi.clearAllMocks());

  it('renders inside a dialog and preserves the panel testid', async () => {
    render(<UserInfo networkId={1} nickname="alice" onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText(/Account:/)).toBeInTheDocument());
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByTestId('user-info-panel')).toBeInTheDocument();
    // No longer a fixed-width side panel.
    expect(screen.getByTestId('user-info-panel').className).not.toContain('w-80');
  });
});
