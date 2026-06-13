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
  const logFd = fs.openSync(logFile, 'w');
  const child = spawn(
    'wails',
    ['dev', '-tags', 'fts5', '-devserver', `localhost:${bridgePort}`, '-loglevel', 'Warning'],
    {
      cwd: repoRoot,
      env: { ...process.env, CASCADE_DATA_DIR: dataDir, VITE_PORT: String(vitePort), CGO_ENABLED: '1' },
      detached: true,
      stdio: ['ignore', logFd, logFd],
    },
  );
  child.unref();

  const bridgeUrl = `http://localhost:${bridgePort}`;
  try {
    // 3. Wait for the bridge (Go compile can take a while on first run).
    await waitForHttp200(`${bridgeUrl}/wails/ipc.js`, 180_000);
  } catch (err) {
    const log = fs.existsSync(logFile) ? fs.readFileSync(logFile, 'utf-8') : '(no log)';
    throw new Error(`wails dev never became ready.\n--- wails log ---\n${log}`);
  }

  const runtime: Runtime = {
    bridgeUrl, vitePort, ergoPort, dataDir, composeProject,
    wailsPid: child.pid as number, logFile,
  };
  writeRuntime(runtime);
  console.log(`[e2e] ready: bridge=${bridgeUrl} ergo=localhost:${ergoPort} data=${dataDir}`);
}
