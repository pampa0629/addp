import { defineConfig } from '@playwright/test'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

const artifactDir = process.env.ADDP_ONLINE_ARTIFACT_DIR || resolve(tmpdir(), 'addp-online-browser')

export default defineConfig({
  testDir: './e2e/online',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'line',
  outputDir: resolve(artifactDir, 'playwright'),
  timeout: 180_000,
  expect: { timeout: 45_000 },
  use: {
    baseURL: process.env.CONSOLE_URL || 'http://127.0.0.1:5170',
    headless: true,
    screenshot: 'only-on-failure',
    trace: 'off',
    viewport: { width: 1440, height: 900 }
  }
})
