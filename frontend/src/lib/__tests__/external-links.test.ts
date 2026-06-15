import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock the Wails runtime binding. Resolves (from this test file) to the same
// module external-links.ts imports, so its import gets the mock too.
const { browserOpenURLMock } = vi.hoisted(() => ({
  browserOpenURLMock: vi.fn(),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: browserOpenURLMock,
}))

import { installExternalLinkHandler } from '../external-links'

// dispatchClick returns false when a listener called preventDefault (event was cancelled).
function dispatchClick(el: Element): boolean {
  return el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
}

describe('external link handler', () => {
  let cleanup: () => void

  beforeEach(() => {
    browserOpenURLMock.mockClear()
    cleanup = installExternalLinkHandler(document)
  })

  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it('opens an external http(s) link in the system browser and cancels in-app navigation', () => {
    const a = document.createElement('a')
    a.setAttribute('href', 'https://example.com/path')
    a.textContent = 'link'
    document.body.appendChild(a)

    const notCancelled = dispatchClick(a)

    expect(browserOpenURLMock).toHaveBeenCalledTimes(1)
    expect(browserOpenURLMock).toHaveBeenCalledWith('https://example.com/path')
    expect(notCancelled).toBe(false) // preventDefault was called
  })

  it('routes clicks that originate on a child element of the anchor', () => {
    const a = document.createElement('a')
    a.setAttribute('href', 'https://example.com/x')
    const child = document.createElement('span')
    child.textContent = 'inner'
    a.appendChild(child)
    document.body.appendChild(a)

    dispatchClick(child)

    expect(browserOpenURLMock).toHaveBeenCalledWith('https://example.com/x')
  })

  it('ignores clicks that are not on an anchor', () => {
    const div = document.createElement('div')
    document.body.appendChild(div)

    const notCancelled = dispatchClick(div)

    expect(browserOpenURLMock).not.toHaveBeenCalled()
    expect(notCancelled).toBe(true)
  })

  it('ignores non-http(s) hrefs such as internal anchors', () => {
    const a = document.createElement('a')
    a.setAttribute('href', '#section')
    document.body.appendChild(a)

    const notCancelled = dispatchClick(a)

    expect(browserOpenURLMock).not.toHaveBeenCalled()
    expect(notCancelled).toBe(true)
  })

  it('stops routing links after cleanup removes the listener', () => {
    cleanup()

    const a = document.createElement('a')
    a.setAttribute('href', 'https://example.com')
    document.body.appendChild(a)
    dispatchClick(a)

    expect(browserOpenURLMock).not.toHaveBeenCalled()

    // Re-install so afterEach's cleanup() has a live listener to remove.
    cleanup = installExternalLinkHandler(document)
  })
})
