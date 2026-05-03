import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30000,
  retries: 1,
  use: {
    baseURL: 'http://10.152.183.195:3000',
    screenshot: 'on',
    trace: 'on-first-retry',
  },
  reporter: [['list'], ['html', { outputFolder: 'tests/e2e/results' }]],
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
