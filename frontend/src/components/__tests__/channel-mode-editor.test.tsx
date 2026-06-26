import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ChannelModeEditor } from '../channel-mode-editor'

// Mock the Wails bindings the editor calls on mount. The resolver (lib/chanmodes)
// and models are left real — this test exercises the real label wiring end to end.
const { sendCommandMock, getNetworksMock, requestChannelBansMock, eventsOnMock } = vi.hoisted(() => ({
  sendCommandMock: vi.fn(),
  getNetworksMock: vi.fn(),
  requestChannelBansMock: vi.fn(),
  eventsOnMock: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  SendCommand: sendCommandMock,
  GetNetworks: getNetworksMock,
  RequestChannelBans: requestChannelBansMock,
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: eventsOnMock,
}))

// A Libera/Solanum-shaped capabilities object, including an unknown flag letter "X".
const solanumCaps = {
  software_family: 'solanum',
  chanmodes_a: 'eIbq',
  chanmodes_b: 'k',
  chanmodes_c: 'flj',
  chanmodes_d: 'CFLMPQcgimnprstuzX',
  extban_prefix: '$',
  extban_types: 'acjrx',
} as never

describe('ChannelModeEditor mode labels', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([])
    requestChannelBansMock.mockResolvedValue(undefined)
    eventsOnMock.mockReturnValue(() => {})
  })

  it('renders server-family-accurate labels and a typed fallback for unknown letters', async () => {
    render(
      <ChannelModeEditor
        networkId={1}
        channelName="#cascade-irc"
        currentModes="+nt"
        capabilities={solanumCaps}
        onClose={() => {}}
        onUpdate={() => {}}
      />,
    )

    // Solanum-specific meaning, not the classic "Private".
    expect(await screen.findByText('No KNOCK')).toBeInTheDocument()
    // Extended letter that used to render the meaningless "Mode +Q".
    expect(screen.getByText('No forwarding into here')).toBeInTheDocument()
    // Unknown letters (e.g. +X) degrade to a typed generic label, never a confident
    // wrong one. The map intentionally omits letters it isn't sure of, so several may
    // share this fallback.
    expect(screen.getAllByText('Flag').length).toBeGreaterThan(0)
  })
})
