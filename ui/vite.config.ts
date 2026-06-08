import { defineConfig } from 'vite'

export default defineConfig({
  base: '/xact/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: 'index.html',
        test: 'test/index.html',
      },
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/zrender/')) return 'zrender';
          if (id.includes('/node_modules/echarts/')) return 'echarts';
        },
      },
    },
  },
  server: {
    port: 3000,
    open: false,
    proxy: {
      '/xact/login': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/xact/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/xact/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/xact/plugins': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/xact/ws': {
        target: 'ws://localhost:9222',
        ws: true,
        changeOrigin: true,
      },
      // '/xact': {
      //   target: 'http://localhost:8080',
      //   changeOrigin: true,
      // },
      // bypass: (req) => {
      //   const url = req.url || ''

      //   // Let Vite handle its own assets
      //   if (
      //     url.startsWith('/xact/@') ||
      //     url.startsWith('/xact/src/') ||
      //     url.includes('.ts') ||
      //     url.includes('.js') ||
      //     url.includes('.css')
      //   ) {
      //     return url
      //   }
      // },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: [],
    exclude: ['node_modules/**', 'dist/**', 'test/**/*.spec.ts'],
    coverage: {
      exclude: ['**/package.json'],
    },
  },
})
