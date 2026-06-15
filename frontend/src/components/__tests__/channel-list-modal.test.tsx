import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ChannelListModal, formatAgo } from '../channel-list-modal'

// Mock the Wails bindings the modal depends on. Paths resolve (from this test file)
// to the same modules the component imports, so its imports get the mocks too.
const { getCachedMock, requestListMock, sendCommandMock } = vi.hoisted(() => ({
  getCachedMock: vi.fn(),
  requestListMock: vi.fn(),
  sendCommandMock: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetCachedChannelList: getCachedMock,
  RequestChannelList: requestListMock,
  SendCommand: sendCommandMock,
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}), // returns an unsubscribe fn
}))

describe('formatAgo', () => {
  it('reports recent fetches as "just now"', () => {
    expect(formatAgo(0)).toBe('just now')
    expect(formatAgo(5_000)).toBe('just now')
  })

  it('formats seconds, minutes, hours and days', () => {
    expect(formatAgo(30_000)).toBe('30s ago')
    expect(formatAgo(3 * 60_000)).toBe('3 min ago')
    expect(formatAgo(2 * 60 * 60_000)).toBe('2h ago')
    expect(formatAgo(3 * 24 * 60 * 60_000)).toBe('3d ago')
  })
})

describe('ChannelListModal cache behavior', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    requestListMock.mockResolvedValue(undefined)
  })

  it('renders cached rows instantly and does NOT refetch when fresh', async () => {
    getCachedMock.mockResolvedValue({
      found: true,
      fetchedAt: Date.now() - 60_000, // 1 min old → fresh
      channels: [{ channel: '#fresh', users: 5, topic: 'hi', networkId: 1 }],
    })

    render(<ChannelListModal networkId={1} onClose={() => {}} />)

    expect(await screen.findByText('#fresh')).toBeInTheDocument()
    expect(requestListMock).not.toHaveBeenCalled()
  })

  it('renders cached rows AND refetches when stale', async () => {
    getCachedMock.mockResolvedValue({
      found: true,
      fetchedAt: Date.now() - 30 * 60_000, // 30 min old → stale
      channels: [{ channel: '#stale', users: 2, topic: '', networkId: 1 }],
    })

    render(<ChannelListModal networkId={1} onClose={() => {}} />)

    expect(await screen.findByText('#stale')).toBeInTheDocument()
    await waitFor(() => expect(requestListMock).toHaveBeenCalledWith(1))
  })

  it('requests a full list on cache miss', async () => {
    getCachedMock.mockResolvedValue({ found: false, fetchedAt: 0, channels: [] })

    render(<ChannelListModal networkId={1} onClose={() => {}} />)

    await waitFor(() => expect(requestListMock).toHaveBeenCalledWith(1))
    expect(screen.getByText('Loading channel list...')).toBeInTheDocument()
  })
})
