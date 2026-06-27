// frontend/src/components/__tests__/scripts-panel.test.tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

const fetch = vi.fn(() => Promise.resolve());
const enable = vi.fn(() => Promise.resolve());
const disable = vi.fn(() => Promise.resolve());
const reload = vi.fn(() => Promise.resolve());
const create = vi.fn(() => Promise.resolve());
const openDir = vi.fn(() => Promise.resolve());
let state: Record<string, unknown>;

vi.mock('../../stores/scripts', () => ({
  useScriptsStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ ...state, fetch, enable, disable, reload, create, openDir, clearCreated: vi.fn() }),
}));

import { ScriptsPanel } from '../scripts-panel';

const row = (over: Record<string, unknown> = {}) => ({
  id: 'greeter', name: 'greeter', description: 'says hi', status: 'loaded',
  enabled: true, error: '', perms: [], ...over,
});

describe('ScriptsPanel', () => {
  beforeEach(() => {
    fetch.mockClear(); enable.mockClear(); disable.mockClear();
    reload.mockClear(); create.mockClear(); openDir.mockClear();
    state = { scripts: [], loading: false, busy: new Set(), lastCreatedPath: null, error: null };
  });

  it('fetches on mount', () => {
    render(<ScriptsPanel />);
    expect(fetch).toHaveBeenCalled();
  });

  it('renders an empty state when there are no scripts', () => {
    render(<ScriptsPanel />);
    expect(screen.getByText(/no scripts/i)).toBeInTheDocument();
  });

  it('does not show empty-state while loading', () => {
    state.loading = true;
    render(<ScriptsPanel />);
    expect(screen.queryByText(/no scripts/i)).not.toBeInTheDocument();
  });

  it('renders a script row with its name and status', () => {
    state.scripts = [row()];
    render(<ScriptsPanel />);
    expect(screen.getByText('greeter')).toBeInTheDocument();
    expect(screen.getByText(/loaded/i)).toBeInTheDocument();
  });

  it('shows the error text and a Runaway badge for a runaway script', () => {
    state.scripts = [row({ status: 'runaway', enabled: false, error: 'dispatch exceeded 1s deadline' })];
    render(<ScriptsPanel />);
    expect(screen.getByText(/runaway/i)).toBeInTheDocument();
    expect(screen.getByText(/dispatch exceeded 1s deadline/i)).toBeInTheDocument();
  });

  it('disables an enabled script when its toggle is clicked', () => {
    state.scripts = [row({ enabled: true })];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /disable/i }));
    expect(disable).toHaveBeenCalledWith('greeter');
  });

  it('enables a disabled script when its toggle is clicked', () => {
    state.scripts = [row({ enabled: false, status: 'disabled' })];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /enable/i }));
    expect(enable).toHaveBeenCalledWith('greeter');
  });

  it('reloads a script when its Reload button is clicked', () => {
    state.scripts = [row()];
    render(<ScriptsPanel />);
    fireEvent.click(screen.getByRole('button', { name: /reload/i }));
    expect(reload).toHaveBeenCalledWith('greeter');
  });

  it('creates a script from the name input', () => {
    render(<ScriptsPanel />);
    fireEvent.change(screen.getByPlaceholderText(/script name/i), { target: { value: 'mybot' } });
    fireEvent.click(screen.getByRole('button', { name: /^new script$/i }));
    expect(create).toHaveBeenCalledWith('mybot');
  });

  it('shows the created path with a Reveal button after creation', () => {
    state.lastCreatedPath = '/scripts/mybot/mybot.go';
    render(<ScriptsPanel />);
    expect(screen.getByText('/scripts/mybot/mybot.go')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /reveal in folder/i }));
    expect(openDir).toHaveBeenCalled();
  });
});
