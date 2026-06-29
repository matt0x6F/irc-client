import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { groupBySender, COLLAPSE_THRESHOLD, InvitesView } from '../invites-view';
import { useNetworkStore } from '../../stores/network';

// Mock the Wails App bindings so button clicks don't throw.
vi.mock('../../../wailsjs/go/main/App', () => ({
  DismissInvite: vi.fn().mockResolvedValue(undefined),
  DismissInvitesFrom: vi.fn().mockResolvedValue(undefined),
  IgnoreInviteSender: vi.fn().mockResolvedValue(undefined),
}));

// Mock the network store so the component reads from our seeded state.
// openOrJoinChannel is called on Join button clicks.
const storeState = {
  invitesByNetwork: {} as Record<number, Array<{ inviter: string; channel: string; trusted: boolean; receivedAt: string }>>,
  openOrJoinChannel: vi.fn().mockResolvedValue(undefined),
};

vi.mock('../../stores/network', () => {
  const useNetworkStore = (sel: (s: typeof storeState) => unknown) => sel(storeState);
  useNetworkStore.setState = (patch: Partial<typeof storeState>) => Object.assign(storeState, patch);
  useNetworkStore.getState = () => storeState;
  return { useNetworkStore };
});

const inv = (inviter: string, channel: string) => ({
  inviter, channel, trusted: false, receivedAt: '2026-06-28T12:00:00Z',
});

describe('groupBySender', () => {
  it('groups invites by inviter', () => {
    const g = groupBySender([inv('a', '#1'), inv('a', '#2'), inv('b', '#3')]);
    expect(g).toHaveLength(2);
    expect(g.find((x) => x.inviter === 'a')!.channels).toHaveLength(2);
  });

  it('collapses a sender at or above the threshold', () => {
    const many = Array.from({ length: COLLAPSE_THRESHOLD }, (_, i) => inv('flood', '#' + i));
    const g = groupBySender(many);
    expect(g[0].collapsed).toBe(true);
  });

  it('keeps a small sender expanded', () => {
    const g = groupBySender([inv('a', '#1'), inv('a', '#2')]);
    expect(g[0].collapsed).toBe(false);
  });
});

describe('InvitesView anti-harassment: collapsed sender hides channel names', () => {
  // Reset the store's invite slice before each test to avoid cross-test bleed.
  beforeEach(() => {
    storeState.invitesByNetwork = {};
  });

  it('shows count-only summary and hides channel names until user expands', () => {
    // Seed 6 invites from one sender — above the collapse threshold.
    storeState.invitesByNetwork = {
      1: ['#a', '#b', '#c', '#d', '#e', '#f'].map((ch) =>
        inv('flooder', ch),
      ),
    };

    render(<InvitesView networkId={1} />);

    // The count-only summary must be visible.
    expect(screen.getByText(/invited you to 6 channels/i)).toBeInTheDocument();

    // Attacker-controlled channel names must NOT appear in the DOM initially.
    expect(screen.queryByText('#a')).toBeNull();
    expect(screen.queryByText('#f')).toBeNull();

    // Click the "Show channels" toggle to expand.
    fireEvent.click(screen.getByRole('button', { name: /show channels/i }));

    // After expansion, channel names must be visible.
    expect(screen.getByText('#a')).toBeInTheDocument();
    expect(screen.getByText('#f')).toBeInTheDocument();
  });
});
