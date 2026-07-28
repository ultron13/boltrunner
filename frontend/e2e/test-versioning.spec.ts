import { test, expect } from '@playwright/test';

test('edit a test and see the new version in its history', async ({ page }) => {
  const name = `E2E Versioning ${Date.now()}`;
  await page.goto('/');

  await page.getByLabel(/name/i).fill(name);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('20');
  await page.getByRole('button', { name: /create test/i }).click();

  const row = page.getByRole('row', { name: new RegExp(name, 'i') });
  await expect(row).toBeVisible();
  await row.getByRole('link', { name }).click();

  await expect(page).toHaveURL(/\/tests\/.+/);
  await expect(page.getByRole('row', { name: /v1/ })).toBeVisible();

  await page.getByLabel(/virtual users/i).fill('7');
  await page.getByRole('button', { name: /save as new version/i }).click();

  await expect(page.getByRole('row', { name: /v2/ })).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('row', { name: /v1/ })).toBeVisible();
  await expect(page.getByLabel(/virtual users/i)).toHaveValue('7');
});
