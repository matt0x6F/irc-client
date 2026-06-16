import { test, expect } from '../lib/fixtures';

// Wails v3 has no v2 `window.go.*` global: bindings are ES-module imports that
// dispatch HTTP calls to the server-mode binary's `/wails/runtime` endpoint.
// This smoke test proves the whole bridge end-to-end — the app boots, and the
// binding call the store fires on mount (GetNetworks) round-trips to the Go
// backend with a 200.
test('app loads and the v3 binding bridge serves runtime calls', async ({ page, runtime }) => {
  // The network store calls GetNetworks on mount (and polls it), so a
  // /wails/runtime POST is guaranteed shortly after load. Arm the wait BEFORE
  // navigating so the call can't be missed.
  const runtimeCall = page.waitForResponse(
    (r) => r.url().endsWith('/wails/runtime') && r.request().method() === 'POST',
    { timeout: 20_000 },
  );

  await page.goto(runtime.bridgeUrl);

  // The React root renders something.
  await expect(page.locator('#root')).toBeVisible();

  // The binding call reached the Go backend and succeeded (proves the
  // frontend → /wails/runtime → bound service path).
  const resp = await runtimeCall;
  expect(resp.status()).toBe(200);
});
