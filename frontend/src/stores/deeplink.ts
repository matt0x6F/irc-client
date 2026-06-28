import { EventsOn } from '../../wailsjs/runtime/runtime';
import {
  OpenSettingsNetworks, ConnectSavedNetwork, GetConnectionStatus, DrainPendingDeepLink,
} from '../../wailsjs/go/main/App';
import { useNetworkStore } from './network';
import { useUIStore } from './ui';

interface Target { name: string; isNick: boolean; key: string }
interface JoinPayload { networkId: number; targets: Target[] }
interface DisambiguatePayload { candidates: { networkId: number; name: string }[]; targets: Target[] }

async function waitForConnection(networkId: number, timeoutMs = 20000): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await GetConnectionStatus(networkId)) return true;
    await new Promise((r) => setTimeout(r, 300));
  }
  return false;
}

// applyDeepLinkTargets connects the network if needed, waits for registration,
// then joins channels / opens PMs. Shared by the join route and the picker.
export async function applyDeepLinkTargets(networkId: number, targets: Target[]): Promise<void> {
  try {
    if (!(await GetConnectionStatus(networkId))) {
      await ConnectSavedNetwork(networkId);
      const ready = await waitForConnection(networkId);
      if (!ready) {
        console.error(`deeplink: network ${networkId} did not connect in time; skipping join`);
        return;
      }
    }
    const net = useNetworkStore.getState();
    for (const t of targets) {
      if (t.isNick) await net.openQuery(networkId, t.name);
      else await net.openOrJoinChannel(networkId, t.name, t.key || undefined);
    }
  } catch (e) {
    console.error('deeplink: failed to apply targets', e);
  }
}

function routeDeepLink(event: string, data: unknown): void {
  switch (event) {
    case 'deeplink:add-network':
      void OpenSettingsNetworks();
      break;
    case 'deeplink:join': {
      const p = data as JoinPayload;
      void applyDeepLinkTargets(p.networkId, p.targets);
      break;
    }
    case 'deeplink:disambiguate':
      useUIStore.getState().setDeepLinkDisambiguation(data as DisambiguatePayload);
      break;
  }
}

// initDeepLinks wires live listeners then drains any cold-start link buffered
// before the webview was ready. Returns an unsubscribe.
export function initDeepLinks(): () => void {
  const offs = [
    EventsOn('deeplink:add-network', (d) => routeDeepLink('deeplink:add-network', d)),
    EventsOn('deeplink:join', (d) => routeDeepLink('deeplink:join', d)),
    EventsOn('deeplink:disambiguate', (d) => routeDeepLink('deeplink:disambiguate', d)),
  ];
  void DrainPendingDeepLink().then((p) => { if (p) routeDeepLink(p.event, p.data); });
  return () => offs.forEach((off) => off && off());
}
