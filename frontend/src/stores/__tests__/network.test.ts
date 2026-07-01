import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useNetworkStore } from '../network';

vi.mock('../../../wailsjs/go/main/App', async (orig) => {
  const actual = await (orig() as Promise<Record<string, unknown>>);
  return { ...actual, GetConnectionStatus: vi.fn() };
});

import { GetConnectionStatus } from '../../../wailsjs/go/main/App';

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

// Guards the IRCv3 live-roster wiring (away-notify / account-notify /
// extended-join / chghost / account-tag) that the nick list dims on and the
// WHOIS panel reads. setUserMeta is fed by the 'usermeta-event' handler.
describe('network store: user meta', () => {
  const meta = (over: Partial<{ away: boolean; away_message: string; account: string; host: string; realname: string }> = {}) => ({
    away: false,
    away_message: '',
    account: '',
    host: '',
    realname: '',
    ...over,
  });

  beforeEach(() => {
    useNetworkStore.setState({ userMeta: {} });
  });

  it('setUserMeta records lowercased and getUserMeta/isAway are case-insensitive', () => {
    useNetworkStore.getState().setUserMeta(1, 'Alice', meta({ away: true, away_message: 'brb' }));
    const { getUserMeta, isAway, userMeta } = useNetworkStore.getState();
    expect(Object.keys(userMeta[1])).toEqual(['alice']);
    expect(getUserMeta(1, 'ALICE')?.away_message).toBe('brb');
    expect(isAway(1, 'alice')).toBe(true);
    expect(isAway(1, 'bob')).toBe(false);
  });

  it('isAway is false for an unknown network', () => {
    useNetworkStore.getState().setUserMeta(1, 'alice', meta({ away: true }));
    expect(useNetworkStore.getState().isAway(2, 'alice')).toBe(false);
  });

  it('setUserMeta is per-network and does not clobber other networks', () => {
    const { setUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'alice', meta({ account: 'a1' }));
    setUserMeta(2, 'bob', meta({ account: 'b2' }));
    const { userMeta } = useNetworkStore.getState();
    expect(userMeta[1]['alice'].account).toBe('a1');
    expect(userMeta[2]['bob'].account).toBe('b2');
  });

  it('setUserMeta with identical attributes is a no-op that keeps the same map reference', () => {
    const { setUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'alice', meta({ away: true, away_message: 'lunch' }));
    const first = useNetworkStore.getState().userMeta[1];
    setUserMeta(1, 'Alice', meta({ away: true, away_message: 'lunch' })); // same state
    const second = useNetworkStore.getState().userMeta[1];
    expect(second).toBe(first); // unchanged reference avoids needless re-renders
  });

  it('setUserMeta replaces the map when an attribute changes', () => {
    const { setUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'alice', meta({ away: true }));
    const first = useNetworkStore.getState().userMeta[1];
    setUserMeta(1, 'alice', meta({ away: false })); // back from away
    const second = useNetworkStore.getState().userMeta[1];
    expect(second).not.toBe(first);
    expect(useNetworkStore.getState().isAway(1, 'alice')).toBe(false);
  });
});

// The roster/presence/bot maps key by a CASEMAPPING-folded nick so the frontend
// buckets identities the SAME way the backend does. On an rfc1459 network the
// bytes []\~ fold to {}|^, so "Nick[a]" and "nick{a}" are one user; plain
// toLowerCase() would split them. See lib/casefold.ts.
describe('network store: CASEMAPPING-aware nick folding', () => {
  const meta = (over: Partial<{ away: boolean; account: string }> = {}) => ({
    away: false,
    away_message: '',
    account: '',
    host: '',
    realname: '',
    ...over,
  });

  beforeEach(() => {
    useNetworkStore.setState({ userMeta: {}, botNicks: {}, presence: {}, caseMapping: {} });
  });

  it('rfc1459: userMeta for Nick[a] resolves for the equivalent nick{a}', () => {
    useNetworkStore.setState({ caseMapping: { 1: 'rfc1459' } });
    const { setUserMeta, getUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'Nick[a]', meta({ account: 'acct' }));
    expect(Object.keys(useNetworkStore.getState().userMeta[1])).toEqual(['nick{a}']);
    expect(getUserMeta(1, 'nick{a}')?.account).toBe('acct');
  });

  it('rfc1459 is the default when no CASEMAPPING is cached yet', () => {
    const { setUserMeta, getUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'Nick[a]', meta({ account: 'acct' }));
    expect(getUserMeta(1, 'nick{a}')?.account).toBe('acct');
  });

  it('ascii: brackets stay distinct (mapping is respected)', () => {
    useNetworkStore.setState({ caseMapping: { 1: 'ascii' } });
    const { setUserMeta, getUserMeta } = useNetworkStore.getState();
    setUserMeta(1, 'Nick[a]', meta({ account: 'acct' }));
    expect(getUserMeta(1, 'nick{a}')).toBeUndefined();
    expect(getUserMeta(1, 'NICK[A]')?.account).toBe('acct'); // ASCII case still folds
  });

  it('isBot folds bot nicks under rfc1459', () => {
    useNetworkStore.setState({ caseMapping: { 1: 'rfc1459' } });
    const { addBot, isBot } = useNetworkStore.getState();
    addBot(1, 'Bot[x]');
    expect(isBot(1, 'bot{x}')).toBe(true);
  });

  it('setPresence folds nicks under rfc1459 (\\ -> |)', () => {
    useNetworkStore.setState({ caseMapping: { 1: 'rfc1459' } });
    useNetworkStore.getState().setPresence(1, 'Bud\\dy', true);
    expect(useNetworkStore.getState().presence[1]['bud|dy']).toBe(true);
  });
});

describe('network store: connectionStatus ordering', () => {
  beforeEach(() => {
    useNetworkStore.setState({ connectionStatus: {}, connectionStatusAt: {} });
  });

  it('applies a newer event and ignores an older one (out-of-order delivery)', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 2000);   // newer: connected
    setConnectionStatus(1, false, 1000);  // older: must be ignored
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(true);
  });

  it('an untimestamped update (poll) always wins', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 5000);
    setConnectionStatus(1, false);        // poll, no timestamp -> authoritative
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(false);
  });

  it('is per-network', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 2000);
    setConnectionStatus(2, false, 1000);
    expect(useNetworkStore.getState().connectionStatus).toEqual({ 1: true, 2: false });
  });

  it('applies an equal-timestamp update (idempotent, not dropped)', () => {
    const { setConnectionStatus } = useNetworkStore.getState();
    setConnectionStatus(1, true, 3000);
    setConnectionStatus(1, false, 3000);  // equal ts -> applied (>= rule)
    expect(useNetworkStore.getState().connectionStatus[1]).toBe(false);
  });
});

describe('network store: refreshAllConnectionStatus', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      networks: [
        { id: 1, name: 'A' } as any,
        { id: 2, name: 'B' } as any,
      ],
      connectionStatus: {},
      connectionStatusAt: {},
    });
    (GetConnectionStatus as any).mockReset();
  });

  it('writes a status for every known network', async () => {
    (GetConnectionStatus as any).mockImplementation((id: number) =>
      Promise.resolve(id === 1),
    );
    await useNetworkStore.getState().refreshAllConnectionStatus();
    expect(useNetworkStore.getState().connectionStatus).toEqual({ 1: true, 2: false });
    expect(useNetworkStore.getState().connectionStatusAt).toEqual({});
  });

  it('treats a failing GetConnectionStatus as disconnected', async () => {
    (GetConnectionStatus as any).mockImplementation((id: number) =>
      id === 1 ? Promise.reject(new Error('boom')) : Promise.resolve(true),
    );
    await useNetworkStore.getState().refreshAllConnectionStatus();
    expect(useNetworkStore.getState().connectionStatus).toEqual({ 1: false, 2: true });
    expect(useNetworkStore.getState().connectionStatusAt).toEqual({});
  });
});
