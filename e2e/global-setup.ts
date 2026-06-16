import { spawn, spawnSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { getFreePorts } from './lib/ports';
import { writeRuntime, repoRoot, Runtime } from './lib/runtime';
import { waitForTcp, waitForHttp200 } from './lib/wait';

const serverBinName = process.platform === 'win32' ? 'cascade-server.exe' : 'cascade-server';

/**
 * Run a command synchronously, inheriting stdio, and throw a readable error on
 * failure. Used for the one-off build steps below.
 */
function run(cmd: string, args: string[], cwd: string, extraEnv: NodeJS.ProcessEnv = {}): void {
  const res = spawnSync(cmd, args, { cwd, env: { ...process.env, ...extraEnv }, stdio: 'inherit' });
  if (res.error) throw new Error(`failed to run ${cmd}: ${res.error.message}`);
  if (res.status !== 0) throw new Error(`${cmd} ${args.join(' ')} exited with code ${res.status}`);
}

/**
 * Build the headless Wails v3 server-mode binary that the suite drives.
 *
 * Server mode (`-tags server`) swaps the native window for a plain HTTP server
 * that serves the embedded frontend + the `/wails/runtime` binding bridge. The
 * binary opens no window at runtime, so the suite needs no xvfb / X display —
 * this is what removes the GTK/X-server flakiness. `fts5` is required for the
 * SQLite full-text message search.
 *
 * Note: on Linux the *build* still links GTK/WebKit (wails' internal
 * operatingsystem/assetserver cgo isn't `server`-guarded in this alpha), so a
 * Linux box needs the gtk4 / webkitgtk-6.0 dev headers installed to build —
 * even though the resulting binary never opens a display. macOS links Cocoa and
 * needs nothing extra.
 *
 * The Go binary embeds `frontend/dist`, so the frontend must be built first.
 */
function buildServerBinary(binPath: string): void {
  console.log('[e2e] building frontend (vite) → frontend/dist …');
  run('npm', ['run', 'build'], path.join(repoRoot, 'frontend'));
  console.log(`[e2e] building ${serverBinName} (-tags "server fts5") …`);
  run('go', ['build', '-tags', 'server fts5', '-o', binPath, '.'], repoRoot, { CGO_ENABLED: '1' });
}

/**
 * Resolve the server binary, building it unless told otherwise.
 *   - CASCADE_SERVER_BIN=/path  → use a prebuilt binary, never build (CI provides this).
 *   - CASCADE_E2E_NO_BUILD=1     → use bin/cascade-server as-is, skip the build.
 *   - otherwise                  → (re)build bin/cascade-server for a fresh, in-sync binary.
 */
function resolveServerBinary(): string {
  if (process.env.CASCADE_SERVER_BIN) {
    const bin = path.resolve(process.env.CASCADE_SERVER_BIN);
    if (!fs.existsSync(bin)) throw new Error(`CASCADE_SERVER_BIN=${bin} does not exist`);
    return bin;
  }
  const bin = path.join(repoRoot, 'bin', serverBinName);
  if (process.env.CASCADE_E2E_NO_BUILD === '1') {
    if (!fs.existsSync(bin)) {
      throw new Error(`CASCADE_E2E_NO_BUILD=1 but ${bin} is missing — build it first (task build-server)`);
    }
    return bin;
  }
  buildServerBinary(bin);
  return bin;
}

export default async function globalSetup(): Promise<void> {
  const [serverPort, ergoPort, ergoTlsPort] = await getFreePorts(3);
  const id = String(serverPort);
  const composeProject = `cascade-e2e-${id}`;
  const dataDir = path.join(os.tmpdir(), 'cascade-e2e', id);
  const logFile = path.join(os.tmpdir(), 'cascade-e2e', `${id}.server.log`);
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(path.dirname(logFile), { recursive: true });

  // 0. Build (or locate) the headless server binary before anything external is
  //    started, so a compile error fails fast without leaving Docker/processes behind.
  const serverBin = resolveServerBinary();

  // 1. Bring up Ergo on the allocated port.
  const composeEnv = { ...process.env, ERGO_PORT: String(ergoPort), ERGO_TLS_PORT: String(ergoTlsPort) };
  const up = spawnSync('docker', ['compose', '-p', composeProject, 'up', '-d'], {
    cwd: repoRoot, env: composeEnv, stdio: 'inherit',
  });
  if (up.status !== 0) throw new Error('docker compose up failed');
  await waitForTcp('localhost', ergoPort, 60_000);

  // 2. Spawn the server-mode binary (own process group so teardown can kill the
  //    whole tree — the app forks plugin subprocesses). WAILS_SERVER_PORT/HOST
  //    are read by the Wails v3 server runtime; CASCADE_DATA_DIR isolates this
  //    run's SQLite DB. CGO is already baked into the binary; nothing to set here.
  const logFd = fs.openSync(logFile, 'w');
  const child = spawn(serverBin, [], {
    cwd: repoRoot,
    env: {
      ...process.env,
      WAILS_SERVER_HOST: 'localhost',
      WAILS_SERVER_PORT: String(serverPort),
      CASCADE_DATA_DIR: dataDir,
    },
    detached: true,
    stdio: ['ignore', logFd, logFd],
  });
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

  // A failed spawn (e.g. binary missing/not executable) emits 'error' asynchronously.
  // Without a listener Node crashes with an unhandled error; race it against
  // readiness so we fail fast with a readable message instead of waiting out
  // the full timeout. The trailing .catch avoids an unhandledRejection once
  // readiness wins the race.
  const spawnFailed = new Promise<never>((_, reject) => {
    child.on('error', (err) => reject(new Error(`failed to spawn ${serverBinName}: ${err.message}`)));
  });
  spawnFailed.catch(() => {});

  // If the server exits during startup (e.g. port already taken, DB init failure),
  // reject immediately on exit instead of blindly polling until the readiness timeout.
  const processExited = new Promise<never>((_, reject) => {
    child.on('exit', (code, signal) =>
      reject(new Error(`${serverBinName} exited before becoming ready (code=${code}, signal=${signal})`)),
    );
  });
  processExited.catch(() => {});

  // In v3 server mode the Go binary serves the frontend + bindings directly on
  // serverPort, exposing a /health endpoint. Playwright connects to this origin.
  const bridgeUrl = `http://localhost:${serverPort}`;
  try {
    // 3. Wait for the server's /health endpoint to report ready.
    const ready = waitForHttp200(`${bridgeUrl}/health`, 60_000);
    await Promise.race([ready, spawnFailed, processExited]);
  } catch (err) {
    cleanupPartial();
    const log = fs.existsSync(logFile) ? fs.readFileSync(logFile, 'utf-8') : '(no log)';
    throw new Error(`${(err as Error).message}\n--- server log ---\n${log}`);
  }

  const runtime: Runtime = {
    bridgeUrl, serverPort, ergoPort, dataDir, composeProject,
    serverPid: child.pid as number, logFile,
  };
  writeRuntime(runtime);
  console.log(`[e2e] ready: app=${bridgeUrl} ergo=localhost:${ergoPort} data=${dataDir}`);
}
