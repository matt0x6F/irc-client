import * as fs from 'fs';
import * as path from 'path';
import { test, expect } from '../lib/fixtures';
import { openSettings } from '../lib/actions';

/**
 * Generates the screenshots embedded in docs/public/scripting/managing-scripts.md.
 *
 * Documentation artifacts, not assertions — only run when explicitly requested
 * via CASCADE_SCREENSHOTS=1 so the normal e2e/CI run doesn't rewrite tracked
 * PNGs. Regenerate intentionally:
 *
 *   cd e2e && CASCADE_SCREENSHOTS=1 npx playwright test tests/screenshots-scripting.spec.ts
 *
 * Images land in docs/public/scripting/images/ (two levels up from this spec).
 */

const OUT_DIR = path.resolve(__dirname, '../../docs/public/scripting/images');

test.describe('Scripting documentation screenshots', () => {
  test.skip(!process.env.CASCADE_SCREENSHOTS, 'set CASCADE_SCREENSHOTS=1 to regenerate doc screenshots');

  test.beforeAll(() => {
    fs.mkdirSync(OUT_DIR, { recursive: true });
  });

  test('Scripts panel with a created script', async ({ page, runtime }) => {
    // openSettings opens a new page at ?view=settings&section=scripts.
    const settings = await openSettings(page, runtime, 'scripts');

    // Create a script so the panel is populated (not the empty state).
    await settings.getByPlaceholder(/script name/i).fill('greeter');
    await settings.getByRole('button', { name: /^new script$/i }).click();

    // Wait for the row to appear (hot-reload loads the new script).
    // Use getByRole to avoid strict-mode collision with the file path shown in the
    // "Reveal in folder" banner, which also contains the text "greeter".
    await expect(settings.getByRole('heading', { name: 'greeter' })).toBeVisible({ timeout: 15_000 });

    await settings.getByTestId('scripts-panel').screenshot({
      path: path.join(OUT_DIR, 'scripts-panel.png'),
    });
  });
});
