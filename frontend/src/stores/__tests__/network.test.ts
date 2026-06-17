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

// Guards the IRCv3 bot-mode wiring that the chat rows, nick list, and WHOIS
// panel read. addBot is fed by the 'bot-event' handler; isBot is what the
// badges query. Reducers are pure, so no Wails bindings need mocking.
describe('network store: bot mode', () => {
  beforeEach(() => {
    useNetworkStore.setState({ botNicks: {} });
  });

  it('addBot records the nick lowercased and isBot is case-insensitive', () => {
    useNetworkStore.getState().addBot(1, 'BuildBot');
    const { isBot, botNicks } = useNetworkStore.getState();
    expect([...botNicks[1]]).toEqual(['buildbot']);
    expect(isBot(1, 'buildbot')).toBe(true);
    expect(isBot(1, 'BUILDBOT')).toBe(true);
    expect(isBot(1, 'someone')).toBe(false);
  });

  it('isBot is false for an unknown network', () => {
    useNetworkStore.getState().addBot(1, 'bot');
    expect(useNetworkStore.getState().isBot(2, 'bot')).toBe(false);
  });

  it('addBot is per-network and does not clobber other networks', () => {
    const { addBot } = useNetworkStore.getState();
    addBot(1, 'bot1');
    addBot(2, 'bot2');
    const { botNicks } = useNetworkStore.getState();
    expect([...botNicks[1]]).toEqual(['bot1']);
    expect([...botNicks[2]]).toEqual(['bot2']);
  });

  it('addBot on an already-known nick is a no-op that keeps the same Set reference', () => {
    const { addBot } = useNetworkStore.getState();
    addBot(1, 'Bot');
    const first = useNetworkStore.getState().botNicks[1];
    addBot(1, 'bot'); // duplicate (case-folded)
    const second = useNetworkStore.getState().botNicks[1];
    expect(second).toBe(first); // unchanged reference avoids needless re-renders
    expect([...second]).toEqual(['bot']);
  });
});
