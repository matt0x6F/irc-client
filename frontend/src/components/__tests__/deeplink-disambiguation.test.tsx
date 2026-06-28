import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const openOrJoinChannel = vi.fn();
const openQuery = vi.fn();
const setDeepLinkDisambiguation = vi.fn();
let disambig: unknown = {
  candidates: [{ networkId: 1, name: 'Libera work' }, { networkId: 2, name: 'Libera personal' }],
  targets: [{ name: '#c', isNick: false, key: '' }],
};

vi.mock('../../stores/network', () => ({
  useNetworkStore: (sel: (s: unknown) => unknown) => sel({ openOrJoinChannel, openQuery }),
}));
vi.mock('../../stores/ui', () => ({
  useUIStore: (sel: (s: unknown) => unknown) =>
    sel({ deepLinkDisambiguation: disambig, setDeepLinkDisambiguation }),
}));

import { DeepLinkDisambiguation } from '../deeplink-disambiguation';

describe('DeepLinkDisambiguation', () => {
  beforeEach(() => vi.clearAllMocks());

  it('lists candidates and joins the chosen one', () => {
    render(<DeepLinkDisambiguation />);
    expect(screen.getByText('Libera work')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Libera personal'));
    expect(openOrJoinChannel).toHaveBeenCalledWith(2, '#c');
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });

  it('renders nothing when there is no pending disambiguation', () => {
    disambig = null;
    const { container } = render(<DeepLinkDisambiguation />);
    expect(container).toBeEmptyDOMElement();
  });

  it('calls openQuery for nick targets', () => {
    disambig = {
      candidates: [{ networkId: 1, name: 'Libera work' }],
      targets: [{ name: 'alice', isNick: true, key: '' }],
    };
    render(<DeepLinkDisambiguation />);
    fireEvent.click(screen.getByText('Libera work'));
    expect(openQuery).toHaveBeenCalledWith(1, 'alice');
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });

  it('cancel button clears state without navigating', () => {
    disambig = {
      candidates: [{ networkId: 1, name: 'Libera work' }],
      targets: [{ name: '#c', isNick: false, key: '' }],
    };
    render(<DeepLinkDisambiguation />);
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(openOrJoinChannel).not.toHaveBeenCalled();
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });
});
