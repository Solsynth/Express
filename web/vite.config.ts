import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import vueJsx from '@vitejs/plugin-vue-jsx'
import { defineConfig, loadEnv } from 'vite'

// In development the SPA runs on the Vite server and every backend path is
// proxied to DistributionCenter, so the app uses same-origin relative URLs in
// both modes: in production the service serves web/dist itself, and the
// HttpOnly AuthToken cookie the hand-off sets on this domain stays first-party
// either way.
const backendPaths = ['/api', '/artifacts', '/health', '/alive', '/ready']

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const backend = env.VITE_API_TARGET || 'http://localhost:8080'

  return {
    plugins: [vue(), vueJsx(), tailwindcss()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      proxy: Object.fromEntries(backendPaths.map((path) => [path, backend])),
    },
  }
})
