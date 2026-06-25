// Send-side state machine for the IRCv3 +typing client tag
// (ircv3.net/specs/client-tags/typing). It owns the throttle/idle/done timing so
// the React input only has to report keystrokes and lifecycle moments; the actual
// wire send is injected so this module stays DOM- and Wails-free (and testable).

import { parseCommandLine } from './command-line';

export type TypingState = 'active' | 'paused' | 'done';

// Whether composing this text should broadcast a typing notification. Slash
// commands are excluded — typing `/msg`, `/join`, etc. is not composing a
// message, and broadcasting it would leak intent. Trimmed first to match the
// send path, which trims before classifying command-vs-message.
export function shouldBroadcastTyping(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.length > 0 && !parseCommandLine(trimmed).isCommand;
}

// Re-assert `active` every REFRESH_MS so receivers (who expire a typing state
// after ~6s) don't drop us mid-compose; declare `paused` after IDLE_MS of no
// keystroke. A single repeating timer drives both, deciding paused-vs-refresh by
// elapsed time inside each tick — avoids a two-timer same-tick race.
export const REFRESH_MS = 3000;
export const IDLE_MS = 6000;

export interface TypingSender {
  /** Report a keystroke while the input is non-empty. */
  type(): void;
  /** Input cleared, message sent, or conversation left: emit `done` if composing. */
  finish(): void;
  /** Tear down silently (no `done`) — e.g. the send setting was turned off. */
  dispose(): void;
}

export function createTypingSender(
  send: (state: TypingState) => void,
  now: () => number = () => Date.now(),
): TypingSender {
  let state: 'idle' | TypingState = 'idle';
  let lastType = 0;
  let timer: ReturnType<typeof setInterval> | null = null;

  const stopTimer = () => {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  };

  const tick = () => {
    if (state !== 'active') {
      stopTimer();
      return;
    }
    if (now() - lastType >= IDLE_MS) {
      state = 'paused';
      send('paused');
      stopTimer();
    } else {
      send('active'); // refresh
    }
  };

  return {
    type() {
      lastType = now();
      if (state !== 'active') {
        state = 'active';
        send('active');
        if (timer === null) timer = setInterval(tick, REFRESH_MS);
      }
    },
    finish() {
      if (state !== 'idle') {
        state = 'idle';
        send('done');
      }
      stopTimer();
    },
    dispose() {
      state = 'idle';
      stopTimer();
    },
  };
}
