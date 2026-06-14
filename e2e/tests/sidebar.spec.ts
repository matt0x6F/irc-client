import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
import type { Page } from '@playwright/test';

// Sidebar collapse/expand + window-resize behavior.
//
// Regression coverage for two coupled layout bugs:
//   #1 The responsive effect rewrote both sidebar states on *every* resize tick,
//      so growing the window re-opened a sidebar the user had manually collapsed.
//   #2 The flex layout had no horizontal-overflow containment, so an open sidebar
//      in a too-narrow window spilled the layout past the viewport edges.
//
// The app exposes data-collapsed on the sidebar wrappers, which is the
// authoritative state signal (independent of width-transition timing).

const WIDE = { width: 1100, height: 800 } as const; // >= 768 → sidebars expanded
const NARROW = { width: 600, height: 800 } as const; // <  768 → sidebars collapsed

/** Positive = layout is wider than the viewport (horizontal overflow). */
function horizontalOverflow(page: Page): Promise<number> {
  return page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
}

test.describe('sidebar + window expansion behavior', () => {
  test.beforeEach(async ({ page, runtime }) => {
    // Mount wide so both sidebars start expanded, then open a channel so the
    // right (channel-info) sidebar is in play too.
    await page.setViewportSize(WIDE);
    await page.goto(runtime.bridgeUrl);
    await addNetworkAndConnect(page, runtime);
    await selectNetwork(page);
    await joinChannel(page, '#e2e');
  });

  test('auto-collapses both sidebars below the 768px breakpoint', async ({ page }) => {
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'false');
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'false');

    await page.setViewportSize(NARROW);

    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'true');
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'true');
  });

  test('auto-expands both sidebars when crossing back above 768px', async ({ page }) => {
    await page.setViewportSize(NARROW);
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'true');

    await page.setViewportSize(WIDE);

    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'false');
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'false');
  });

  test('manual collapse survives an in-band resize (regression #1: no clobber)', async ({ page }) => {
    await page.setViewportSize({ width: 1000, height: 800 });
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'false');

    // Manually collapse the left sidebar.
    await page.getByTestId('toggle-left-sidebar').click();
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'true');

    // Grow the window while staying >= 768 (no breakpoint crossing).
    await page.setViewportSize({ width: 1300, height: 800 });

    // Wait until the resize is actually processed (innerWidth reflects it), then
    // let React settle — so a buggy per-tick handler would have had its chance to
    // re-open the sidebar. Without this wait the assertion races the resize and
    // can pass on the stale pre-resize value.
    await expect.poll(() => page.evaluate(() => window.innerWidth)).toBe(1300);
    await page.waitForTimeout(300);

    // Pre-fix, the resize handler force-rewrote collapsed=false here, re-opening it.
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'true');
  });

  test('right sidebar toggles independently of the left', async ({ page }) => {
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'false');

    await page.getByTestId('toggle-right-sidebar').click();
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'true');
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'false');

    await page.getByTestId('toggle-right-sidebar').click();
    await expect(page.getByTestId('right-sidebar')).toHaveAttribute('data-collapsed', 'false');
  });

  test('an open sidebar wider than the window never causes body overflow (regression #2: containment)', async ({ page }) => {
    // Window narrower than the 256px sidebar. The responsive logic auto-collapses
    // it; force it open to recreate the inconsistent state a manual toggle makes.
    await page.setViewportSize({ width: 240, height: 800 });
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'true');

    await page.getByTestId('toggle-left-sidebar').click();
    await expect(page.getByTestId('left-sidebar')).toHaveAttribute('data-collapsed', 'false');

    // Pre-fix the 256px fixed sidebar spilled past the 240px viewport (body scrolled
    // sideways); overflow containment clips it so the page never scrolls horizontally.
    await expect
      .poll(() => horizontalOverflow(page), { message: 'horizontal overflow at width=240' })
      .toBeLessThanOrEqual(0);
  });

  test('layout fits the viewport across a range of widths (invariant)', async ({ page }) => {
    for (const width of [1300, 1000, 820, 768, 600]) {
      await page.setViewportSize({ width, height: 800 });
      await expect
        .poll(() => horizontalOverflow(page), { message: `horizontal overflow at width=${width}` })
        .toBeLessThanOrEqual(0);
    }
  });

  test('left resize handle drags to widen the sidebar', async ({ page }) => {
    const sidebar = page.getByTestId('left-sidebar');
    const before = (await sidebar.boundingBox())!.width;

    const handle = page.getByTestId('left-resize-handle');
    const box = (await handle.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + 80, box.y + box.height / 2, { steps: 8 });
    await page.mouse.up();

    const after = (await sidebar.boundingBox())!.width;
    expect(after).toBeGreaterThan(before);
  });
});
