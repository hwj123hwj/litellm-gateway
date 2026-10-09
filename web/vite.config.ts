import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: './',
  define: {
    __APP_VERSION__: JSON.stringify('1.0.0'),
  },
  build: {
    outDir: 'dist/renderer',
    emptyOutDir: true,
  },
  server: {
    port: 3000,
    proxy: {
      '/admin': {
        target: 'http://localhost:4001',
        changeOrigin: true,
      },
    },
  },
})
