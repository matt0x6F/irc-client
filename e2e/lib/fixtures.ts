import { test as base, expect } from '@playwright/test';
import { readRuntime, Runtime } from './runtime';

// Workers start AFTER globalSetup, so .runtime.json exists by import time.
const runtime: Runtime = readRuntime();

export const test = base.extend<{ runtime: Runtime }>({
  runtime: async ({}, use) => {
    await use(runtime);
  },
});

export { expect, runtime };
