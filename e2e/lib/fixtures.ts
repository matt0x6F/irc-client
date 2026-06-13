import { test as base, expect } from '@playwright/test';
import { readRuntime, Runtime } from './runtime';

// Read lazily: `playwright test --list` imports this module WITHOUT running
// globalSetup, so .runtime.json may not exist yet. Deferring the read into the
// fixture means it only happens during actual test execution (workers start
// after globalSetup has written the descriptor).
let cached: Runtime | undefined;
function getRuntime(): Runtime {
  if (!cached) cached = readRuntime();
  return cached;
}

export const test = base.extend<{ runtime: Runtime }>({
  runtime: async ({}, use) => {
    await use(getRuntime());
  },
});

export { expect };
