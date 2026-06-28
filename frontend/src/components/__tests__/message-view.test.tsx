import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MessageView } from '../message-view'
import { storage } from '../../../wailsjs/go/models'
import { useNetworkStore } from '../../stores/network'

// The view pulls nickname colors and several store slices on render. Stub both so
// the test exercises only the marker-rendering branch, with no Wails round-trips.
vi.mock('../../hooks/useNicknameColors', () => ({
  useNicknameColors: () => new Map<string, string>(),
}))

const storeState = {
  networks: [] as Array<{ id: number; nickname: string }>,
  currentNick: {} as Record<number, string>,
  botNicks: {} as Record<number, Set<string>>,
  pinnedMessages: [],
  pinMessage: vi.fn(),
  unpinMessage: vi.fn(),
  viewMode: 'live',
  anchoredMessageId: null,
  clearAnchorFlash: vi.fn(),
  returnToLive: vi.fn(),
  newSinceAnchor: 0,
  loadOlderMessages: vi.fn(),
  loadNewerMessages: vi.fn(),
  loadingHistory: false,
  openOrJoinChannel: vi.fn().mockResolvedValue(undefined),
  selectPane: vi.fn(),
  setReplyTarget: vi.fn(),
  pendingScrollMsgid: null as string | null,
  clearPendingScrollMsgid: vi.fn(),
  openParentMessage: vi.fn(),
}

vi.mock('../../stores/network', () => {
  const useNetworkStore = (sel: (s: typeof storeState) => unknown) => sel(storeState)
  useNetworkStore.getState = () => storeState
  return { useNetworkStore }
})

// The consolidate-join/quit preference now comes from the settings store. Stub it
// (default off) so these marker tests don't pull in the real Wails bindings.
vi.mock('../../stores/settings', () => ({
  useSettingsStore: (selector: (s: { consolidateJoinQuit: boolean }) => unknown) =>
    selector({ consolidateJoinQuit: false }),
}))

const makeMessage = (overrides: Partial<storage.Message>) =>
  storage.Message.createFrom({
    id: 1,
    network_id: 1,
    channel_id: 5,
    user: '*',
    message: '',
    message_type: 'privmsg',
    timestamp: '2026-06-15T14:35:02Z',
    raw_line: '',
    pm_target: '',
    msgid: '',
    reply_msgid: '',
    channel_context: '',
    ...overrides,
  })

describe('MessageView connection markers', () => {
  it('renders a marker message as a divider, not a normal/pinnable row', () => {
    const marker = makeMessage({ id: 1, message: 'Reconnected', message_type: 'marker' })

    render(<MessageView messages={[marker]} networkId={1} selectedChannel="#chan" />)

    const divider = screen.getByTestId('connection-marker')
    expect(divider).toHaveTextContent('Reconnected')
    // A marker is its own row type — never the standard message row, never pinnable.
    expect(screen.queryByTestId('message-item')).toBeNull()
    expect(screen.queryByTestId('pin-button')).toBeNull()
  })

  it('renders markers and regular messages distinctly side by side', () => {
    const marker = makeMessage({ id: 1, message: 'Disconnected', message_type: 'marker' })
    const regular = makeMessage({ id: 2, user: 'alice', message: 'hello', message_type: 'privmsg' })

    render(<MessageView messages={[marker, regular]} networkId={1} selectedChannel="#chan" />)

    expect(screen.getByTestId('connection-marker')).toHaveTextContent('Disconnected')
    // Only the regular privmsg is a standard, pinnable row.
    expect(screen.getAllByTestId('message-item')).toHaveLength(1)
    expect(screen.getAllByTestId('pin-button')).toHaveLength(1)
  })
})

describe('MessageView mention highlight', () => {
  // After a runtime /nick, "who am I" must come from the live server-assigned nick
  // (currentNick), not the stale configured network.nickname. A message mentioning
  // the new nick should highlight; the configured nick is intentionally different.
  it('highlights a mention of the live current nick, not the configured nick', () => {
    storeState.networks = [{ id: 1, nickname: 'oldnick' }]
    storeState.currentNick = { 1: 'newnick' }
    const msg = makeMessage({ id: 7, user: 'alice', message: 'hey newnick how are you', message_type: 'privmsg' })

    render(<MessageView messages={[msg]} networkId={1} selectedChannel="#chan" />)

    expect(screen.getByTestId('message-item')).toHaveClass('cc-mention')

    storeState.networks = []
    storeState.currentNick = {}
  })
})

describe('MessageView reply quote strip', () => {
  it('renders parent nick and snippet when parent is in the loaded set', () => {
    const parent = makeMessage({ id: 1, msgid: 'p1', user: 'bob', message: 'original text here' })
    const reply = makeMessage({ id: 2, msgid: 'c1', reply_msgid: 'p1', user: 'amy', message: 'reply text' })

    render(<MessageView messages={[parent, reply]} networkId={1} selectedChannel="#chan" />)

    // The quote strip button contains the parent nick; find it there (the parent row
    // also shows "bob" in a nick span, so we look inside the .reply-quote button)
    const quoteBtn = document.querySelector('.reply-quote')
    expect(quoteBtn).not.toBeNull()
    expect(quoteBtn!.textContent).toMatch(/bob/)
    expect(quoteBtn!.textContent).toMatch(/original text here/)
  })

  it('renders muted fallback when parent is not in the loaded set', () => {
    const reply = makeMessage({ id: 2, msgid: 'c1', reply_msgid: 'missing-parent', user: 'amy', message: 'reply text' })

    render(<MessageView messages={[reply]} networkId={1} selectedChannel="#chan" />)

    expect(screen.getByText(/replying to an earlier message/i)).toBeInTheDocument()
  })

  it('exposes data-msgid on each message row', () => {
    const msg1 = makeMessage({ id: 1, msgid: 'abc123', user: 'alice', message: 'hello' })
    const msg2 = makeMessage({ id: 2, msgid: 'def456', user: 'bob', message: 'world' })

    render(<MessageView messages={[msg1, msg2]} networkId={1} selectedChannel="#chan" />)

    const rows = screen.getAllByTestId('message-item')
    const dataMsgids = rows.map((r) => r.getAttribute('data-msgid')).filter(Boolean)
    expect(dataMsgids).toContain('abc123')
    expect(dataMsgids).toContain('def456')
  })
})

describe('MessageView bot badge', () => {
  it('badges a message whose author is a recognized bot (case-insensitive)', () => {
    storeState.botNicks = { 1: new Set(['buildbot']) }
    const msg = makeMessage({ id: 3, user: 'BuildBot', message: 'build passed', message_type: 'privmsg' })

    render(<MessageView messages={[msg]} networkId={1} selectedChannel="#chan" />)

    expect(screen.getByText('bot')).toBeInTheDocument()
    storeState.botNicks = {}
  })

  it('does not badge a message from a non-bot author', () => {
    storeState.botNicks = { 1: new Set(['buildbot']) }
    const msg = makeMessage({ id: 4, user: 'alice', message: 'hi', message_type: 'privmsg' })

    render(<MessageView messages={[msg]} networkId={1} selectedChannel="#chan" />)

    expect(screen.queryByText('bot')).toBeNull()
    storeState.botNicks = {}
  })
})

describe('MessageView invite-line channel links', () => {
  it('invite-line channel click calls openOrJoinChannel', () => {
    const spy = vi
      .spyOn(useNetworkStore.getState(), 'openOrJoinChannel')
      .mockResolvedValue(undefined)

    // renderInviteText splits on /(\s[#&]\S+)/g — the channel must be preceded by a space.
    const msg = makeMessage({
      id: 10,
      user: '*',
      message: 'You have been invited to #welcome',
      message_type: 'invite',
    })

    render(<MessageView messages={[msg]} networkId={3} selectedChannel="#chan" />)

    fireEvent.click(screen.getByRole('button', { name: '#welcome' }))
    expect(spy).toHaveBeenCalledWith(3, '#welcome')
    spy.mockRestore()
  })
})

describe('MessageView channel-context pill', () => {
  it('renders an in-#channel pill on a PM with channel_context', () => {
    const pm = makeMessage({
      id: 10,
      channel_id: null,
      user: 'bob',
      message: 'see chan',
      message_type: 'privmsg',
      pm_target: 'bob',
      channel_context: '#dev',
      msgid: 'pm1',
      reply_msgid: '',
    })

    render(<MessageView messages={[pm]} networkId={1} selectedChannel="pm:bob" />)

    expect(screen.getByText('#dev')).toBeInTheDocument()
  })

  it('does not render the pill on a channel message even if channel_context is set', () => {
    const chan = makeMessage({
      id: 11,
      channel_id: 5,
      user: 'alice',
      message: 'hello',
      message_type: 'privmsg',
      channel_context: '#dev',
      msgid: 'ch1',
      reply_msgid: '',
    })

    render(<MessageView messages={[chan]} networkId={1} selectedChannel="#general" />)

    expect(screen.queryByText('#dev')).toBeNull()
  })
})
