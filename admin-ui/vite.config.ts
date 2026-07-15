import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

function normalizeBasePath(value: string | undefined): string {
  const path = value?.trim() || '/app/searxng-admin/'
  return `${path.startsWith('/') ? path : `/${path}`}${path.endsWith('/') ? '' : '/'}`
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiBase = env.VITE_API_BASE?.trim()

  return {
    base: normalizeBasePath(env.VITE_BASE_PATH),
    plugins: [vue()],
    server: apiBase
      ? {
          proxy: {
            '/api': {
              target: apiBase,
              changeOrigin: true,
            },
          },
        }
      : undefined,
  }
})
