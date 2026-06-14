import { spawnSync } from 'child_process';
import * as fs from 'fs';
import { readRuntime, clearRuntime } from './lib/runtime';

export default async function globalTeardown(): Promise<void> {
  let rt;
  try {
    rt = readRuntime();
  } catch {
    return; // setup never completed; nothing to tear down
  }

  // 1. Kill the wails dev process group (Go app + vite watcher + build procs).
  try {
    process.kill(-rt.wailsPid, 'SIGTERM');
  } catch { /* already gone */ }

  // 2. Tear down Ergo + its volume.
  spawnSync('docker', ['compose', '-p', rt.composeProject, 'down', '-v'], { stdio: 'inherit' });

  // 3. Remove the isolated data dir.
  try {
    fs.rmSync(rt.dataDir, { recursive: true, force: true });
  } catch { /* best effort */ }

  clearRuntime();
}
