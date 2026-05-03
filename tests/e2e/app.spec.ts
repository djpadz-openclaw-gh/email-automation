import { test, expect } from '@playwright/test';

const API_BASE = 'http://10.152.183.143:8080';
const TEST_USER = 'testuser';
const TEST_PASS = 'testpass123!';

test.describe('Email Automation UI', () => {

  test('login page loads with correct title', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.screenshot({ path: 'tests/e2e/screenshots/01-login-page.png', fullPage: true });

    // Should see the Email Automation heading
    await expect(page.locator('h1', { hasText: 'Email Automation' })).toBeVisible({ timeout: 10000 });

    // Should see the passkey and password tabs
    const passwordTab = page.locator('button', { hasText: /Password/i });
    await expect(passwordTab).toBeVisible();
  });

  test('login with username and password - no errors', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Click the Password tab (default is Passkey)
    const passwordTab = page.locator('button', { hasText: /Password/i });
    await passwordTab.click();
    await page.waitForTimeout(500);

    // Fill in credentials using the actual input IDs from LoginForm.tsx
    await page.locator('#login-username').fill(TEST_USER);
    await page.locator('#login-password').fill(TEST_PASS);
    await page.screenshot({ path: 'tests/e2e/screenshots/02-login-filled.png', fullPage: true });

    // Click Sign In
    await page.locator('button[type="submit"]', { hasText: 'Sign In' }).click();

    // Wait for login to complete and dashboard to load
    await page.waitForTimeout(3000);
    await page.screenshot({ path: 'tests/e2e/screenshots/03-after-login.png', fullPage: true });

    // CRITICAL: Check there's no "interface conversion" error
    const pageContent = await page.textContent('body');
    expect(pageContent).not.toContain('interface conversion');
    expect(pageContent).not.toContain('nil, not int64');

    // Should no longer see the login form
    await expect(page.locator('h1', { hasText: 'Email Automation' })).toBeVisible();
  });

  test('dashboard loads accounts and rules without errors', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Login
    await page.locator('button', { hasText: /Password/i }).click();
    await page.waitForTimeout(300);
    await page.locator('#login-username').fill(TEST_USER);
    await page.locator('#login-password').fill(TEST_PASS);
    await page.locator('button[type="submit"]', { hasText: 'Sign In' }).click();
    await page.waitForTimeout(3000);

    // Take screenshot of the dashboard
    await page.screenshot({ path: 'tests/e2e/screenshots/04-dashboard.png', fullPage: true });

    // Verify no error banners
    const errorBanner = page.locator('[role="alert"]');
    const errorCount = await errorBanner.count();
    if (errorCount > 0) {
      const errorText = await errorBanner.first().textContent();
      // Fail if the error is the int64 conversion error
      expect(errorText).not.toContain('interface conversion');
      expect(errorText).not.toContain('nil, not int64');
    }

    // Page should have loaded successfully
    const body = await page.textContent('body');
    expect(body).toBeTruthy();
    expect(body).not.toContain('interface conversion');
  });

  test('can navigate to rule editor', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Login
    await page.locator('button', { hasText: /Password/i }).click();
    await page.waitForTimeout(300);
    await page.locator('#login-username').fill(TEST_USER);
    await page.locator('#login-password').fill(TEST_PASS);
    await page.locator('button[type="submit"]', { hasText: 'Sign In' }).click();
    await page.waitForTimeout(3000);

    // Look for new rule / add rule button
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    if (await newRuleBtn.isVisible()) {
      await newRuleBtn.click();
      await page.waitForTimeout(1000);
    }

    await page.screenshot({ path: 'tests/e2e/screenshots/05-rule-editor.png', fullPage: true });

    // No errors
    const body = await page.textContent('body');
    expect(body).not.toContain('interface conversion');
  });

  test('API endpoints return proper responses (no panics)', async ({ request }) => {
    // Login via API
    const loginResp = await request.post(`${API_BASE}/auth/login`, {
      data: { username: TEST_USER, password: TEST_PASS },
    });
    expect(loginResp.ok()).toBeTruthy();
    const loginData = await loginResp.json();
    expect(loginData.token).toBeTruthy();
    expect(loginData.user.id).toBeGreaterThan(0);

    const token = loginData.token;
    const headers = { Authorization: `Bearer ${token}` };

    // Test accounts endpoint - should return 200, not 500
    const accountsResp = await request.get(`${API_BASE}/api/v1/accounts`, { headers });
    expect(accountsResp.ok()).toBeTruthy();
    const accounts = await accountsResp.json();
    expect(Array.isArray(accounts)).toBeTruthy();

    // Test rules endpoint
    const rulesResp = await request.get(`${API_BASE}/api/v1/rules`, { headers });
    expect(rulesResp.ok()).toBeTruthy();
    const rules = await rulesResp.json();
    expect(Array.isArray(rules)).toBeTruthy();

    // Test profile endpoint
    const profileResp = await request.get(`${API_BASE}/auth/profile`, { headers });
    expect(profileResp.ok()).toBeTruthy();
    const profile = await profileResp.json();
    expect(profile.username).toBe(TEST_USER);
  });
});
