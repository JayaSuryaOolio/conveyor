import {defineConfig} from '@playwright/test'

// Run: npm run e2e  (needs `wails generate module` once for ../wailsjs; the Go bindings are mocked in e2e/mock.ts).
export default defineConfig({
    testDir: 'e2e',
    workers: 1,
    use: {baseURL: 'http://localhost:5199'},
    webServer: {command: 'npx vite --port 5199 --strictPort', url: 'http://localhost:5199', reuseExistingServer: true},
})
