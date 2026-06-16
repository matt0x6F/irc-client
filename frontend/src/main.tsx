import React from 'react'
import {createRoot} from 'react-dom/client'
import {EventsEmit} from '../wailsjs/runtime/runtime'
import './style.css'
import App from './App'
import { SettingsWindow } from './components/settings-window'
import { initTheme } from './stores/theme'
import { initSettings } from './stores/settings'
import { initPreferences } from './stores/preferences'
import { installExternalLinkHandler } from './lib/external-links'

// The same bundle backs both the main window and the standalone Settings window.
// The backend opens the latter at /?view=settings (see App.openSettingsSection);
// we branch on it here to render the settings UI instead of the main app.
const isSettingsWindow = new URLSearchParams(window.location.search).get('view') === 'settings'

// Suppress expected Wails dev mode WebSocket errors
// These occur when Wails tries to connect to the dev server before it's ready
const originalError = console.error
console.error = (...args: any[]) => {
  const message = args[0]?.toString() || ''
  // Filter out Wails dev mode WebSocket connection errors
  if (
    message.includes('WebSocket connection') ||
    message.includes('wails.localhost') ||
    message.includes('localhost:undefined') ||
    message.includes('Invalid url for WebSocket') ||
    (message.includes('WebSocket') && message.includes('failed'))
  ) {
    // Silently ignore these expected dev mode errors
    return
  }
  originalError.apply(console, args)
}

// Suppress unhandled promise rejections from Wails WebSocket
window.addEventListener('unhandledrejection', (event) => {
  const reason = event.reason?.toString() || ''
  if (
    reason.includes('WebSocket') ||
    reason.includes('wails.localhost') ||
    reason.includes('localhost:undefined') ||
    reason.includes('did not match the expected pattern') ||
    (reason.includes('WebSocket') && reason.includes('failed'))
  ) {
    event.preventDefault()
    return
  }
})

// Open external links (message URLs, the About pane link) in the system
// browser. In a Wails webview a bare <a> click would otherwise navigate the
// app's own webview or silently do nothing.
installExternalLinkHandler()

// Track window focus/blur for desktop notification suppression. Only the main
// window drives this — the Settings window gaining/losing focus must not toggle
// notification suppression for chat in the main window.
if (!isSettingsWindow) {
  window.addEventListener('focus', () => {
    EventsEmit('window-focused')
  })
  window.addEventListener('blur', () => {
    EventsEmit('window-blurred')
  })
}

const container = document.getElementById('root')

const root = createRoot(container!)

// Hydrate durable UI preferences (consolidate join/quit, formatting toolbar, …)
// from the backend in parallel, and subscribe each store to the setting:changed
// broadcast so a change in one window reaches the other live. Both run in both
// windows. Non-blocking — stores carry sensible defaults until they resolve.
void initSettings()
void initPreferences()

// Load the persisted theme (stored in the backend DB, not localStorage) before
// the first paint to avoid a flash of the wrong theme — in both windows, so the
// Settings window opens already themed. initTheme applies a synchronous default
// first and always resolves, so a slow or failed read still renders.
initTheme().finally(() => {
    root.render(
        <React.StrictMode>
            {isSettingsWindow ? <SettingsWindow/> : <App/>}
        </React.StrictMode>
    )
})
