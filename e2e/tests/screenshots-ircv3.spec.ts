import * as fs from 'fs';
import * as path from 'path';
import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, openChannel, openSettings } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

/**
 * Generates the screenshots embedded in docs/ircv3-support.md.
 *
 * These are documentation artifacts, not assertions, so they only run when
 * explicitly requested via CASCADE_SCREENSHOTS=1 — that keeps the normal e2e /
 * CI run from rewriting tracked PNGs on every pass. Regenerate intentionally:
 *
 *   cd e2e && CASCADE_SCREENSHOTS=1 npx playwright test tests/screenshots-ircv3.spec.ts
 *
 * Images land in docs/images/ircv3/ (two levels up from this spec). Traffic is
 * produced by a real IrcPeer against the per-run Ergo server, so what's captured
 * is genuine client behavior.
 */

// Minimal structural types so we don't import @playwright/test directly.
type Screenshotable = { screenshot(options: { path: string }): Promise<Buffer> };
type Evaluable = { evaluate(fn: () => void): Promise<void> };

const OUT_DIR = path.resolve(__dirname, '../../docs/images/ircv3');

/** Save a screenshot of `target` (page or element) under docs/images/ircv3/. */
async function shoot(target: Screenshotable, name: string): Promise<void> {
  await target.screenshot({ path: path.join(OUT_DIR, name) });
}

/** Scroll the message list to its oldest entry (CAP negotiation logs live at the top). */
async function scrollMessagesToTop(page: Evaluable): Promise<void> {
  await page.evaluate(() => {
    let el = document.querySelector('[data-testid="message-list"]') as HTMLElement | null;
    while (el) {
      if (el.scrollHeight > el.clientHeight) {
        el.scrollTop = 0;
        return;
      }
      el = el.parentElement;
    }
  });
}

test.describe('IRCv3 documentation screenshots', () => {
  // Documentation artifacts, not assertions: only run when explicitly requested so
  // the normal e2e / CI run doesn't rewrite tracked PNGs on every pass.
  test.skip(!process.env.CASCADE_SCREENSHOTS, 'set CASCADE_SCREENSHOTS=1 to regenerate doc screenshots');

  test.beforeAll(() => {
    fs.mkdirSync(OUT_DIR, { recursive: true });
  });

  test('CAP negotiation in the status buffer', async ({ page, runtime }) => {
    await page.goto(runtime.bridgeUrl);
    await addNetworkAndConnect(page, runtime);
    await selectNetwork(page); // selects the network's status pane

    // CAP LS/REQ/ACK lines are logged to the status buffer at connect time (top of list).
    await expect(
      page.getByTestId('message-list').getByText(/capabilit/i).first(),
    ).toBeVisible({ timeout: 15_000 });
    await scrollMessagesToTop(page);

    await shoot(page.getByTestId('message-list'), 'cap-negotiation.png');
  });

  test('server-time timestamps on messages', async ({ page, runtime }) => {
    await page.goto(runtime.bridgeUrl);
    await addNetworkAndConnect(page, runtime);
    await selectNetwork(page);
    await joinChannel(page, '#cc-servertime');

    const peer = new IrcPeer('localhost', runtime.ergoPort, 'timebot');
    await peer.connect();
    peer.join('#cc-servertime');
    await peer.waitForJoin('#cc-servertime');
    try {
      const lines = ['hey there 👋', 'server-time stamps this line', 'one more for the screenshot'];
      for (const line of lines) peer.say('#cc-servertime', line);
      await expect(
        page.getByTestId('message-list').getByText(lines[lines.length - 1]),
      ).toBeVisible({ timeout: 15_000 });

      await shoot(page.getByTestId('message-list'), 'server-time.png');
    } finally {
      peer.close();
    }
  });

  test('chathistory replay on join', async ({ page, runtime }) => {
    await page.goto(runtime.bridgeUrl);
    await addNetworkAndConnect(page, runtime);
    await selectNetwork(page);

    // Peer populates the channel BEFORE the UI user joins, so the UI user receives
    // these lines via CHATHISTORY replay rather than live traffic.
    const peer = new IrcPeer('localhost', runtime.ergoPort, 'histbot');
    await peer.connect();
    peer.join('#cc-history');
    await peer.waitForJoin('#cc-history');
    try {
      for (let i = 1; i <= 6; i++) peer.say('#cc-history', `earlier message #${i} (sent before you joined)`);
      // Let Ergo persist the lines into channel history before we join: the client
      // fetches CHATHISTORY exactly once, on the initial join, so that single fetch
      // must happen after the messages are stored.
      await page.waitForTimeout(2_500);

      await joinChannel(page, '#cc-history'); // triggers RequestChatHistoryLatest

      // The replay arrives as a BATCH and is written to the DB asynchronously,
      // after the pane's initial load. Give it a moment, then re-open the pane so
      // it reloads from storage and renders the backfilled history.
      await page.waitForTimeout(2_500);
      await selectNetwork(page); // switch away to the status pane
      await openChannel(page, '#cc-history'); // switch back -> reloads messages from DB

      await expect(
        page.getByTestId('message-list').getByText('earlier message #6 (sent before you joined)'),
      ).toBeVisible({ timeout: 15_000 });

      await shoot(page.getByTestId('message-list'), 'chathistory.png');
    } finally {
      peer.close();
    }
  });

  test('SASL configuration in network settings', async ({ page, runtime }) => {
    await page.goto(runtime.bridgeUrl);
    const settings = await openSettings(page, runtime, 'networks');
    try {
      // Open a fresh add-network form; we configure SASL for the screenshot but never save,
      // so the shared-DB suite's single-network assumption is preserved.
      await settings.getByTestId('add-network-button').waitFor({ state: 'visible', timeout: 10_000 });
      await settings.getByTestId('add-network-button').click();
      await settings.getByTestId('network-name-input').fill('Libera.Chat');
      await settings.getByTestId('server-address-input').fill('irc.libera.chat');
      await settings.getByTestId('server-port-input').fill('6697');
      await settings.getByTestId('network-nickname-input').fill('cascade-user');

      // Reveal and fill the SASL section.
      await settings.getByText('Enable SASL').click();
      await settings.getByText('Select mechanism...').click();
      await settings.getByRole('option', { name: 'SCRAM-SHA-256' }).click();
      await settings.getByPlaceholder('SASL username').fill('cascade-user');
      await settings.getByPlaceholder('SASL password').fill('••••••••••');

      const saslHeading = settings.getByText('SASL Authentication');
      await saslHeading.scrollIntoViewIfNeeded();
      // The chosen mechanism shows in the trigger's value span (unique, unlike the
      // bare text which also matches the hidden native <option>).
      await expect(settings.locator('[data-slot="select-value"]')).toHaveText('SCRAM-SHA-256');

      await shoot(settings, 'sasl-settings.png');
    } finally {
      await settings.close();
    }
  });

  test('WHOIS account name in the user info panel', async ({ page, runtime }) => {
    // RPL_WHOISACCOUNT (330) only appears for a user logged into a services account,
    // and the test Ergo starts with none — so register one at runtime. With
    // email-verification disabled and allow-before-connect enabled, NickServ REGISTER
    // completes instantly and logs the peer in, so a later WHOIS reports its account.
    const peer = new IrcPeer('localhost', runtime.ergoPort, 'alice');
    await peer.connect();
    peer.sendRaw('PRIVMSG NickServ :REGISTER s3cret-passw0rd alice@example.com');
    await peer.waitForLine(/ 900 |logged in as|registered/i);
    peer.join('#cc-account');
    await peer.waitForJoin('#cc-account');
    try {
      await page.goto(runtime.bridgeUrl);
      await addNetworkAndConnect(page, runtime);
      await selectNetwork(page);
      await joinChannel(page, '#cc-account');

      // Right-click the member in the user list, then choose Whois to open the panel.
      const member = page.getByTestId('channel-user-list').getByText('alice', { exact: true });
      await member.waitFor({ state: 'visible', timeout: 15_000 });
      await member.click({ button: 'right' });
      await page.getByRole('button', { name: 'Whois' }).click();

      // The panel renders the account name from RPL_WHOISACCOUNT (330).
      const panel = page.getByTestId('user-info-panel');
      await expect(panel.getByText(/Account:/)).toBeVisible({ timeout: 15_000 });

      await shoot(panel, 'whois-account.png');
    } finally {
      peer.close();
    }
  });

  test('invite-notify shows a clickable invite in the status buffer', async ({ page, runtime }) => {
    // A peer in a channel invites the UI user; with invite-notify the INVITE is
    // surfaced in the status buffer as a clickable join line.
    const peer = new IrcPeer('localhost', runtime.ergoPort, 'invbot');
    await peer.connect();
    peer.join('#cc-invite');
    await peer.waitForJoin('#cc-invite');
    try {
      await page.goto(runtime.bridgeUrl);
      await addNetworkAndConnect(page, runtime);
      await selectNetwork(page); // status pane, where the invite lands

      peer.sendRaw('INVITE e2euser #cc-invite');

      await expect(
        page.getByTestId('message-list').getByText(/invited you to #cc-invite/i),
      ).toBeVisible({ timeout: 15_000 });

      await shoot(page.getByTestId('message-list'), 'invite-notify.png');
    } finally {
      peer.close();
    }
  });

  // Note: standard-replies (FAIL/WARN/NOTE) has no screenshot here — triggering a
  // deterministic server standard-reply through the UI isn't reliable, and the
  // rendering reuses the existing error/warning/status message styles (documented
  // in prose in docs/ircv3-support.md). Unit coverage is in
  // internal/irc/standard_replies_test.go.

  test('monitor buddy list shows online presence', async ({ page, runtime }) => {
    // A peer stays connected so it reads as online once monitored.
    const peer = new IrcPeer('localhost', runtime.ergoPort, 'buddybot');
    await peer.connect();
    try {
      await page.goto(runtime.bridgeUrl);
      await addNetworkAndConnect(page, runtime);
      await selectNetwork(page);
      await joinChannel(page, '#cc-monitor'); // a channel so the right sidebar shows

      // Switch the right sidebar to the Buddies tab and add the peer.
      await page.getByTestId('right-sidebar').getByText('Buddies', { exact: true }).click();
      await page.getByPlaceholder('Add nick…').fill('buddybot');
      await page.getByTestId('monitor-list').getByRole('button', { name: 'Add' }).click();

      // The buddy appears in the pane and is flagged online (730 RPL_MONONLINE).
      await expect(
        page.getByTestId('monitor-list').getByText('buddybot', { exact: true }),
      ).toBeVisible({ timeout: 15_000 });
      await page
        .getByTestId('monitor-list')
        .getByTitle('Online')
        .first()
        .waitFor({ state: 'visible', timeout: 10_000 });

      await shoot(page.getByTestId('monitor-list'), 'monitor.png');
    } finally {
      peer.close();
    }
  });
});
