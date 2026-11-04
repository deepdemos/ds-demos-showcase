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
      // `/download` 同理，而且漏了更隐蔽：Vite 的 SPA fallback 会回 200 + index.html，
      // 页面上「下载作品」点下去拿到的是一个 HTML 页面（浏览器会当页面渲染、而不是落盘），
      // 没有任何报错。这条和 `/preview` 是同一个坑，第 218 号 PR 补了前者、漏了后者。
      '/api': 'http://127.0.0.1:8080',
      '/preview': 'http://127.0.0.1:8080',
      '/download': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: false,
  },
});
