import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    host: true,
    https: {
      key: fs.readFileSync(path.resolve(__dirname, '../backend/certs/server-san.key')),
      cert: fs.readFileSync(path.resolve(__dirname, '../backend/certs/server-san.crt')),
    }
  },
  preview: {
    port: 4173,
    strictPort: true,
    host: true,
    https: {
      key: fs.readFileSync(path.resolve(__dirname, '../backend/certs/server-san.key')),
      cert: fs.readFileSync(path.resolve(__dirname, '../backend/certs/server-san.crt')),
    }
  }
})
