import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  outputDir: './tests/e2e/results',
  timeout: 30000,
  use: {
    baseURL: 'http://frontend.email-automation.svc.cluster.local:3000',
    screenshot: 'on',
    trace: 'on-first-retry',
    viewport: { width: 1280, height: 800 },
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
  reporter: [['list'], ['html', { outputFolder: './tests/e2e/report', open: 'never' }]],
});
