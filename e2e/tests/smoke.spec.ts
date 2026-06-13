import { test, expect } from '../lib/fixtures';

test('app loads through the wails dev bridge with bindings available', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);

  // The React root renders something.
  await expect(page.locator('#root')).toBeVisible();

  // window.go bindings are wired up (proves the bridge → Go backend path).
  const hasBindings = await page.evaluate(() => {
    // @ts-ignore - injected by the wails runtime
    return typeof window.go?.main?.App?.GetNetworks === 'function';
  });
  expect(hasBindings).toBe(true);
});
