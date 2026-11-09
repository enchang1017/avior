import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env['AVIOR_E2E_BASE_URL'] ?? 'http://localhost:5173'

/*
 * The performance budget (ADR 0009 P8 to P18) is measured on a production
 * build, not the dev server the rest of the suite drives: `vite preview` on a
 * port of its own, built fresh for the run.
 */
const previewPort = Number(process.env['AVIOR_PREVIEW_PORT'] ?? '4174')

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: process.env['CI'] ? 1 : 0,
  reporter: process.env['CI'] ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    // A DBA console is used on a large screen, and the result grid sits below a
    // picker and an editor. At 720px high it falls off the fold, and toBeVisible
    // does not scroll.
    viewport: { width: 1440, height: 1000 },
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: `vite build && exec vite preview --port ${previewPort} --strictPort`,
    url: `http://localhost:${previewPort}/login`,
    reuseExistingServer: false,
    timeout: 180_000,
  },
  projects: [
    {
      name: 'chromium',
      testIgnore: /(performance|effects)\.spec\.ts$/u,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'performance',
      testMatch: /(performance|effects)\.spec\.ts$/u,
      // One at a time: a budget measured beside another browser is a measure of both.
      fullyParallel: false,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: `http://localhost:${previewPort}`,
        viewport: { width: 1440, height: 900 },
      },
    },
  ],
  workers: process.env['CI'] ? 2 : undefined,
})
