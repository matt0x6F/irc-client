import * as net from 'net';

/** A throwaway IRC client: registers, joins a channel, and can PRIVMSG it. */
export class IrcPeer {
  private sock!: net.Socket;
  private buffer = '';
  private joinWaiters: Array<{ channel: string; resolve: () => void }> = [];
  private lineWaiters: Array<{ test: RegExp; resolve: () => void; timer: NodeJS.Timeout }> = [];

  constructor(private host: string, private port: number, private nick: string) {}

  /** Connect and resolve on RPL_WELCOME (001). Rejects on socket error, early close, or timeout. */
  connect(timeoutMs = 30_000): Promise<void> {
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (err?: Error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        if (err) reject(err);
        else resolve();
      };
      const timer = setTimeout(
        () => finish(new Error(`IrcPeer: timed out waiting for RPL_WELCOME after ${timeoutMs}ms`)),
        timeoutMs,
      );

      this.sock = net.connect({ host: this.host, port: this.port }, () => {
        this.send(`NICK ${this.nick}`);
        this.send(`USER ${this.nick} 0 * :${this.nick}`);
      });
      this.sock.setEncoding('utf-8');
      this.sock.on('error', (e) => finish(e));
      this.sock.on('close', () => finish(new Error('IrcPeer: socket closed before RPL_WELCOME')));
      this.sock.on('data', (chunk: string) => {
        this.buffer += chunk;
        let idx;
        while ((idx = this.buffer.indexOf('\r\n')) !== -1) {
          const line = this.buffer.slice(0, idx);
          this.buffer = this.buffer.slice(idx + 2);
          this.handleLine(line, finish);
        }
      });
    });
  }

  private handleLine(line: string, onRegistered: () => void): void {
    if (line.startsWith('PING')) this.send(`PONG ${line.slice(5)}`);
    if (/ 001 /.test(line)) onRegistered(); // RPL_WELCOME

    // Our own JOIN echo, e.g. ":peerbot!~u@host JOIN #e2e" or "... JOIN :#e2e"
    const m = line.match(/^:[^ ]+ JOIN :?(\S+)/i);
    if (m) {
      const chan = m[1].toLowerCase();
      this.joinWaiters = this.joinWaiters.filter((w) => {
        if (w.channel.toLowerCase() === chan) {
          w.resolve();
          return false;
        }
        return true;
      });
    }

    // Generic line waiters (e.g. RPL_LOGGEDIN / NickServ confirmations).
    this.lineWaiters = this.lineWaiters.filter((w) => {
      if (w.test.test(line)) {
        clearTimeout(w.timer);
        w.resolve();
        return false;
      }
      return true;
    });
  }

  /** Send an arbitrary raw IRC line (e.g. a NickServ PRIVMSG). */
  sendRaw(line: string): void {
    this.send(line);
  }

  /** Resolve once a received line matches `pattern`. Rejects on timeout. */
  waitForLine(pattern: RegExp, timeoutMs = 15_000): Promise<void> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`IrcPeer: timed out waiting for line matching ${pattern}`)),
        timeoutMs,
      );
      this.lineWaiters.push({ test: pattern, resolve: () => { clearTimeout(timer); resolve(); }, timer });
    });
  }

  /** Resolve once the server echoes our JOIN for `channel`. Call after join(). */
  waitForJoin(channel: string, timeoutMs = 15_000): Promise<void> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`IrcPeer: timed out waiting for JOIN echo of ${channel}`)),
        timeoutMs,
      );
      this.joinWaiters.push({ channel, resolve: () => { clearTimeout(timer); resolve(); } });
    });
  }

  private send(line: string): void {
    this.sock.write(line + '\r\n');
  }

  join(channel: string): void {
    this.send(`JOIN ${channel}`);
  }

  say(channel: string, message: string): void {
    this.send(`PRIVMSG ${channel} :${message}`);
  }

  close(): void {
    try {
      this.send('QUIT :bye');
      this.sock.end();
    } catch {
      /* ignore */
    }
  }
}
