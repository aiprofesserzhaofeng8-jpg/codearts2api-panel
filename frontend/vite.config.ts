import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
const backend = process.env.PANEL_BACKEND ?? 'http://127.0.0.1:10089';
export default defineConfig({
  plugins: [vue()],
  server: { host: '127.0.0.1', port: 5173, strictPort: true, proxy: Object.fromEntries(['/admin', '/auth', '/user/api', '/v1', '/health', '/metrics'].map(prefix => [prefix, {
    target: backend, changeOrigin: true, xfwd: true,
    configure(proxy) { proxy.on('proxyReq', (request, incoming) => {
      // Translate only the actual development origin. Preserve foreign origins
      // so the backend continues to reject cross-site administrative writes.
      const origin = incoming.headers.origin;
      if (origin && origin === new URL('http://' + incoming.headers.host).origin) request.setHeader('Origin', new URL(backend).origin);
    }); }
  }])) },
  build: { manifest: true, sourcemap: false, target: 'es2022' },
});
