import * as fs from 'fs';
import * as path from 'path';

export interface Runtime {
  bridgeUrl: string;   // e.g. http://localhost:34567
  vitePort: number;
  ergoPort: number;
  dataDir: string;
  composeProject: string;
  wailsPid: number;
  logFile: string;
}

const RUNTIME_PATH = path.join(__dirname, '..', '.runtime.json');

export function writeRuntime(r: Runtime): void {
  fs.writeFileSync(RUNTIME_PATH, JSON.stringify(r, null, 2));
}

export function readRuntime(): Runtime {
  if (!fs.existsSync(RUNTIME_PATH)) {
    throw new Error(
      'e2e/.runtime.json not found — run `npx playwright test` (it starts globalSetup); `--list` alone does not create it',
    );
  }
  return JSON.parse(fs.readFileSync(RUNTIME_PATH, 'utf-8')) as Runtime;
}

export function clearRuntime(): void {
  if (fs.existsSync(RUNTIME_PATH)) fs.rmSync(RUNTIME_PATH);
}

export const repoRoot = path.resolve(__dirname, '..', '..');
