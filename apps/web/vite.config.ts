import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

// The production bundle lands inside the Go module so `go build` embeds it
// (server/internal/web). The folder is git-ignored apart from .keep, which the build
// puts back after emptying the folder.
const outDir = resolve(__dirname, '../../server/internal/web/dist')

const keepFile = (): Plugin => ({
  name: 'roxy-keep-file',
  closeBundle() {
    mkdirSync(outDir, { recursive: true })
    writeFileSync(resolve(outDir, '.keep'), 'The Roxy Web build (apps/web, `npm run build`) lands in this folder and is embedded in the server binary.\n')
  },
})

export default defineConfig({
  plugins: [react(), keepFile()],
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    // The dev checkout sits on the Windows drive while Vite runs in WSL, where inotify
    // never fires for /mnt/c: poll, or edits are silently served stale.
    watch: { usePolling: true, interval: 400 },
    // The dev server proxies the API to a running Roxy Server, so the page is
    // same-origin in development too (no CORS, no tainted video).
    proxy: {
      '/v1': { target: process.env.ROXY_SERVER ?? 'http://127.0.0.1:8096', changeOrigin: true },
      '/healthz': { target: process.env.ROXY_SERVER ?? 'http://127.0.0.1:8096', changeOrigin: true },
    },
  },
})
