import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const gateway = process.env.VITE_GATEWAY || 'http://127.0.0.1:8090'

export default defineConfig({
  base: '/',
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api/portal': { target: gateway, changeOrigin: false },
      '/api/admin': { target: gateway, changeOrigin: false },
      '/voice': { target: gateway, ws: true, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rolldownOptions: {
      output: {
        // Give the large, rarely changing dependencies their own chunks so an
        // app-code release does not invalidate them in the browser cache.
        // Everything else is left to automatic chunking, which keeps packages
        // used by a single route (Table, Select, ...) inside that route's
        // lazy chunk instead of the one every visitor downloads.
        codeSplitting: {
          groups: [
            {
              name: 'react',
              test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/,
              priority: 100,
            },
            {
              name: 'tanstack',
              test: /node_modules[\\/]@tanstack[\\/]/,
              priority: 90,
            },
          ],
        },
      },
    },
  },
})
