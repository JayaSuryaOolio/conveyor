import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      // Vendor chunks keep the app chunk small; the terminal and markdown renderer are lazy-loaded.
      output: {manualChunks: {mantine: ['@mantine/core', '@mantine/hooks'], icons: ['@tabler/icons-react']}},
    },
  },
})
