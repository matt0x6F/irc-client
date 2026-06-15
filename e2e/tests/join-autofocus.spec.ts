import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, openChannel } from '../lib/actions';

// Regression coverage for the post-join auto-focus overriding a manual switch.
//
// Issuing `/join #x` arms a deferred selectPane (App.tsx) that auto-focuses the
// newly joined channel ~300ms after the server confirms the join. Previously that
// deferred fired unconditionally, so if the user manually switched to another
// channel inside that window, the pane was yanked back to the just-joined channel.
//
// The fix makes the auto-focus yield to manual navigation: it only fires if the
// user is still on the channel they issued `/join` from. This test issues a join
// and immediately switches to a different channel, then asserts the manual choice
// survives the deferred window.

let execCount = 0;

test('a manual channel switch is not overridden by the just-joined channel auto-focus', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const ORIGIN = `#origin${run}`; // where /join is issued from
  const STAY = `#stay${run}`; // channel we manually switch to and must remain on
  const JOINED = `#joined${run}`; // freshly joined channel whose auto-focus must not win

  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  // Pre-join the two existing channels; end focused on ORIGIN.
  await joinChannel(page, STAY);
  await joinChannel(page, ORIGIN);

  // Let the auto-focus from those joins settle before the timing-sensitive part.
  await page.waitForTimeout(600);
  await expect(page.getByTestId('active-channel-name')).toHaveText(ORIGIN);

  // From ORIGIN, issue /join for a brand-new channel, then immediately switch to
  // STAY — inside the ~300ms window before the join's deferred auto-focus fires.
  const input = page.getByTestId('message-input');
  await input.click();
  await input.fill(`/join ${JOINED}`);
  await input.press('Enter');
  await openChannel(page, STAY);

  // Wait out the deferred auto-focus window.
  await page.waitForTimeout(800);

  // REGRESSION: the manual switch wins — we stay on STAY, not snapped to JOINED.
  await expect(page.getByTestId('active-channel-name')).toHaveText(STAY);
});

test('issuing /join auto-focuses the new channel when the user stays put', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const ORIGIN = `#origin${run}`;
  const NEW = `#new${run}`;

  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  await joinChannel(page, ORIGIN);
  await page.waitForTimeout(600); // let prior auto-focus settle
  await expect(page.getByTestId('active-channel-name')).toHaveText(ORIGIN);

  // Issue /join and DON'T navigate: the deferred auto-focus should still switch us.
  const input = page.getByTestId('message-input');
  await input.click();
  await input.fill(`/join ${NEW}`);
  await input.press('Enter');

  await expect(page.getByTestId('active-channel-name')).toHaveText(NEW, { timeout: 10_000 });
});
