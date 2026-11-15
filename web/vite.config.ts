import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The Go binary embeds web/dist (see web/embed.go). The build goes to
// dist/ui so the committed dist/placeholder is never touched.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/ui',
    emptyOutDir: true,
  },
  server: {
    // `npm run dev` talks to a backend started with `make run`.
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
});
