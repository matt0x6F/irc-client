import { useEffect } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { useTypingStore } from '../stores/typing';
import { useSettingsStore } from '../stores/settings';

// Wire backend typing events into the ephemeral typing store. The receive toggle
// gates here (cheapest place to drop), and a lost connection clears that
// network's typers so a stale "is typing…" can't linger past a disconnect.
// Returns a cleanup that detaches both listeners. Call once at app startup.
export function registerTypingRouting(): () => void {
  const offTyping = EventsOn('typing-event', (p: unknown) => {
    if (!useSettingsStore.getState().typingReceive) return;
    const ev = p as { networkId: number; target: string; nick: string; state: string };
    useTypingStore.getState().applyTypingEvent(ev);
  });

  const offConn = EventsOn('connection-status', (p: unknown) => {
    const { networkId, connected } = (p as { networkId?: number; connected?: boolean }) ?? {};
    if (networkId !== undefined && connected === false) {
      useTypingStore.getState().clearNetwork(networkId);
    }
  });

  return () => {
    offTyping();
    offConn();
  };
}

/** useTypingRouting wires inbound +typing events into the store for the app lifetime. */
export function useTypingRouting(): void {
  useEffect(() => registerTypingRouting(), []);
}
