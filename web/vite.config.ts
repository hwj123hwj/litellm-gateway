import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import desktopConfig from '../desktop/mygo.json'

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  base: './',
  define: {
    __APP_VERSION__: JSON.stringify(mode === 'desktop' ? desktopConfig.version : '1.0.0'),
  },
  build: {
    outDir: mode === 'desktop' ? '../desktop/frontend.noindex' : 'dist/renderer',
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
}))
