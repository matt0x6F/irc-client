import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { fireEvent, waitFor } from '@testing-library/react'
import { SettingsPanel } from '../settings-panel'

// Mock the Wails bindings the panel calls on mount. Paths resolve (from this
// test file) to the same modules the component imports, so its imports get the
// mocks too. The models module is left real — the component uses
// main.NetworkConfig.createFrom() during render.
const {
  getNetworksMock,
  listPluginsMock,
  getBuildInfoMock,
  getConnectionStatusMock,
  getServersMock,
  getLogConfigMock,
  getDefaultLogPathMock,
} = vi.hoisted(() => ({
  getNetworksMock: vi.fn(),
  listPluginsMock: vi.fn(),
  getBuildInfoMock: vi.fn(),
  getConnectionStatusMock: vi.fn(),
  getServersMock: vi.fn(),
  getLogConfigMock: vi.fn(),
  getDefaultLogPathMock: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetNetworks: getNetworksMock,
  ListPlugins: listPluginsMock,
  GetBuildInfo: getBuildInfoMock,
  GetConnectionStatus: getConnectionStatusMock,
  GetServers: getServersMock,
  GetLogConfig: getLogConfigMock,
  GetDefaultLogPath: getDefaultLogPathMock,
  SetLogConfig: vi.fn(),
  SaveNetwork: vi.fn(),
  ConnectNetwork: vi.fn(),
  DeleteNetwork: vi.fn(),
  DisconnectNetwork: vi.fn(),
  EnablePlugin: vi.fn(),
  DisablePlugin: vi.fn(),
  ReloadPlugin: vi.fn(),
  CheckForUpdates: vi.fn(),
  GetSTSPolicies: vi.fn().mockResolvedValue([]),
  ClearSTSPolicy: vi.fn(),
  RequestNotificationPermission: vi.fn().mockResolvedValue(true),
}))

// Mock scripts-panel to avoid pulling Wails bindings + scripts store into this test.
vi.mock('../scripts-panel', () => ({ ScriptsPanel: () => <button>Open scripts folder</button> }))

describe('SettingsPanel Scripts pane', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([])
    listPluginsMock.mockResolvedValue([])
    getConnectionStatusMock.mockResolvedValue({})
    getServersMock.mockResolvedValue([])
    getLogConfigMock.mockResolvedValue({ enabled: false, path: '/tmp/cascade.log', level: 'info' })
    getDefaultLogPathMock.mockResolvedValue('/tmp/cascade.log')
    getBuildInfoMock.mockResolvedValue({ version: 'v1.2.3', commit: 'abc1234', buildDate: '2026-06-15T12:00:00Z' })
  })

  it('renders the Scripts nav item', async () => {
    render(<SettingsPanel section="networks" onSectionChange={() => {}} />)
    expect(await screen.findByRole('button', { name: /^Scripts$/i })).toBeInTheDocument()
  })

  it('renders ScriptsPanel when section is scripts', async () => {
    render(<SettingsPanel section="scripts" onSectionChange={() => {}} />)
    expect(await screen.findByRole('button', { name: /open scripts folder/i })).toBeInTheDocument()
  })
})

describe('SettingsPanel Networks master-detail', () => {
  const network = {
    id: 1, name: 'ErgoIRC', address: 'irc.ergo.chat', port: 6697, tls: true,
    nickname: 'matt', username: 'matt', realname: 'matt', password: '',
    sasl_enabled: false, sasl_mechanism: '', sasl_username: '', sasl_password: '',
    sasl_external_cert: '', auto_connect: false, identify_as_bot: false,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([network])
    listPluginsMock.mockResolvedValue([])
    getConnectionStatusMock.mockResolvedValue({ 1: false })
    getServersMock.mockResolvedValue([
      { id: 10, network_id: 1, address: 'irc.ergo.chat', port: 6697, tls: true, order: 0, created_at: '' },
    ])
    getLogConfigMock.mockResolvedValue({ enabled: false, path: '/tmp/c.log', level: 'info' })
    getDefaultLogPathMock.mockResolvedValue('/tmp/c.log')
    getBuildInfoMock.mockResolvedValue({ version: 'v1', commit: 'abc', buildDate: '2026-01-01T00:00:00Z' })
  })

  it('shows the list and hides it once the editor opens', async () => {
    render(<SettingsPanel section="networks" onSectionChange={() => {}} />)
    // List view present, editor absent.
    const addBtn = await screen.findByTestId('add-network-button')
    expect(screen.queryByTestId('network-editor')).not.toBeInTheDocument()
    // Enter the editor via "Add network".
    fireEvent.click(addBtn)
    await waitFor(() => expect(screen.getByTestId('network-editor')).toBeInTheDocument())
    // The list view (and its Add button) is gone — not stacked behind the form.
    expect(screen.queryByTestId('add-network-button')).not.toBeInTheDocument()
    expect(screen.queryByTestId('network-list')).not.toBeInTheDocument()
  })
})

describe('SettingsPanel About pane', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([])
    listPluginsMock.mockResolvedValue([])
    getConnectionStatusMock.mockResolvedValue({})
    getServersMock.mockResolvedValue([])
    getLogConfigMock.mockResolvedValue({ enabled: false, path: '/tmp/cascade.log', level: 'info' })
    getDefaultLogPathMock.mockResolvedValue('/tmp/cascade.log')
    getBuildInfoMock.mockResolvedValue({
      version: 'v1.2.3',
      commit: 'abc1234',
      buildDate: '2026-06-15T12:00:00Z',
    })
  })

  it('renders version, commit, and build date from GetBuildInfo', async () => {
    render(<SettingsPanel section="about" onSectionChange={() => {}} />)

    expect(await screen.findByTestId('about-version')).toHaveTextContent('v1.2.3')
    expect(screen.getByTestId('about-commit')).toHaveTextContent('abc1234')
    expect(screen.getByTestId('about-build-date')).toHaveTextContent('2026-06-15T12:00:00Z')
    expect(getBuildInfoMock).toHaveBeenCalledOnce()
  })

  it('links to the GitHub repository', async () => {
    render(<SettingsPanel section="about" onSectionChange={() => {}} />)

    const link = await screen.findByRole('link', { name: /view on github/i })
    expect(link).toHaveAttribute('href', 'https://github.com/matt0x6F/irc-client')
  })
})
