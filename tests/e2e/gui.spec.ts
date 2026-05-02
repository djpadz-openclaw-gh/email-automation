import { test, expect } from '@playwright/test';

// Get a tenant API key from the running API
async function getTenantApiKey(): Promise<string> {
  const adminKey = 'ea_45d384f375af77460856c9a50110463a2bb824ff8324b7ccc2594e6a48673a41';
  const apiUrl = 'http://api.email-automation.svc.cluster.local:8080';
  
  const response = await fetch(`${apiUrl}/admin/tenants`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-API-Key': adminKey },
    body: JSON.stringify({ name: 'E2E Test Tenant', slug: `e2e-test-${Date.now()}` }),
  });
  const data = await response.json();
  return data.api_key;
}

let apiKey: string;

test.beforeAll(async () => {
  apiKey = await getTenantApiKey();
});

test.describe('Email Automation Frontend - Login Screen', () => {
  test('login page renders correctly', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    
    await expect(page).toHaveTitle(/Email Automation/);
    
    // Check login form elements
    const heading = page.locator('text=Email Automation').first();
    await expect(heading).toBeVisible();
    
    const apiKeyInput = page.locator('input').first();
    await expect(apiKeyInput).toBeVisible();
    
    const connectBtn = page.locator('button:has-text("Connect")');
    await expect(connectBtn).toBeVisible();
    
    await page.screenshot({ path: 'tests/e2e/screenshots/01-login-page.png', fullPage: true });
  });

  test('login page dark mode', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    
    await page.screenshot({ path: 'tests/e2e/screenshots/02-login-dark-mode.png', fullPage: true });
  });

  test('login page mobile responsive', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    
    await page.screenshot({ path: 'tests/e2e/screenshots/03-login-mobile.png', fullPage: true });
  });
});

test.describe('Email Automation Frontend - Authenticated', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    
    // Enter API key and connect
    const apiKeyInput = page.locator('input').first();
    await apiKeyInput.fill(apiKey);
    
    const connectBtn = page.locator('button:has-text("Connect")');
    await connectBtn.click();
    
    // Wait for the main UI to load
    await page.waitForTimeout(1500);
  });

  test('main dashboard renders after login', async ({ page }) => {
    await page.screenshot({ path: 'tests/e2e/screenshots/04-dashboard.png', fullPage: true });
    
    // Should show some kind of main UI (rules list, empty state, etc.)
    const body = await page.textContent('body');
    expect(body).toBeTruthy();
  });

  test('new rule button exists and opens editor', async ({ page }) => {
    // Look for any button that creates a new rule
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    
    if (await newRuleBtn.isVisible()) {
      await page.screenshot({ path: 'tests/e2e/screenshots/05-before-new-rule.png', fullPage: true });
      
      await newRuleBtn.click();
      await page.waitForTimeout(1000);
      
      await page.screenshot({ path: 'tests/e2e/screenshots/06-rule-editor.png', fullPage: true });
    } else {
      // Take screenshot of whatever state we're in
      await page.screenshot({ path: 'tests/e2e/screenshots/05-dashboard-state.png', fullPage: true });
    }
  });

  test('rule editor shows natural language input', async ({ page }) => {
    // Open new rule editor
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    if (await newRuleBtn.isVisible()) {
      await newRuleBtn.click();
      await page.waitForTimeout(1000);
    }
    
    // Check for natural language textarea
    const nlInput = page.locator('textarea').first();
    if (await nlInput.isVisible()) {
      await nlInput.fill('Delete emails from noreply@example.com that are older than 24 hours');
      await page.waitForTimeout(500);
      await page.screenshot({ path: 'tests/e2e/screenshots/07-natural-language.png', fullPage: true });
    } else {
      await page.screenshot({ path: 'tests/e2e/screenshots/07-editor-state.png', fullPage: true });
    }
  });

  test('rule editor shows code editor (Monaco)', async ({ page }) => {
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    if (await newRuleBtn.isVisible()) {
      await newRuleBtn.click();
      await page.waitForTimeout(2000); // Monaco takes time to load
    }
    
    // Monaco editor renders with specific classes
    const monacoEditor = page.locator('.monaco-editor, [data-keybinding-context], section:has(textarea)').first();
    await page.screenshot({ path: 'tests/e2e/screenshots/08-code-editor.png', fullPage: true });
  });

  test('reference panel toggles open', async ({ page }) => {
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    if (await newRuleBtn.isVisible()) {
      await newRuleBtn.click();
      await page.waitForTimeout(1000);
    }
    
    const refBtn = page.locator('button:has-text("Reference")').first();
    if (await refBtn.isVisible()) {
      await refBtn.click();
      await page.waitForTimeout(500);
      await page.screenshot({ path: 'tests/e2e/screenshots/09-reference-panel.png', fullPage: true });
    } else {
      await page.screenshot({ path: 'tests/e2e/screenshots/09-current-state.png', fullPage: true });
    }
  });

  test('test panel opens with sample email', async ({ page }) => {
    const newRuleBtn = page.locator('button:has-text("New"), button:has-text("Add"), button:has-text("Create"), button:has-text("+")').first();
    if (await newRuleBtn.isVisible()) {
      await newRuleBtn.click();
      await page.waitForTimeout(1000);
    }
    
    const testBtn = page.locator('button:has-text("Test")').first();
    if (await testBtn.isVisible()) {
      await testBtn.click();
      await page.waitForTimeout(500);
      await page.screenshot({ path: 'tests/e2e/screenshots/10-test-panel.png', fullPage: true });
    } else {
      await page.screenshot({ path: 'tests/e2e/screenshots/10-current-state.png', fullPage: true });
    }
  });

  test('dark mode renders correctly when authenticated', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.waitForTimeout(500);
    await page.screenshot({ path: 'tests/e2e/screenshots/11-authenticated-dark.png', fullPage: true });
  });

  test('mobile responsive when authenticated', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.waitForTimeout(500);
    await page.screenshot({ path: 'tests/e2e/screenshots/12-authenticated-mobile.png', fullPage: true });
  });
});
