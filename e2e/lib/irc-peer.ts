import * as net from 'net';

/** A throwaway IRC client: registers, joins a channel, and can PRIVMSG it. */
export class IrcPeer {
  private sock!: net.Socket;
  private buffer = '';

  constructor(private host: string, private port: number, private nick: string) {}

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.sock = net.connect({ host: this.host, port: this.port }, () => {
        this.send(`NICK ${this.nick}`);
        this.send(`USER ${this.nick} 0 * :${this.nick}`);
      });
      this.sock.setEncoding('utf-8');
      this.sock.on('error', reject);
      this.sock.on('data', (chunk: string) => {
        this.buffer += chunk;
        let idx;
        while ((idx = this.buffer.indexOf('\r\n')) !== -1) {
          const line = this.buffer.slice(0, idx);
          this.buffer = this.buffer.slice(idx + 2);
          if (line.startsWith('PING')) this.send(`PONG ${line.slice(5)}`);
          // RPL_WELCOME (001) => registration complete.
          if (/ 001 /.test(line)) resolve();
        }
      });
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
    try { this.send('QUIT :bye'); this.sock.end(); } catch { /* ignore */ }
  }
}
