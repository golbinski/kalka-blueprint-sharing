import { copyFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The build goes into the Go package that embeds it. dist/.gitkeep keeps
// the directory in git so `go build` works without a UI build.
const here = fileURLToPath(new URL('.', import.meta.url))
const outDir = resolve(here, '../internal/ui/dist')

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'keep-gitkeep',
      closeBundle() {
        writeFileSync(resolve(outDir, '.gitkeep'), '')
        // Mermaid's own browser bundle, unmodified, for the sandboxed frame
        // in public/mermaid/; it is loaded only when a page shows a diagram.
        mkdirSync(resolve(outDir, 'mermaid'), { recursive: true })
        copyFileSync(resolve(here, 'node_modules/mermaid/dist/mermaid.min.js'), resolve(outDir, 'mermaid/mermaid.min.js'))
      },
    },
  ],
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
    // No inline scripts or styles: the CSP forbids them.
    assetsInlineLimit: 0,
    // Served from the local daemon; highlight.js's languages push the bundle past the default 500 kB.
    chunkSizeWarningLimit: 700,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:7717' },
    // parity.test.ts reads internal/cli/parity.json (spec 12.2).
    fs: { allow: [here, resolve(here, '../internal/cli/parity.json')] },
  },
  test: {
    environment: 'jsdom',
  },
})
