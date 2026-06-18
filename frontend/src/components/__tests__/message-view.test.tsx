import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MessageView } from '../message-view'
import { storage } from '../../../wailsjs/go/models'

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
}

vi.mock('../../stores/network', () => ({
  useNetworkStore: (selector: (s: typeof storeState) => unknown) => selector(storeState),
}))

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
