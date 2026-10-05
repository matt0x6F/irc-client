import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

// Reproduces: after a runtime `/nick`, the header nick chip stays stale.
//
// The header chip (data-testid="current-nick") reads the live nick the server
// assigned us. Changing nick via `/nick` should flow: input -> SendCommand ->
// raw NICK -> server echo -> handleNickMessage -> current-nick event -> store ->
// chip. This test drives that whole path through the real UI and asserts the
// chip reflects the new nick.
test('header nick chip updates after /nick', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime); // connects as the default nick "e2euser"
  await selectNetwork(page);

  const chip = page.getByTestId('current-nick');
  // Sanity: the chip shows the nick we registered with.
  await expect(chip).toContainText('e2euser', { timeout: 15_000 });

  const newNick = `renamed${Date.now() % 100000}`;
  const input = page.getByTestId('message-input');

  try {
    await input.click();
    await input.fill(`/nick ${newNick}`);
    await input.press('Enter');

    // The header chip must reflect the server-assigned nick after the change.
    await expect(chip).toContainText(newNick, { timeout: 15_000 });
  } finally {
    // Restore the shared connection's nick so later specs see "e2euser".
    await input.click();
    await input.fill('/nick e2euser');
    await input.press('Enter');
    await expect(chip).toContainText('e2euser', { timeout: 15_000 }).catch(() => {});
  }
});

// Reproduces: after a runtime `/nick`, our own entry in the channel member list
// ("side bar", right Users panel) keeps showing the old nick.
//
// The member list refreshes on a `user.nick` message-event. If the backend never
// forwards EventUserNick to the frontend, the list never re-queries the (already
// renamed) DB rows, so the stale nick lingers in the side bar.
test('member list updates own nick after /nick', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime); // connects as "e2euser"
  await selectNetwork(page);
  await joinChannel(page, '#e2e'); // opens the channel; right Users panel is the default tab

  const userList = page.getByTestId('channel-user-list');
  // Our nick is present in the member list once NAMES lands.
  await expect(userList).toContainText('e2euser', { timeout: 20_000 });

  const newNick = `member${Date.now() % 100000}`;
  const input = page.getByTestId('message-input');

  try {
    await input.click();
    await input.fill(`/nick ${newNick}`);
    await input.press('Enter');

    // The side bar must show the renamed entry and drop the old one.
    await expect(userList).toContainText(newNick, { timeout: 15_000 });
    await expect(userList).not.toContainText('e2euser', { timeout: 15_000 });
  } finally {
    await input.click();
    await input.fill('/nick e2euser');
    await input.press('Enter');
    await expect(userList).toContainText('e2euser', { timeout: 15_000 }).catch(() => {});
  }
});

// Reproduces: after a self /nick, the member list no longer recognizes our own
// (renamed) row as "self". Self-detection compares each row to "who am I", which
// must track the live nick — not the stale configured network.nickname. The
// observable signal is the row's context menu: operating on yourself is blocked.
test('member list recognizes own renamed row as self', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime); // connects as "e2euser"
  await selectNetwork(page);
  await joinChannel(page, '#e2e');

  const userList = page.getByTestId('channel-user-list');
  await expect(userList).toContainText('e2euser', { timeout: 20_000 });

  const newNick = `self${Date.now() % 100000}`;
  const input = page.getByTestId('message-input');

  try {
    await input.click();
    await input.fill(`/nick ${newNick}`);
    await input.press('Enter');
    await expect(userList).toContainText(newNick, { timeout: 15_000 });

    // Right-click our own (renamed) row: the menu must recognize us as self.
    await userList.getByText(newNick, { exact: true }).click({ button: 'right' });
    await expect(page.getByText('Cannot operate on yourself')).toBeVisible({ timeout: 10_000 });
  } finally {
    await page.keyboard.press('Escape');
    await input.click();
    await input.fill('/nick e2euser');
    await input.press('Enter');
    await expect(userList).toContainText('e2euser', { timeout: 15_000 }).catch(() => {});
  }
});

test('direct messages follow a seasonal nick change and keep presence, history, and replies', async ({ page, runtime }) => {
  await page.setViewportSize({ width: 1200, height: 440 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#e2e');

  const suffix = Date.now() % 100000;
  const oldNick = `autumn${suffix}`;
  const newNick = `Spooky${suffix}`;
  const peer = new IrcPeer('localhost', runtime.ergoPort, oldNick);
  await peer.connect();
  try {
    const joined = peer.waitForJoin('#e2e');
    peer.join('#e2e');
    await joined;
    peer.say('e2euser', `before-rename-${suffix}`);
    const sidebar = page.getByTestId('channel-panel');
    const oldRow = sidebar.locator(`[data-testid="pm-node"][data-peer="${oldNick}"]`);
    await oldRow.click();
    await expect(oldRow.getByTitle('Online', { exact: true })).toBeVisible();
    await expect(page.getByTestId('message-list').getByText(`before-rename-${suffix}`, { exact: true })).toBeVisible();

    // Renaming a conversation must not snap someone reading earlier messages
    // back to the newest line, as ordinary pane navigation does.
    const list = page.getByTestId('message-list');
    const pad = 'autumn leaves '.repeat(15).trim();
    for (let i = 0; i < 20; i++) peer.say('e2euser', `history-${suffix}-${i} ${pad}`);
    await expect(list.getByText(`history-${suffix}-19 ${pad}`, { exact: true })).toBeAttached();
    await expect.poll(() => list.evaluate((el) => el.scrollHeight - el.clientHeight)).toBeGreaterThan(100);
    await list.evaluate((el) => el.scrollTo({ top: 0 }));
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible();

    const renamed = peer.waitForLine(new RegExp(` NICK :?${newNick}$`));
    peer.sendRaw(`NICK ${newNick}`);
    await renamed;
    const newRow = sidebar.locator(`[data-testid="pm-node"][data-peer="${newNick}" i]`);
    await expect(newRow.getByTitle('Online', { exact: true })).toBeVisible();
    await expect(oldRow).toHaveCount(0);
    await expect(newRow).toHaveClass(/cc-active-pane/);
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible();
    await expect.poll(() => list.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeGreaterThan(100);
    await expect(page.getByTestId('message-list').getByText(`before-rename-${suffix}`, { exact: true })).toBeVisible();

    const reply = `reply-after-rename-${suffix}`;
    const received = peer.waitForLine(new RegExp(` PRIVMSG ${newNick} :${reply}$`));
    const input = page.getByTestId('message-input');
    await input.fill(reply);
    await input.press('Enter');
    await received;

    // Saved conversation routing and history must survive a frontend reload.
    await page.reload();
    await expect(newRow.getByTitle('Online', { exact: true })).toBeVisible();
    await expect(oldRow).toHaveCount(0);
    await newRow.click();
    await list.evaluate((el) => el.scrollTo({ top: 0 }));
    await expect(page.getByTestId('message-list').getByText(`before-rename-${suffix}`, { exact: true })).toBeVisible();
    peer.close();
    await expect(newRow.getByTitle('Presence unknown', { exact: true })).toBeVisible();
  } finally {
    peer.close();
  }
});
