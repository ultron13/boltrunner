import { test, expect } from '@playwright/test';

test('create a project, switch to it, and scope tests to it', async ({ page }) => {
  // Timestamped because the database outlives a run, and project names are
  // unique -- a fixed name would 409 on the second run.
  const project = `E2E Project ${Date.now()}`;
  const testName = `E2E Scoped Test ${Date.now()}`;
  await page.goto('/');

  await page.getByRole('button', { name: /default/i }).click();
  await page.getByRole('button', { name: /new project/i }).click();
  const input = page.getByRole('textbox', { name: /project name/i });
  await input.fill(project);
  await input.press('Enter');

  // The switcher now reads the new project, and it starts empty.
  await expect(page.getByRole('button', { name: new RegExp(project, 'i') })).toBeVisible();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toHaveCount(0);

  await page.getByLabel(/name/i).fill(testName);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('2');
  await page.getByLabel(/duration/i).fill('10');
  await page.getByRole('button', { name: /create test/i }).click();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toBeVisible();

  // Switching back to Default must not show the other project's test.
  await page.getByRole('button', { name: new RegExp(project, 'i') }).click();
  await page.getByRole('menuitemradio', { name: /^\s*✓?\s*Default$/i }).click();
  await expect(page.getByRole('button', { name: /default/i })).toBeVisible();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toHaveCount(0);
});

test('rejects a duplicate project name without losing what was typed', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: /default/i }).click();
  await page.getByRole('button', { name: /new project/i }).click();
  const input = page.getByRole('textbox', { name: /project name/i });
  await input.fill('Default');
  await input.press('Enter');

  await expect(page.getByText(/already exists/i)).toBeVisible();
  await expect(page.getByRole('textbox', { name: /project name/i })).toHaveValue('Default');
});
