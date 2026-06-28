import { EventsOn } from '../../wailsjs/runtime/runtime';
import { OpenSettingsNetworks } from '../../wailsjs/go/main/App';
import { useNetworkStore } from './network';
import { useUIStore } from './ui';

interface Target { name: string; isNick: boolean; key: string }
interface JoinPayload { networkId: number; targets: Target[] }
interface DisambiguatePayload {
  candidates: { networkId: number; name: string }[];
  targets: Target[];
}

async function applyTargets(networkId: number, targets: Target[]) {
  const net = useNetworkStore.getState();
  for (const t of targets) {
    if (t.isNick) await net.openQuery(networkId, t.name);
    else await net.openOrJoinChannel(networkId, t.name);
  }
}

// initDeepLinks subscribes the MAIN window to backend deep-link events. The
// add-network prefill itself is fetched by the settings window via
// GetPendingNetworkPrefill; here we only open that window. Returns an unsubscribe.
export function initDeepLinks(): () => void {
  const offs = [
    EventsOn('deeplink:add-network', () => {
      void OpenSettingsNetworks();
    }),
    EventsOn('deeplink:join', (raw) => {
      const p = raw as JoinPayload;
      void applyTargets(p.networkId, p.targets);
    }),
    EventsOn('deeplink:disambiguate', (raw) => {
      useUIStore.getState().setDeepLinkDisambiguation(raw as DisambiguatePayload);
    }),
  ];
  return () => offs.forEach((off) => off && off());
}
