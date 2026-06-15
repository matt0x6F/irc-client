import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SettingsModal } from '../settings-modal'

// Mock the Wails bindings the modal calls on mount. Paths resolve (from this
// test file) to the same modules the component imports, so its imports get the
// mocks too. The models module is left real — the component uses
// main.NetworkConfig.createFrom() during render.
const {
  getNetworksMock,
  listPluginsMock,
  getBuildInfoMock,
  getConnectionStatusMock,
  getServersMock,
} = vi.hoisted(() => ({
  getNetworksMock: vi.fn(),
  listPluginsMock: vi.fn(),
  getBuildInfoMock: vi.fn(),
  getConnectionStatusMock: vi.fn(),
  getServersMock: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetNetworks: getNetworksMock,
  ListPlugins: listPluginsMock,
  GetBuildInfo: getBuildInfoMock,
  GetConnectionStatus: getConnectionStatusMock,
  GetServers: getServersMock,
  SaveNetwork: vi.fn(),
  ConnectNetwork: vi.fn(),
  DeleteNetwork: vi.fn(),
  DisconnectNetwork: vi.fn(),
  EnablePlugin: vi.fn(),
  DisablePlugin: vi.fn(),
  ReloadPlugin: vi.fn(),
}))

describe('SettingsModal About pane', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([])
    listPluginsMock.mockResolvedValue([])
    getConnectionStatusMock.mockResolvedValue({})
    getServersMock.mockResolvedValue([])
    getBuildInfoMock.mockResolvedValue({
      version: 'v1.2.3',
      commit: 'abc1234',
      buildDate: '2026-06-15T12:00:00Z',
    })
  })

  it('renders version, commit, and build date from GetBuildInfo', async () => {
    render(<SettingsModal onClose={() => {}} initialSection="about" />)

    expect(await screen.findByTestId('about-version')).toHaveTextContent('v1.2.3')
    expect(screen.getByTestId('about-commit')).toHaveTextContent('abc1234')
    expect(screen.getByTestId('about-build-date')).toHaveTextContent('2026-06-15T12:00:00Z')
    expect(getBuildInfoMock).toHaveBeenCalledOnce()
  })

  it('links to the GitHub repository', async () => {
    render(<SettingsModal onClose={() => {}} initialSection="about" />)

    const link = await screen.findByRole('link', { name: /view on github/i })
    expect(link).toHaveAttribute('href', 'https://github.com/matt0x6F/irc-client')
  })
})
