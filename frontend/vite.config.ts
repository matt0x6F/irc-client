import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import wails from '@wailsio/runtime/plugins/vite'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  // `wails("./bindings")` is the canonical v3 plugin (matches the react-ts
  // template). It wires the generated typed-event bindings into the runtime at
  // build time; with it installed, `vite build` requires `frontend/bindings`
  // to have been generated first. It is a build-time transform only (no
  // dev-server proxy), so it does not affect the server-mode e2e harness.
  plugins: [react(), tailwindcss(), wails('./bindings')],
  server: {
    port: Number(process.env.VITE_PORT) || 5173,
    strictPort: true,
    // Bind IPv4 explicitly: Wails' dev asset proxy dials tcp4 127.0.0.1, but
    // 'localhost' resolves to ::1 (IPv6) first on some systems, leaving Vite
    // IPv6-only — the proxy then gets "connection refused" and the webview
    // never loads. 127.0.0.1 keeps the dev page reachable for both `task dev`
    // and `task dev:mcp`.
    host: '127.0.0.1',
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})
