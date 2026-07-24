import { test, expect } from '@playwright/test';

test('create a test, run it, watch live metrics, see completion', async ({ page }) => {
  await page.goto('/');

  await page.getByLabel(/name/i).fill('E2E Smoke Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('20');
  await page.getByRole('button', { name: /create test/i }).click();

  const row = page.getByRole('row', { name: /E2E Smoke Test/i });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /run/i }).click();

  await expect(page).toHaveURL(/\/runs\/.+/);
  await expect(page.getByText(/status: (pending|running)/i)).toBeVisible({ timeout: 15_000 });

  // Live metrics should start populating within a few seconds of the run starting.
  await expect(page.getByText(/throughput/i)).toBeVisible();

  // Run duration is 20s; allow generous headroom for pod scheduling + JMeter startup.
  await expect(page.getByText(/status: completed/i)).toBeVisible({ timeout: 90_000 });
});

test('cancel a running test stops it', async ({ page }) => {
  await page.goto('/');

  await page.getByLabel(/name/i).fill('E2E Cancel Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('60');
  await page.getByRole('button', { name: /create test/i }).click();
  const row = page.getByRole('row', { name: /E2E Cancel Test/i });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /run/i }).click();

  await expect(page).toHaveURL(/\/runs\/.+/);
  await expect(page.getByText(/status: (pending|running)/i)).toBeVisible({ timeout: 15_000 });

  await page.getByRole('button', { name: /cancel/i }).click();

  await expect(page.getByText(/status: stopped/i)).toBeVisible({ timeout: 15_000 });
});
