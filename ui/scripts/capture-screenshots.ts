/**
 * capture-screenshots.ts - Playwright script to capture screenshots of the
 * XACT UI for inclusion in the user manual.
 *
 * Usage:
 *   npx playwright test scripts/capture-screenshots.ts
 *
 * Prerequisites:
 *   npm i -D @playwright/test
 *   npx playwright install chromium
 *
 * The XACT server and UI dev server must be running before execution.
 * Adjust BASE_URL, LOGIN_NAME, and LOGIN_PASSWORD below as needed.
 */

import { test } from '@playwright/test';
import path from 'path';

const BASE_URL = process.env.XACT_URL || 'http://localhost:5173';
const LOGIN_NAME = process.env.XACT_USER || 'admin';
const LOGIN_PASSWORD = process.env.XACT_PASS || 'admin';

const OUTPUT_DIR = path.resolve(__dirname, '../public/manual/images');

/** Helper: log in and return the authenticated page. */
async function login(page: import('@playwright/test').Page): Promise<void> {
  await page.goto(BASE_URL);
  // Wait for the login form to appear.
  await page.waitForSelector('input[type="text"], input[name="login_name"]', { timeout: 10_000 });
  await page.fill('input[type="text"], input[name="login_name"]', LOGIN_NAME);
  await page.fill('input[type="password"]', LOGIN_PASSWORD);
  await page.click('button[type="submit"], button:has-text("Sign In")');
  // Wait for the main UI to load.
  await page.waitForSelector('app-sidebar, app-header', { timeout: 15_000 });
  // Give widgets a moment to render.
  await page.waitForTimeout(2000);
}

// ── Screenshot definitions ───────────────────────────────────────────────────
//
// Add entries here for each screenshot you want to capture. Each entry
// describes what to navigate to and what to capture.
//

interface ScreenshotDef {
  name: string;          // output filename (without extension)
  description: string;   // for logging
  capture: (page: import('@playwright/test').Page) => Promise<void>;
}

const screenshots: ScreenshotDef[] = [
  {
    name: 'login-screen',
    description: 'Login page before authentication',
    capture: async (page) => {
      await page.goto(BASE_URL);
      await page.waitForSelector('input[type="password"]', { timeout: 10_000 });
      await page.waitForTimeout(500);
      await page.screenshot({ path: path.join(OUTPUT_DIR, 'login-screen.png'), fullPage: false });
    },
  },
  {
    name: 'main-interface',
    description: 'Main dashboard after login',
    capture: async (page) => {
      await login(page);
      await page.screenshot({ path: path.join(OUTPUT_DIR, 'main-interface.png'), fullPage: false });
    },
  },
  {
    name: 'sidebar',
    description: 'Sidebar navigation',
    capture: async (page) => {
      await login(page);
      const sidebar = page.locator('app-sidebar');
      if (await sidebar.isVisible()) {
        await sidebar.screenshot({ path: path.join(OUTPUT_DIR, 'sidebar.png') });
      }
    },
  },
  {
    name: 'widget-toolbar',
    description: 'Widget toolbar in edit mode',
    capture: async (page) => {
      await login(page);
      // Try to enter edit mode by clicking the edit toggle.
      const editBtn = page.locator('[title="Edit"], button:has-text("Edit"), .edit-toggle').first();
      if (await editBtn.isVisible()) {
        await editBtn.click();
        await page.waitForTimeout(1000);
      }
      await page.screenshot({ path: path.join(OUTPUT_DIR, 'widget-toolbar.png'), fullPage: false });
    },
  },
];

// ── Test runner ──────────────────────────────────────────────────────────────

test.describe('Manual screenshots', () => {
  test.use({
    viewport: { width: 1440, height: 900 },
    colorScheme: 'dark',
  });

  for (const ss of screenshots) {
    test(`capture: ${ss.name}`, async ({ page }) => {
      console.log(`  Capturing ${ss.name} - ${ss.description}`);
      await ss.capture(page);
      console.log(`  ✓ Saved ${ss.name}.png`);
    });
  }
});
