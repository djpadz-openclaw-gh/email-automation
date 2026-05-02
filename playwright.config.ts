import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  outputDir: './tests/e2e/results',
  timeout: 30000,
  use: {
    baseURL: 'http://localhost:18082',
    connectOptions: {
      wsEndpoint: 'http://127.0.0.1:18800',
    },
    screenshot: 'on',
    trace: 'on-first-retry',
    viewport: { width: 1280, height: 800 },
  },
  reporter: [['list'], ['html', { outputFolder: './tests/e2e/report', open: 'never' }]],
});
