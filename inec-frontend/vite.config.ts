import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      '/auth': 'http://localhost:8088',
      '/api': 'http://localhost:8088',
      '/disputes': 'http://localhost:8088',
      '/push': 'http://localhost:8088',
      '/healthz': 'http://localhost:8088',
      '/elections': 'http://localhost:8088',
      '/results': 'http://localhost:8088',
      '/collation': 'http://localhost:8088',
      '/dashboard': 'http://localhost:8088',
      '/observer': 'http://localhost:8088',
      '/bvas': 'http://localhost:8088',
      '/audit': 'http://localhost:8088',
      '/incidents': 'http://localhost:8088',
    },
  },
})

