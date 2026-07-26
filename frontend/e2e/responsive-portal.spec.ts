import { test, expect } from '@playwright/test';

test.describe('responsive portal (phone viewport)', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('shows the bottom tab bar instead of the desktop nav and tree', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeHidden();
    await expect(page.getByRole('link', { name: 'Test Runs' })).toBeHidden();
  });

  test('navigates between all four tabs', async ({ page }) => {
    await page.goto('/');
    const tabBar = page.getByRole('navigation', { name: 'Primary' });

    await tabBar.getByRole('link', { name: /tests/i }).click();
    await expect(page).toHaveURL(/\/tests$/);
    await expect(page.getByRole('heading', { name: 'Tests' })).toBeVisible();

    await tabBar.getByRole('link', { name: /runs/i }).click();
    await expect(page).toHaveURL(/\/history$/);

    await tabBar.getByRole('link', { name: /admin/i }).click();
    await expect(page).toHaveURL(/\/admin$/);

    await tabBar.getByRole('link', { name: /dashboard/i }).click();
    await expect(page).toHaveURL(/\/$/);
  });

  test('history table renders as stacked cards instead of a table', async ({ page }) => {
    const testName = `Mobile E2E Test ${Date.now()}`;

    await page.goto('/tests');
    await page.getByLabel(/name/i).fill(testName);
    await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
    await page.getByLabel(/virtual users/i).fill('2');
    await page.getByLabel(/duration/i).fill('10');
    await page.getByRole('button', { name: /create test/i }).click();

    const testCard = page.getByRole('listitem').filter({ hasText: testName });
    await expect(testCard).toBeVisible();
    await testCard.getByRole('button', { name: /run/i }).click();
    await expect(page).toHaveURL(/\/runs\/.+/);
    const runId = page.url().split('/runs/')[1];

    await page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: /runs/i }).click();
    await expect(page).toHaveURL(/\/history$/);

    await expect(page.getByRole('table')).toBeHidden();
    const card = page.getByRole('listitem').filter({ hasText: new RegExp(runId, 'i') });
    await expect(card).toBeVisible({ timeout: 15_000 });
  });
});
