import React from 'react'
import {createRoot} from 'react-dom/client'
import {EventsEmit} from '../wailsjs/runtime/runtime'
import './style.css'
import App from './App'
import { initTheme } from './stores/theme'
import { installExternalLinkHandler } from './lib/external-links'

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

// Track window focus/blur for desktop notification suppression
window.addEventListener('focus', () => {
  EventsEmit('window-focused')
})
window.addEventListener('blur', () => {
  EventsEmit('window-blurred')
})

const container = document.getElementById('root')

const root = createRoot(container!)

// Load the persisted theme (now stored in the backend DB, not localStorage)
// before the first paint to avoid a flash of the wrong theme. initTheme applies
// a synchronous default first and always resolves, so a slow or failed read
// still renders the app.
initTheme().finally(() => {
    root.render(
        <React.StrictMode>
            <App/>
        </React.StrictMode>
    )
})
