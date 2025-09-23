import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

// Helper function to safely check if SSL certificates exist
function getHttpsConfig() {
  const certPath = path.resolve(__dirname, '../backend/certs/server-san.crt')
  const keyPath = path.resolve(__dirname, '../backend/certs/server-san.key')
  
  try {
    if (fs.existsSync(certPath) && fs.existsSync(keyPath)) {
      return {
        key: fs.readFileSync(keyPath),
        cert: fs.readFileSync(certPath),
      }
    }
  } catch (error) {
    console.warn('SSL certificates not found, using HTTP for dev server')
  }
  
  return undefined
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    host: true,
    https: getHttpsConfig()
  },
  preview: {
    port: 4173,
    strictPort: true,
    host: true,
    https: getHttpsConfig()
  }
})
