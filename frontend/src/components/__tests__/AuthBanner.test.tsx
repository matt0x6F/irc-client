import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'

// authState is keyed by networkId; we use a mutable ref so individual tests can
// override the returned slice without re-hoisting.
let mockAuthState: Record<number, { reason: string } | undefined> = {}

vi.mock('../../stores/network', () => ({
  useNetworkStore: (selector: (s: { authState: typeof mockAuthState }) => unknown) =>
    selector({ authState: mockAuthState }),
}))

import { AuthBanner } from '../AuthBanner'

describe('AuthBanner', () => {
  const onReconnect = vi.fn()
  const onEditCredentials = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    mockAuthState = {}
  })

  it('renders nothing when there is no auth failure for the network', () => {
    const { container } = render(
      <AuthBanner networkId={1} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders the banner when authState has an entry for the network', () => {
    mockAuthState = { 1: { reason: 'Invalid password' } }
    render(
      <AuthBanner networkId={1} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText(/Authentication failed: Invalid password/i)).toBeTruthy()
  })

  it('shows the reason when provided', () => {
    mockAuthState = { 2: { reason: 'SASL auth failed' } }
    render(
      <AuthBanner networkId={2} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    expect(screen.getByText(/SASL auth failed/)).toBeTruthy()
  })

  it('shows generic message when reason is empty', () => {
    mockAuthState = { 3: { reason: '' } }
    render(
      <AuthBanner networkId={3} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    expect(screen.getByText(/Authentication failed\. You are not connected\./)).toBeTruthy()
  })

  it('calls onReconnect with networkId when Reconnect is clicked', () => {
    mockAuthState = { 1: { reason: 'bad pass' } }
    render(
      <AuthBanner networkId={1} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    fireEvent.click(screen.getByRole('button', { name: /reconnect/i }))
    expect(onReconnect).toHaveBeenCalledWith(1)
  })

  it('calls onEditCredentials with networkId when Edit credentials is clicked', () => {
    mockAuthState = { 1: { reason: 'bad pass' } }
    render(
      <AuthBanner networkId={1} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    fireEvent.click(screen.getByRole('button', { name: /edit credentials/i }))
    expect(onEditCredentials).toHaveBeenCalledWith(1)
  })

  it('does not show banner for a different networkId', () => {
    mockAuthState = { 2: { reason: 'fail' } }
    const { container } = render(
      <AuthBanner networkId={1} onReconnect={onReconnect} onEditCredentials={onEditCredentials} />
    )
    expect(container.firstChild).toBeNull()
  })
})
