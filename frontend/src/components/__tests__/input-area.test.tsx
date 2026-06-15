import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { InputArea } from '../input-area'

const noop = async () => {}

describe('InputArea auto-focus on buffer switch', () => {
  it('focuses the message input on mount', () => {
    render(<InputArea onSendMessage={noop} networkId={1} channelName="#foo" />)
    expect(screen.getByTestId('message-input')).toHaveFocus()
  })

  it('re-focuses the input when switching to a different channel', () => {
    const { rerender } = render(
      <InputArea onSendMessage={noop} networkId={1} channelName="#foo" />
    )
    const input = screen.getByTestId('message-input') as HTMLInputElement

    // Simulate the user moving focus elsewhere (e.g. clicking the channel tree).
    input.blur()
    expect(input).not.toHaveFocus()

    // Switching buffers changes the channelName prop and should refocus.
    rerender(<InputArea onSendMessage={noop} networkId={1} channelName="#bar" />)
    expect(input).toHaveFocus()
  })

  it('re-focuses the input when switching to a PM buffer', () => {
    const { rerender } = render(
      <InputArea onSendMessage={noop} networkId={1} channelName="#foo" />
    )
    const input = screen.getByTestId('message-input') as HTMLInputElement
    input.blur()

    rerender(<InputArea onSendMessage={noop} networkId={1} channelName="pm:alice" />)
    expect(input).toHaveFocus()
  })

  it('does not steal focus from another editable field (e.g. a modal search box)', () => {
    const { rerender } = render(
      <InputArea onSendMessage={noop} networkId={1} channelName="#foo" />
    )

    // Stand in for a modal's text field that the user is actively typing in.
    const modalInput = document.createElement('input')
    document.body.appendChild(modalInput)
    modalInput.focus()
    expect(modalInput).toHaveFocus()

    rerender(<InputArea onSendMessage={noop} networkId={1} channelName="#bar" />)

    // The message input must not grab focus while the user is typing elsewhere.
    expect(modalInput).toHaveFocus()
    expect(screen.getByTestId('message-input')).not.toHaveFocus()

    modalInput.remove()
  })

  it('grabs focus when the previously-focused field is removed before the switch', () => {
    // Mirrors the search-modal flow: the modal's search box is closed (removed
    // from the DOM, reverting focus to body) just before navigation re-renders
    // InputArea. Focus should then land in the message input.
    const { rerender } = render(
      <InputArea onSendMessage={noop} networkId={1} channelName="#foo" />
    )

    const modalInput = document.createElement('input')
    document.body.appendChild(modalInput)
    modalInput.focus()
    expect(modalInput).toHaveFocus()

    // Modal closes: the focused element leaves the DOM and focus reverts to body.
    modalInput.remove()

    rerender(<InputArea onSendMessage={noop} networkId={1} channelName="#bar" />)
    expect(screen.getByTestId('message-input')).toHaveFocus()
  })
})
