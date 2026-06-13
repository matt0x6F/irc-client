import * as net from 'net';
import * as http from 'http';

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Resolve once a TCP connection to host:port succeeds, else throw after timeout. */
export async function waitForTcp(host: string, port: number, timeoutMs = 30000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((resolve) => {
      const sock = net.connect({ host, port }, () => {
        sock.destroy();
        resolve(true);
      });
      sock.on('error', () => resolve(false));
    });
    if (ok) return;
    await sleep(500);
  }
  throw new Error(`timed out waiting for tcp ${host}:${port}`);
}

/** Resolve once GET url returns HTTP 200, else throw after timeout. */
export async function waitForHttp200(url: string, timeoutMs = 120000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((resolve) => {
      const req = http.get(url, (res) => {
        res.resume();
        resolve(res.statusCode === 200);
      });
      req.setTimeout(5000, () => {
        req.destroy();
        resolve(false);
      });
      req.on('error', () => resolve(false));
    });
    if (ok) return;
    await sleep(1000);
  }
  throw new Error(`timed out waiting for http 200 from ${url}`);
}
