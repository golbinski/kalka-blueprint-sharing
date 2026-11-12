import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// The build goes into the Go package that embeds it. dist/.gitkeep keeps
// the directory in git so `go build` works without a UI build.
const outDir = resolve(fileURLToPath(new URL('.', import.meta.url)), '../internal/ui/dist')

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'keep-gitkeep',
      closeBundle() {
        writeFileSync(resolve(outDir, '.gitkeep'), '')
      },
    },
  ],
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
    // No inline scripts or styles: the CSP forbids them.
    assetsInlineLimit: 0,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:7717' },
  },
  test: {
    environment: 'jsdom',
  },
})
