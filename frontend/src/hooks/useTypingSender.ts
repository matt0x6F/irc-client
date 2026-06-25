import { useEffect, useRef } from 'react';
import { SendTyping } from '../../wailsjs/go/main/App';
import { useSettingsStore } from '../stores/settings';
import { createTypingSender, shouldBroadcastTyping, type TypingSender } from '../lib/typing-sender';

// The frontend keys PMs as `pm:<nick>`; the IRC wire target is the bare nick.
function wireTarget(channelName?: string | null): string | null {
  if (!channelName) return null;
  return channelName.startsWith('pm:') ? channelName.slice(3) : channelName;
}

/**
 * Drives the +typing send-state machine from the message input. Reports
 * keystrokes (`onChange`) and the moment a message is sent (`onSubmit`); the
 * underlying sender handles the throttle/idle/done timing. Rebinds when the
 * conversation changes, flushing `done` to the old target first. The send
 * setting is checked at the wire sink so a refresh timer can't leak traffic
 * after the user disables it mid-compose.
 */
export function useTypingSender(networkId?: number | null, channelName?: string | null) {
  const senderRef = useRef<TypingSender | null>(null);
  const target = wireTarget(channelName);

  useEffect(() => {
    if (networkId == null || !target) {
      senderRef.current = null;
      return;
    }
    const sender = createTypingSender((state) => {
      if (!useSettingsStore.getState().typingSend) return;
      void SendTyping(networkId, target, state).catch(() => {});
    });
    senderRef.current = sender;
    // Leaving this conversation (switch or unmount): tell the peer we're done.
    return () => {
      sender.finish();
      senderRef.current = null;
    };
  }, [networkId, target]);

  const onChange = (text: string) => {
    if (!useSettingsStore.getState().typingSend) return; // don't even arm timers
    const sender = senderRef.current;
    if (!sender) return;
    // Composing a message broadcasts typing; emptying the box or starting a
    // slash command flushes `done` (and never broadcasts a command).
    if (shouldBroadcastTyping(text)) sender.type();
    else sender.finish();
  };

  const onSubmit = () => senderRef.current?.finish();

  return { onChange, onSubmit };
}
