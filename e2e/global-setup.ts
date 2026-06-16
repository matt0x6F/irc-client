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

  // 2. Spawn `wails3 dev` (own process group so teardown can kill the whole tree).
  //    Wails v3 dev model: `-port` sets the Vite dev-server port and `wails3 dev`
  //    exports FRONTEND_DEVSERVER_URL=http://localhost:<port>, which the app loads
  //    its frontend from. The fts5 tag is no longer passed here — it is hardcoded
  //    in the platform build Taskfiles (build/<os>/Taskfile.yml), which `wails3 dev`
  //    invokes via `wails3 build DEV=true`. Vite reads VITE_PORT (frontend/vite.config.ts)
  //    so the watcher and the dev-server URL resolve to the same per-worktree port.
  //
  //    NOTE: v3 replaces v2's standalone `-devserver` HTTP bridge with the Vite
  //    dev server. The Playwright connection below targets the Vite origin. This
  //    harness was migrated to the v3 invocation but must be validated on a host
  //    that can run the full headless GUI stack (Docker Ergo + xvfb + WebKitGTK).
  const logFd = fs.openSync(logFile, 'w');
  const child = spawn(
    'wails3',
    ['dev', '-config', './build/config.yml', '-port', String(vitePort)],
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
    child.on('error', (err) => reject(new Error(`failed to spawn wails3 dev: ${err.message}`)));
  });
  spawnFailed.catch(() => {});

  // If the app crashes during startup (e.g. the WebKit webview fails to init in a
  // headless environment), the process exits before the bridge ever serves. Reject
  // immediately on exit instead of blindly polling until the readiness timeout.
  const processExited = new Promise<never>((_, reject) => {
    child.on('exit', (code, signal) =>
      reject(new Error(`wails3 dev exited before becoming ready (code=${code}, signal=${signal})`)),
    );
  });
  processExited.catch(() => {});

  // In v3 the app's frontend is served by the Vite dev server on vitePort; there
  // is no separate v2 `-devserver` bridge. Playwright connects to the Vite origin.
  const bridgeUrl = `http://localhost:${vitePort}`;
  try {
    // 3. Wait for the Vite dev server to serve the app (Go compile + Vite start
    //    can take a while on first run).
    const ready = waitForHttp200(`${bridgeUrl}/`, 180_000);
    await Promise.race([ready, spawnFailed, processExited]);
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
