import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The backend sends no CORS headers and blocks cross-origin writes, so the
// dev server proxies /api to it and the app runs on a single origin.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
});
