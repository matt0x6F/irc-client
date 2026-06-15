import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'

// In a Wails webview there is no browser chrome, so a plain `<a href>` click
// navigates the embedded webview itself and `target="_blank"` is a silent no-op
// (the webview won't spawn a new window). Neither opens the user's real browser.
//
// We delegate from `document` rather than wiring each anchor: messages render
// continuously, so links appear after this handler is installed, and event
// delegation transparently covers them (plus the About pane link and any future
// anchors). External URLs are handed to the OS via the Wails runtime.

// Only intercept absolute http(s) links. We read the raw `href` attribute
// (not the normalized `.href` property) so relative/in-app links like "#tab"
// aren't resolved to an absolute URL and mistaken for external ones.
function isExternalHref(href: string | null): href is string {
  return href !== null && /^https?:\/\//i.test(href)
}

export function handleExternalLinkClick(event: MouseEvent): void {
  // Let modified clicks (open-in-new-tab style) and non-primary buttons through
  // untouched — there's nothing meaningful for them to do in a single-window app,
  // but intercepting them would be surprising.
  if (event.defaultPrevented || event.button !== 0) return

  const target = event.target as Element | null
  const anchor = target?.closest('a')
  if (!anchor) return

  const href = anchor.getAttribute('href')
  if (!isExternalHref(href)) return

  event.preventDefault()
  BrowserOpenURL(href)
}

// Installs the delegated handler and returns a disposer that removes it.
export function installExternalLinkHandler(doc: Document = document): () => void {
  doc.addEventListener('click', handleExternalLinkClick)
  return () => doc.removeEventListener('click', handleExternalLinkClick)
}
