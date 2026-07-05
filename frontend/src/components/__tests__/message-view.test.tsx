import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MessageView } from '../message-view'
import { storage } from '../../../wailsjs/go/models'
import { useNetworkStore } from '../../stores/network'

// The view pulls nickname colors and several store slices on render. Stub both so
// the tests exercise only the rendering branches, with no Wails round-trips.
vi.mock('../../hooks/useNicknameColors', () => ({
  useNicknameColors: () => new Map<string, string>(),
}))

const storeState = {
  messages: [] as storage.Message[],
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
  setAtBottom: vi.fn(),
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
  // Used by the double-click-to-DM and the shared user context menu.
  openQuery: vi.fn(),
  addMonitorNick: vi.fn(),
  setChannelContext: vi.fn(),
}

vi.mock('../../stores/network', () => {
  const useNetworkStore = (sel: (s: typeof storeState) => unknown) => sel(storeState)
  useNetworkStore.getState = () => storeState
  return { useNetworkStore }
})

// jsdom has no layout, so the real virtualizer would render zero rows. These tests
// exercise the per-row rendering branches (not scroll behavior, which is covered by
// e2e), so stub the virtualizer to render every row.
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 44,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({ key: index, index, start: index * 44, size: 44 })),
    scrollToIndex: () => {},
    scrollToEnd: () => {},
    measureElement: () => {},
  }),
}))

// The consolidate-join/quit preference now comes from the settings store. Stub it
// (default off) so these tests don't pull in the real Wails bindings.
vi.mock('../../stores/settings', () => ({
  useSettingsStore: (selector: (s: { consolidateJoinQuit: boolean }) => unknown) =>
    selector({ consolidateJoinQuit: false }),
}))

// The shared user context menu routes Whois through the app-level UI store.
const uiState = { setShowUserInfo: vi.fn(), setInviteTo: vi.fn() }
vi.mock('../../stores/ui', () => ({
  useUIStore: Object.assign(
    (sel: (s: typeof uiState) => unknown) => sel(uiState),
    { getState: () => uiState },
  ),
}))

// The menu fetches a fresh permission snapshot (self modes + capabilities) and the
// joined-channel list on open; stub both so tests stay off the Wails bridge.
vi.mock('../../../wailsjs/go/main/App', () => ({
  SendCommand: vi.fn().mockResolvedValue(undefined),
  GetChannelInfo: vi.fn().mockResolvedValue({ users: [], capabilities: null }),
  GetJoinedChannels: vi.fn().mockResolvedValue([]),
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

// MessageView now reads messages from the store (not a prop); seed the mock store
// then render.
const renderView = (
  messages: storage.Message[],
  networkId: number | null,
  selectedChannel?: string | null,
) => {
  storeState.messages = messages
  return render(<MessageView networkId={networkId} selectedChannel={selectedChannel} />)
}

describe('MessageView mention highlight', () => {
  // After a runtime /nick, "who am I" must come from the live server-assigned nick
  // (currentNick), not the stale configured network.nickname. A message mentioning
  // the new nick should highlight; the configured nick is intentionally different.
  it('highlights a mention of the live current nick, not the configured nick', () => {
    storeState.networks = [{ id: 1, nickname: 'oldnick' }]
    storeState.currentNick = { 1: 'newnick' }
    const msg = makeMessage({ id: 7, user: 'alice', message: 'hey newnick how are you', message_type: 'privmsg' })

    renderView([msg], 1, '#chan')

    expect(screen.getByTestId('message-item')).toHaveClass('cc-mention')

    storeState.networks = []
    storeState.currentNick = {}
  })
})

describe('MessageView reply quote strip', () => {
  it('renders parent nick and snippet when parent is in the loaded set', () => {
    const parent = makeMessage({ id: 1, msgid: 'p1', user: 'bob', message: 'original text here' })
    const reply = makeMessage({ id: 2, msgid: 'c1', reply_msgid: 'p1', user: 'amy', message: 'reply text' })

    renderView([parent, reply], 1, '#chan')

    // The quote strip button contains the parent nick; find it there (the parent row
    // also shows "bob" in a nick span, so we look inside the .reply-quote button)
    const quoteBtn = document.querySelector('.reply-quote')
    expect(quoteBtn).not.toBeNull()
    expect(quoteBtn!.textContent).toMatch(/bob/)
    expect(quoteBtn!.textContent).toMatch(/original text here/)
  })

  it('renders muted fallback when parent is not in the loaded set', () => {
    const reply = makeMessage({ id: 2, msgid: 'c1', reply_msgid: 'missing-parent', user: 'amy', message: 'reply text' })

    renderView([reply], 1, '#chan')

    expect(screen.getByText(/replying to an earlier message/i)).toBeInTheDocument()
  })

  it('exposes data-msgid on each message row', () => {
    const msg1 = makeMessage({ id: 1, msgid: 'abc123', user: 'alice', message: 'hello' })
    const msg2 = makeMessage({ id: 2, msgid: 'def456', user: 'bob', message: 'world' })

    renderView([msg1, msg2], 1, '#chan')

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

    renderView([msg], 1, '#chan')

    expect(screen.getByText('bot')).toBeInTheDocument()
    storeState.botNicks = {}
  })

  it('does not badge a message from a non-bot author', () => {
    storeState.botNicks = { 1: new Set(['buildbot']) }
    const msg = makeMessage({ id: 4, user: 'alice', message: 'hi', message_type: 'privmsg' })

    renderView([msg], 1, '#chan')

    expect(screen.queryByText('bot')).toBeNull()
    storeState.botNicks = {}
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

    renderView([pm], 1, 'pm:bob')

    // The pill strips the leading '#' and renders the channel name next to the Hash icon
    expect(screen.getByText('dev')).toBeInTheDocument()
    // The pill button itself should be present
    expect(document.querySelector('.channel-context-pill')).not.toBeNull()
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

    renderView([chan], 1, '#general')

    // No pill should render for a channel message
    expect(document.querySelector('.channel-context-pill')).toBeNull()
  })
})

describe('MessageView nick interactions', () => {
  it('opens a DM when the author nick is double-clicked', () => {
    storeState.openQuery = vi.fn()
    const msg = makeMessage({ id: 20, user: 'alice', message: 'hi', message_type: 'privmsg' })

    renderView([msg], 1, '#chan')

    fireEvent.doubleClick(screen.getByTestId('author-nick'))

    expect(storeState.openQuery).toHaveBeenCalledWith(1, 'alice')
  })

  it('opens the shared user context menu when the author nick is right-clicked', () => {
    const msg = makeMessage({ id: 21, user: 'alice', message: 'hi', message_type: 'privmsg' })

    renderView([msg], 1, '#chan')

    // No menu until the nick is right-clicked.
    expect(screen.queryByText('CTCP Version')).toBeNull()

    fireEvent.contextMenu(screen.getByTestId('author-nick'))

    // The same always-available entries the userlist menu shows.
    expect(screen.getByText('Whois')).toBeInTheDocument()
    expect(screen.getByText('CTCP Version')).toBeInTheDocument()
  })

  it('right-click menu offers a single Invite entry, not a per-channel list', () => {
    const msg = makeMessage({ id: 22, user: 'alice', message: 'hi', message_type: 'privmsg' })
    renderView([msg], 1, '#chan')

    fireEvent.contextMenu(screen.getByTestId('author-nick'))

    expect(screen.getByText('Invite to channel…')).toBeInTheDocument()
    // The old inline "Invite to" section header must be gone.
    expect(screen.queryByText('Invite to', { exact: true })).toBeNull()
  })
})
