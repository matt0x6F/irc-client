import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { fireEvent, waitFor } from '@testing-library/react'
import { SettingsPanel } from '../settings-panel'

// Mock the Wails bindings the panel calls on mount. Paths resolve (from this
// test file) to the same modules the component imports, so its imports get the
// mocks too.
const {
  getNetworksMock,
  listPluginsMock,
  getBuildInfoMock,
  getConnectionStatusMock,
  getServersMock,
  getLogConfigMock,
  getDefaultLogPathMock,
  getActivitySettingsMock,
  setActivitySettingsMock,
} = vi.hoisted(() => ({
  getNetworksMock: vi.fn(),
  listPluginsMock: vi.fn(),
  getBuildInfoMock: vi.fn(),
  getConnectionStatusMock: vi.fn(),
  getServersMock: vi.fn(),
  getLogConfigMock: vi.fn(),
  getDefaultLogPathMock: vi.fn(),
  getActivitySettingsMock: vi.fn(),
  setActivitySettingsMock: vi.fn(),
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
  GetPendingNetworkPrefill: vi.fn().mockResolvedValue(null),
  GetSetting: vi.fn().mockResolvedValue(''),
  SetSetting: vi.fn().mockResolvedValue(undefined),
  GetActivitySettings: getActivitySettingsMock,
  SetActivitySettings: setActivitySettingsMock,
}))

// Mock scripts-panel to avoid pulling Wails bindings + scripts store into this test.
vi.mock('../scripts-panel', () => ({ ScriptsPanel: () => <button>Open scripts folder</button> }))

describe('SettingsPanel Activity settings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getNetworksMock.mockResolvedValue([])
    listPluginsMock.mockResolvedValue([])
    getConnectionStatusMock.mockResolvedValue({})
    getServersMock.mockResolvedValue([])
    getLogConfigMock.mockResolvedValue({ enabled: false, path: '/tmp/cascade.log', level: 'info' })
    getDefaultLogPathMock.mockResolvedValue('/tmp/cascade.log')
    getBuildInfoMock.mockResolvedValue({ version: 'v1.2.3', commit: 'abc1234', buildDate: '2026-06-15T12:00:00Z' })
    getActivitySettingsMock.mockResolvedValue({
      highlights: true,
      keywords: true,
      invites: true,
      pms: true,
      keywordList: [],
    })
    setActivitySettingsMock.mockResolvedValue(undefined)
  })

  it('renders the activity toggles on the notifications section', async () => {
    render(<SettingsPanel section="notifications" onSectionChange={() => {}} />)
    expect(await screen.findByTestId('activity-toggle-highlights')).toBeInTheDocument()
    expect(await screen.findByTestId('activity-toggle-keywords')).toBeInTheDocument()
    expect(await screen.findByTestId('activity-toggle-invites')).toBeInTheDocument()
    expect(await screen.findByTestId('activity-toggle-pms')).toBeInTheDocument()
  })

  it('flips highlights to false and persists the rest unchanged', async () => {
    render(<SettingsPanel section="notifications" onSectionChange={() => {}} />)
    const toggle = await screen.findByTestId('activity-toggle-highlights')
    fireEvent.click(toggle)

    await waitFor(() => {
      expect(setActivitySettingsMock).toHaveBeenCalledWith({
        highlights: false,
        keywords: true,
        invites: true,
        pms: true,
        keywordList: [],
      })
    })
  })

  it('adds a new keyword to keywordList on Add', async () => {
    render(<SettingsPanel section="notifications" onSectionChange={() => {}} />)
    const input = await screen.findByTestId('activity-keyword-input')
    fireEvent.change(input, { target: { value: 'urgent' } })
    const addButton = await screen.findByTestId('activity-keyword-add')
    fireEvent.click(addButton)

    await waitFor(() => {
      expect(setActivitySettingsMock).toHaveBeenCalledWith({
        highlights: true,
        keywords: true,
        invites: true,
        pms: true,
        keywordList: ['urgent'],
      })
    })
  })
})
