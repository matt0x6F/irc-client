import { spawn, spawnSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { getFreePorts } from './lib/ports';
import { writeRuntime, repoRoot, Runtime } from './lib/runtime';
import { waitForTcp, waitForHttp200 } from './lib/wait';

export default async function globalSetup(): Promise<void> {
  const [bridgePort, vitePort, ergoPort, ergoTlsPort] = await getFreePorts(4);
  const id = String(bridgePort);
  const composeProject = `cascade-e2e-${id}`;
  const dataDir = path.join(os.tmpdir(), 'cascade-e2e', id);
  const logFile = path.join(os.tmpdir(), 'cascade-e2e', `${id}.wails.log`);
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(path.dirname(logFile), { recursive: true });

  // 1. Bring up Ergo on the allocated port.
  const composeEnv = { ...process.env, ERGO_PORT: String(ergoPort), ERGO_TLS_PORT: String(ergoTlsPort) };
  const up = spawnSync('docker', ['compose', '-p', composeProject, 'up', '-d'], {
    cwd: repoRoot, env: composeEnv, stdio: 'inherit',
  });
  if (up.status !== 0) throw new Error('docker compose up failed');
  await waitForTcp('localhost', ergoPort, 60_000);

  // 2. Spawn `wails dev` (own process group so teardown can kill the whole tree).
  //    `-tags fts5` is REQUIRED: without it the Go app crashes at startup on the
  //    SQLite FTS5 migration. `-frontenddevserverurl` is REQUIRED for per-worktree
  //    isolation: wails.json hardcodes `frontend:dev:serverUrl: http://localhost:5173`,
  //    so without this override Wails proxies the bridge to the fixed 5173 instead of
  //    our dynamic VITE_PORT, breaking parallel runs. Vite reads VITE_PORT (see
  //    frontend/vite.config.ts), so the watcher and this URL resolve to the same port.
  const logFd = fs.openSync(logFile, 'w');
  const child = spawn(
    'wails',
    ['dev', '-tags', 'fts5', '-devserver', `localhost:${bridgePort}`, '-frontenddevserverurl', `http://localhost:${vitePort}`, '-loglevel', 'Warning'],
    {
      cwd: repoRoot,
      env: { ...process.env, CASCADE_DATA_DIR: dataDir, VITE_PORT: String(vitePort), CGO_ENABLED: '1' },
      detached: true,
      stdio: ['ignore', logFd, logFd],
    },
  );
  fs.closeSync(logFd); // child holds its own duplicated fd; release the parent's copy
  child.unref();

  // Playwright does NOT run globalTeardown when globalSetup throws, so any
  // resources started above must be cleaned up here before re-throwing.
  const cleanupPartial = () => {
    try {
      if (child.pid) process.kill(-child.pid, 'SIGTERM');
    } catch {
      /* already gone */
    }
    spawnSync('docker', ['compose', '-p', composeProject, 'down', '-v'], {
      cwd: repoRoot,
      env: composeEnv,
      stdio: 'inherit',
    });
  };

  // A failed spawn (e.g. `wails` not on PATH) emits 'error' asynchronously.
  // Without a listener Node crashes with an unhandled error; race it against
  // readiness so we fail fast with a readable message instead of waiting out
  // the full timeout. The trailing .catch avoids an unhandledRejection once
  // readiness wins the race.
  const spawnFailed = new Promise<never>((_, reject) => {
    child.on('error', (err) => reject(new Error(`failed to spawn wails dev: ${err.message}`)));
  });
  spawnFailed.catch(() => {});

  const bridgeUrl = `http://localhost:${bridgePort}`;
  try {
    // 3. Wait for the bridge (Go compile can take a while on first run).
    const ready = waitForHttp200(`${bridgeUrl}/wails/ipc.js`, 180_000);
    await Promise.race([ready, spawnFailed]);
  } catch (err) {
    cleanupPartial();
    const log = fs.existsSync(logFile) ? fs.readFileSync(logFile, 'utf-8') : '(no log)';
    throw new Error(`${(err as Error).message}\n--- wails log ---\n${log}`);
  }

  const runtime: Runtime = {
    bridgeUrl, vitePort, ergoPort, dataDir, composeProject,
    wailsPid: child.pid as number, logFile,
  };
  writeRuntime(runtime);
  console.log(`[e2e] ready: bridge=${bridgeUrl} ergo=localhost:${ergoPort} data=${dataDir}`);
}
