const { defineConfig, devices } = require('@playwright/test');
module.exports = defineConfig({
  timeout: 30000,
  retries: 0,
  workers: 1,
  reporter: 'line',
  use: { trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
