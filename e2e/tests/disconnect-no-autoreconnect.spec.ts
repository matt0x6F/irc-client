import { test, expect } from '../lib/fixtures';
import {
  openSettings,
  addNetwork,
  toggleAutoConnect,
  readAutoConnect,
  connectViaContextMenu,
  disconnectViaContextMenu,
  networkIndicator,
  deleteNetwork,
} from '../lib/actions';

// Regression test for the bug where hitting Disconnect on an auto-connect
// network immediately reconnected it. The deliberate teardown rode the same
// EventConnectionLost path as an unexpected drop, so handleConnectionLost — which
// only gated on auto_connect — reconnected right after the user disconnected. The
// fix flags the teardown as intentional so that path is skipped once.
//
// Like auto-connect-persist.spec, this shares one backend DB with the rest of the
// suite, so it uses a dedicated `e2e-dc` network + nick and deletes it afterward
// (pass or fail) to leave the shared world untouched.
const NAME = 'e2e-dc';

test.afterEach(async ({ page }) => {
  await deleteNetwork(page, NAME).catch(() => {});
});

test('disconnecting an auto-connect network does not auto-reconnect', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  // Add the network, enable auto-connect, and connect.
  const settings = await openSettings(page, runtime, 'networks');
  await addNetwork(settings, runtime, { name: NAME, nick: 'e2edc' });
  await settings.close();

  await toggleAutoConnect(page, NAME);
  expect(await readAutoConnect(page, NAME)).toBe(true);

  await connectViaContextMenu(page, NAME);

  // Hit Disconnect. The indicator must go grey and STAY grey: a deliberate
  // disconnect must not trip auto-reconnect even with auto_connect enabled.
  await disconnectViaContextMenu(page, NAME);

  // Dwell well past a reconnect's cleanup + connect window (cleanup is 500ms and
  // the local Ergo connect is near-instant), then assert no green indicator
  // reappeared for this network.
  await page.waitForTimeout(6_000);
  await expect(networkIndicator(page, NAME, true)).toHaveCount(0);
  await expect(networkIndicator(page, NAME, false)).toBeVisible();

  // The auto_connect preference itself must be untouched — suppression is a
  // one-shot guard, not a silent disable. A later startup/manual reconnect still
  // honors it.
  expect(await readAutoConnect(page, NAME)).toBe(true);
});
