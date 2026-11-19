import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const backendTarget = env.VITE_BACKEND_PROXY_TARGET || 'http://127.0.0.1:8080'
  const proxy = { target: backendTarget, changeOrigin: false }

  return {
    plugins: [vue({ template: { transformAssetUrls: false } })],
    server: {
      proxy: {
        '/api': proxy,
        '/healthz': proxy,
        '/readyz': proxy,
      },
    },
  }
})
