import { test, expect } from '@playwright/test';

test('portal shell renders top nav, tree nav, and breadcrumb on the dashboard', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('banner').getByText('BoltRunner')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Test Runs' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible();
});

test('theme toggle switches to dark mode and persists across reload', async ({ page }) => {
  await page.goto('/');
  const toggle = page.getByRole('button', { name: /toggle theme/i });
  await toggle.click();
  await expect(page.locator('html')).toHaveClass(/dark/);

  await page.reload();
  await expect(page.locator('html')).toHaveClass(/dark/);
});

test('history page lists a real run and links to its detail page', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel(/name/i).fill('History E2E Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('2');
  await page.getByLabel(/duration/i).fill('10');
  await page.getByRole('button', { name: /create test/i }).click();

  const row = page.getByRole('row', { name: /History E2E Test/i });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /run/i }).click();
  await expect(page).toHaveURL(/\/runs\/.+/);
  const runId = page.url().split('/runs/')[1];

  await page.getByRole('link', { name: 'Test Runs' }).click();
  await expect(page).toHaveURL(/\/history/);
  const historyRow = page.getByRole('row', { name: new RegExp(runId, 'i') });
  await expect(historyRow).toBeVisible({ timeout: 15_000 });
  await historyRow.click();
  await expect(page).toHaveURL(new RegExp(`/runs/${runId}`));
});

test('admin page renders with the API base URL', async ({ page }) => {
  await page.goto('/admin');
  await expect(page.getByText(/API base URL/i)).toBeVisible();
  await expect(page.getByRole('button', { name: /toggle theme/i })).toBeVisible();
});

test('workspace switcher shows Default checked and a disabled New project action', async ({ page }) => {
  await page.goto('/');
  const trigger = page.getByRole('button', { name: /default/i });
  await trigger.click();
  await expect(page.getByRole('menuitemradio', { name: /default/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /new project/i })).toBeDisabled();

  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();
});
