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
    host: 'localhost',
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
})
