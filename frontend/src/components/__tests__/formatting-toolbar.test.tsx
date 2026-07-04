import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { InputArea } from '../input-area'
import { usePreferencesStore } from '../../stores/preferences'

const noop = async () => {}

beforeEach(() => {
  // The preferences store is a module singleton — reset it so tests don't leak.
  localStorage.clear()
  usePreferencesStore.setState({ showFormattingToolbar: true })
})

describe('composer formatting toolbar', () => {
  it('wraps the current selection in bold markup when Bold is clicked', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)
    const input = screen.getByTestId('message-input') as HTMLInputElement

    fireEvent.change(input, { target: { value: 'team' } })
    input.setSelectionRange(0, 4)

    fireEvent.click(screen.getByTitle('Bold (Ctrl/Cmd+B)'))

    expect(input.value).toBe('*team*')
  })

  it('wraps the current selection in monospace backticks when Monospace is clicked', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)
    const input = screen.getByTestId('message-input') as HTMLInputElement

    fireEvent.change(input, { target: { value: 'code' } })
    input.setSelectionRange(0, 4)

    fireEvent.click(screen.getByTitle('Monospace (Ctrl/Cmd+E)'))

    expect(input.value).toBe('`code`')
  })

  it('wraps the current selection in backticks on Ctrl/Cmd+E', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)
    const input = screen.getByTestId('message-input') as HTMLInputElement

    fireEvent.change(input, { target: { value: 'code' } })
    input.setSelectionRange(0, 4)

    fireEvent.keyDown(input, { key: 'e', ctrlKey: true })

    expect(input.value).toBe('`code`')
  })

  it('renders a live preview only once the text contains markup', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)
    const input = screen.getByTestId('message-input') as HTMLInputElement

    fireEvent.change(input, { target: { value: 'plain text' } })
    expect(screen.queryByTestId('message-preview')).toBeNull()

    fireEvent.change(input, { target: { value: '*bold*' } })
    expect(screen.getByTestId('message-preview')).toBeInTheDocument()
  })

  it('hides the formatting strip when the Aa toggle is turned off, keeping emoji/mention', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)

    expect(screen.getByTitle('Bold (Ctrl/Cmd+B)')).toBeInTheDocument()

    fireEvent.click(screen.getByTitle('Hide formatting toolbar'))

    expect(screen.queryByTitle('Bold (Ctrl/Cmd+B)')).toBeNull()
    // Emoji and mention stay pinned regardless of the toggle.
    expect(screen.getByTitle('Emoji')).toBeInTheDocument()
    expect(screen.getByTitle('Mention a member')).toBeInTheDocument()
  })
})
