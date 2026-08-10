import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Build output feeds internal/webui's //go:embed directive directly - see
// internal/webui/webui.go. The dev server proxies /v1 to a real `bop
// controller` (see web/README.md) so the UI is always developed against
// the actual API, never a mock.
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/v1': 'http://127.0.0.1:9091',
    },
  },
})
