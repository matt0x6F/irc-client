import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const { applyDeepLinkTargets } = vi.hoisted(() => ({
  applyDeepLinkTargets: vi.fn(() => Promise.resolve()),
}));

const setDeepLinkDisambiguation = vi.fn();
let disambig: unknown = {
  candidates: [{ networkId: 1, name: 'Libera work' }, { networkId: 2, name: 'Libera personal' }],
  targets: [{ name: '#c', isNick: false, key: '' }],
};

vi.mock('../../stores/deeplink', () => ({
  applyDeepLinkTargets,
}));
vi.mock('../../stores/ui', () => ({
  useUIStore: (sel: (s: unknown) => unknown) =>
    sel({ deepLinkDisambiguation: disambig, setDeepLinkDisambiguation }),
}));

import { DeepLinkDisambiguation } from '../deeplink-disambiguation';

describe('DeepLinkDisambiguation', () => {
  beforeEach(() => vi.clearAllMocks());

  it('lists candidates and delegates to applyDeepLinkTargets on choose', () => {
    render(<DeepLinkDisambiguation />);
    expect(screen.getByText('Libera work')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Libera personal'));
    expect(applyDeepLinkTargets).toHaveBeenCalledWith(2, [{ name: '#c', isNick: false, key: '' }]);
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });

  it('renders nothing when there is no pending disambiguation', () => {
    disambig = null;
    const { container } = render(<DeepLinkDisambiguation />);
    expect(container).toBeEmptyDOMElement();
  });

  it('passes nick targets to applyDeepLinkTargets', () => {
    disambig = {
      candidates: [{ networkId: 1, name: 'Libera work' }],
      targets: [{ name: 'alice', isNick: true, key: '' }],
    };
    render(<DeepLinkDisambiguation />);
    fireEvent.click(screen.getByText('Libera work'));
    expect(applyDeepLinkTargets).toHaveBeenCalledWith(1, [{ name: 'alice', isNick: true, key: '' }]);
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });

  it('cancel button clears state without navigating', () => {
    disambig = {
      candidates: [{ networkId: 1, name: 'Libera work' }],
      targets: [{ name: '#c', isNick: false, key: '' }],
    };
    render(<DeepLinkDisambiguation />);
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(applyDeepLinkTargets).not.toHaveBeenCalled();
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(null);
  });
});
