import * as net from 'net';

/** Allocate a free TCP port by binding to :0 and reading the assigned port. */
export function getFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (addr && typeof addr === 'object') {
        const { port } = addr;
        srv.close(() => resolve(port));
      } else {
        srv.close(() => reject(new Error('could not determine free port')));
      }
    });
  });
}

/** Allocate N distinct free ports. */
export async function getFreePorts(n: number): Promise<number[]> {
  const seen = new Set<number>();
  while (seen.size < n) seen.add(await getFreePort());
  return [...seen];
}
