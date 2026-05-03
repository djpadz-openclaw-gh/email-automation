import { test, expect } from '@playwright/test';

test('registration should be disabled', async ({ page }) => {
  // Navigate to the app
  await page.goto('https://email-automation.oc.ctb.padz.net');
  
  // Wait for page to load
  await page.waitForLoadState('networkidle');
  
  // Check if register link is visible
  const registerLink = page.locator('text=Don\'t have an account');
  const isVisible = await registerLink.isVisible().catch(() => false);
  
  console.log('Register link visible:', isVisible);
  
  // Check network requests for /config
  const requests: string[] = [];
  page.on('response', response => {
    if (response.url().includes('/config')) {
      requests.push(`${response.url()} - ${response.status()}`);
    }
  });
  
  // Wait a bit for any pending requests
  await page.waitForTimeout(2000);
  
  console.log('Config requests:', requests);
  
  // The register link should NOT be visible
  expect(isVisible).toBe(false);
});
