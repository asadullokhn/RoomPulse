import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Build output lands inside the Go module (backend/internal/api/web/dist) so
// //go:embed can pick it up — go:embed can't reach outside its package dir.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: fileURLToPath(new URL('../backend/internal/api/web/dist', import.meta.url)),
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/rooms': 'http://localhost:8080',
      '/reservations': 'http://localhost:8080',
      '/occupancy': 'http://localhost:8080',
      '/devices': 'http://localhost:8080',
      '/beacons': 'http://localhost:8080',
      '/events': 'http://localhost:8080',
      '/utilization': 'http://localhost:8080',
      '/collisions': 'http://localhost:8080',
      '/overstays': 'http://localhost:8080',
      '/notifications': 'http://localhost:8080',
      '/floor/rooms': 'http://localhost:8080',
      '/floor/image': 'http://localhost:8080',
      '/info': 'http://localhost:8080',
      '/sync': 'http://localhost:8080',
      '/favicon.svg': 'http://localhost:8080',
    },
  },
})
