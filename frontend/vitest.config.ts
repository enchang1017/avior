import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    include: ['src/**/*.spec.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      include: ['src/**/*.{ts,vue}'],
      // Generated client types carry no logic to cover; the no-drift gate is
      // what keeps them honest.
      // Views are template, and their behaviour is asserted end to end by
      // Playwright. Logic lives in composables and stores, which are covered
      // here. See the test layers in CLAUDE.md.
      exclude: [
        'src/gen/**',
        'src/test-setup.ts',
        'src/main.ts',
        'src/App.vue',
        // Views are template, asserted end to end by Playwright.
        'src/**/*View.vue',
        // Layout chrome: navigation and a page title, nothing to get wrong that
        // a unit test would catch before the eye does.
        'src/layouts/AppShell.vue',
        // A wrapper around CodeMirror. What matters is that typing reaches the
        // model and the model reaches the editor, which needs a real DOM and a
        // real editor: the e2e suite covers it.
        'src/features/query/SqlEditor.vue',
      ],
      thresholds: {
        statements: 80,
        branches: 75,
        functions: 80,
        lines: 80,
      },
    },
  },
})
