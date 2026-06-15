import { describe, it, expect, beforeEach } from 'vitest';
import { useNetworkStore } from '../network';

// Guards the currentNick store wiring that the header reads and the
// 'current-nick' event handler writes. The reducer is pure, so no Wails
// bindings need mocking here.
describe('network store: currentNick', () => {
  beforeEach(() => {
    useNetworkStore.setState({ currentNick: {} });
  });

  it('setCurrentNick records the server-assigned nick for a network', () => {
    useNetworkStore.getState().setCurrentNick(1, 'matt0x6f_0');
    expect(useNetworkStore.getState().currentNick[1]).toBe('matt0x6f_0');
  });

  it('setCurrentNick is per-network and does not clobber other networks', () => {
    const { setCurrentNick } = useNetworkStore.getState();
    setCurrentNick(1, 'matt0x6f_0');
    setCurrentNick(2, 'someoneelse');
    setCurrentNick(1, 'matt0x6f'); // network 1 reclaimed its preferred nick
    expect(useNetworkStore.getState().currentNick).toEqual({ 1: 'matt0x6f', 2: 'someoneelse' });
  });
});
