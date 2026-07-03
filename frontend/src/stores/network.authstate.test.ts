import { describe, it, expect, beforeEach } from 'vitest';
import { useNetworkStore } from './network';

describe('authState slice', () => {
  beforeEach(() => {
    useNetworkStore.setState({ authState: {}, connectionStatus: {} });
  });

  it('records an auth failure for a network', () => {
    useNetworkStore.getState().setAuthFailed(1, 'invalid credentials');
    expect(useNetworkStore.getState().authState[1]).toEqual({ reason: 'invalid credentials' });
  });

  it('clears an auth failure (e.g. on successful reconnect)', () => {
    useNetworkStore.getState().setAuthFailed(1, 'invalid credentials');
    useNetworkStore.getState().clearAuthFailed(1);
    expect(useNetworkStore.getState().authState[1]).toBeUndefined();
  });

  it('keeps per-network isolation', () => {
    useNetworkStore.getState().setAuthFailed(1, 'a');
    useNetworkStore.getState().setAuthFailed(2, 'b');
    useNetworkStore.getState().clearAuthFailed(1);
    expect(useNetworkStore.getState().authState[1]).toBeUndefined();
    expect(useNetworkStore.getState().authState[2]).toEqual({ reason: 'b' });
  });

  it('clears a stale connected flag so reconnect is not silently blocked', () => {
    // A network that was connected earlier this session leaves connectionStatus
    // true. An auth failure means the session dropped, so the flag must be
    // cleared — otherwise connectNetwork's `if (connectionStatus[id]) return`
    // guard silently refuses to redial from the auth banner (reconnect "does
    // nothing" until an app restart).
    useNetworkStore.setState({ connectionStatus: { 1: true } });
    useNetworkStore.getState().setAuthFailed(1, 'invalid credentials');
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(false);
  });
});
