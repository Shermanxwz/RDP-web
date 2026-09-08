const { test, expect } = require('@playwright/test');
const fs = require('fs');

test.use({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140 Safari/537.36' });

test('first-run UI, CRUD, PWA, export and Windows RDP download', async ({ page }) => {
  await page.goto('http://127.0.0.1:18081/?setup=browser-setup-token');
  await expect(page.locator('#auth')).toBeVisible();
  await page.locator('#auth-username').fill('browserowner');
  await page.locator('#auth-password').fill('correct horse battery staple');
  await page.locator('#auth-submit').click();
  await expect(page.locator('#app')).toBeVisible();

  const sw = await page.evaluate(async () => {
    if (!('serviceWorker' in navigator)) return false;
    await navigator.serviceWorker.ready;
    return !!(await navigator.serviceWorker.getRegistration());
  });
  expect(sw).toBeTruthy();

  await page.locator('#add-group').click();
  await page.locator('#group-name').fill('Production');
  await page.locator('#group-form button[type="submit"]').click();
  await expect(page.locator('#group-list')).toContainText('Production');

  await page.locator('#add-device').click();
  await page.locator('#device-name').fill('Browser Server');
  await page.locator('#device-host').fill('browser.example.com');
  await page.locator('#device-username').fill('Administrator');
  await page.locator('#device-group').selectOption({ label: 'Production' });
  await page.locator('#device-favorite').check();
  await page.locator('#device-form button[type="submit"]').click();
  await expect(page.locator('.device-card')).toContainText('Browser Server');

  await page.locator('#search').fill('browser.example.com');
  await expect(page.locator('.device-card')).toHaveCount(1);
  await page.locator('#search').fill('');

  const rdpDownload = page.waitForEvent('download');
  await page.locator('.connect').click();
  const rdp = await rdpDownload;
  const rdpPath = await rdp.path();
  const bytes = fs.readFileSync(rdpPath);
  expect(bytes[0]).toBe(0xff);
  expect(bytes[1]).toBe(0xfe);

  const exportDownload = page.waitForEvent('download');
  await page.locator('#export-btn').click();
  const exported = await exportDownload;
  const json = JSON.parse(fs.readFileSync(await exported.path(), 'utf8'));
  expect(json.app).toBe('RDP Web');
  expect(json.devices.some(d => d.host === 'browser.example.com')).toBeTruthy();

  await page.locator('#logout-btn').click();
  await expect(page.locator('#auth')).toBeVisible();
  await page.locator('#auth-username').fill('browserowner');
  await page.locator('#auth-password').fill('correct horse battery staple');
  await page.locator('#auth-submit').click();
  await expect(page.locator('.device-card')).toContainText('Browser Server');
});
