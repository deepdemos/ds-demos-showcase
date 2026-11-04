import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    // `/preview` 也必须代理：详情页把作品放在 iframe 里，src 是 `/preview/{slug}/`。
    // 漏了这一条，iframe 打到的是 Vite dev server，拿到的是 404（或者 SPA 的 index.html），
    // 表现是「作品详情能打开，但里面永远空白」——而生产和 CI 都不会暴露这个问题，
    // 因为那边 Nginx 把 /preview 和 /api 一起反代给 Go。
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/preview': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: false,
  },
});
