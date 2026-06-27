import { describe, it, expect, beforeEach } from 'vitest';
import { useNetworkStore } from './network';

describe('authState slice', () => {
  beforeEach(() => {
    useNetworkStore.setState({ authState: {} });
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
});
