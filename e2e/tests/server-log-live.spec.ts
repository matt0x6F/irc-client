import { test, expect } from '../lib/fixtures';
import {
  openSettings,
  addNetwork,
  selectNetwork,
  connectViaContextMenu,
  disconnectViaContextMenu,
  deleteNetwork,
  networkIndicator,
} from '../lib/actions';

// Regression test for the server-log pane not updating while open. Status-buffer
// rows (server notices, MOTD, connect/disconnect markers, CAP lines, ...) were
// written straight to SQLite with no event emission, and the frontend refresh is
// fully event-driven — so the open pane only caught up when re-selected. Every
// status write now goes through writeStatusBuffer / writeNetworkStatus, which
// emit status.message; the message-event handler routes the target-less event to
// the open status pane and reloads it.
//
// Uses a dedicated network + nick (the suite shares one backend DB per run) and
// deletes it afterward, pass or fail, to leave the shared world untouched.
const NAME = 'e2e-slog';

test.afterEach(async ({ page }) => {
  await deleteNetwork(page, NAME).catch(() => {});
});

test('server log updates live while the pane stays open', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  // Add via Settings but connect via the context menu: the connect() helper's
  // "wait for a green indicator" fast-path matches the shared `e2e` network when
  // the whole suite runs, closing Settings while this network's ConnectNetwork
  // RPC is still in flight. connectViaContextMenu waits on THIS network's row.
  const settings = await openSettings(page, runtime, 'networks');
  await addNetwork(settings, runtime, { name: NAME, nick: 'e2eslog' });
  await settings.close();
  await connectViaContextMenu(page, NAME);

  // Open the server log; the connect-time lines are visible via the pane-open load.
  await selectNetwork(page, NAME);
  const list = page.getByTestId('message-list');
  await expect(list.getByText('Connected to server').first()).toBeVisible({ timeout: 10_000 });

  // Disconnect WITHOUT leaving the pane: the "Disconnected from server" line must
  // appear live. Before the fix the row was stored but the open pane never
  // reloaded, so it only surfaced after switching away and back.
  await disconnectViaContextMenu(page, NAME);
  await expect(networkIndicator(page, NAME, false)).toBeVisible({ timeout: 10_000 });
  await expect(list.getByText('Disconnected from server').first()).toBeVisible({ timeout: 10_000 });

  // Reconnect, still watching: the app-side connect-flow line lands live too
  // (writeNetworkStatus path, distinct from the IRC-client path above). The list
  // is virtualized and pinned to the bottom, so assert on the success line that
  // stays at the tail — earlier lines ("Connecting to…") get scrolled out of the
  // rendered window by the MOTD/CAP flood.
  await connectViaContextMenu(page, NAME);
  await expect(list.getByText(`Connected to localhost:${runtime.ergoPort}`).first()).toBeVisible({
    timeout: 10_000,
  });
});
