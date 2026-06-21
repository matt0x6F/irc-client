import { useEffect } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { FocusMainWindow } from '../../wailsjs/go/main/App';
import { useNetworkStore } from '../stores/network';

interface NavPayload {
  networkId: number;
  target: string;
}

/**
 * registerNotificationRouting subscribes to backend notification-response events
 * and routes them into the network store. Returns an unsubscribe function.
 * Extracted from the hook so it can be unit-tested without a React renderer.
 */
export function registerNotificationRouting(): () => void {
  const offNav = EventsOn('notification:navigate', (p: NavPayload) => {
    if (p?.target) {
      void useNetworkStore.getState().selectPane(p.networkId, p.target);
    }
    void FocusMainWindow();
  });
  const offRead = EventsOn('notification:markRead', (p: NavPayload) => {
    if (p?.target) {
      useNetworkStore.getState().clearActivity(`${p.networkId}:${p.target}`);
    }
  });
  return () => {
    offNav();
    offRead();
  };
}

/** useNotificationRouting wires notification-response routing for the app lifetime. */
export function useNotificationRouting(): void {
  useEffect(() => registerNotificationRouting(), []);
}
