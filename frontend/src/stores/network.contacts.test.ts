import { beforeEach, expect, it, vi } from 'vitest';
import { useNetworkStore } from './network';

const getContacts=vi.fn();
const sendPM = vi.fn();
vi.mock('../../wailsjs/go/main/App',()=>({GetPrivateContacts:(...args:unknown[])=>getContacts(...args),SendMessageWithContext:(...args:unknown[])=>sendPM(...args)}));

beforeEach(()=>{
  getContacts.mockReset();
  useNetworkStore.setState({connectionStatus:{1:true},connectionStatusAt:{},selectedNetwork:1,selectedChannel:'pm:@17',viewMode:'anchored',atBottom:false,anchoredMessageId:42,unreadCounts:new Map([['1:pm:@17',3]]),channelContextByPane:new Map([['pm:@17','#autumn']])});
});

it('does not restore stale session evidence when a snapshot finishes after disconnect',async()=>{
  let finish!:(contacts:unknown[])=>void;
  getContacts.mockImplementation(()=>new Promise(resolve=>{finish=resolve;}));
  const loading=useNetworkStore.getState().loadPrivateContacts(1);
  useNetworkStore.getState().setConnectionStatus(1,false);
  finish([{id:17,reference:'@17',account:'alice-account',presence:'online',target:'alice',sessions:['alice']}]);
  await loading;
  expect(useNetworkStore.getState().privateContacts[1]['@17'].presence).toBe('unknown');
});

it('withdraws current sessions on disconnect while preserving the saved contact',async()=>{
  getContacts.mockResolvedValue([{id:17,reference:'@17',account:'alice-account',presence:'online',target:'spookyAlice',sessions:['spookyAlice']}]);
  await useNetworkStore.getState().loadPrivateContacts(1);
  useNetworkStore.getState().setConnectionStatus(1,false);
  const contact=useNetworkStore.getState().privateContacts[1]['@17'];
  expect(contact.account).toBe('alice-account');
  expect(contact.presence).toBe('unknown');
  expect(contact.target).toBe('');
  expect(contact.sessions).toEqual([]);
});

it('propagates an unresolved identity error so the composer can retain its draft',async()=>{
  sendPM.mockRejectedValue(new Error("this contact's current nickname is unknown"));
  await expect(useNetworkStore.getState().sendMessage('private reply')).rejects.toThrow('current nickname is unknown');
});

it('refreshes a contact nickname without changing selection, unread, context, or reading state',async()=>{
  getContacts.mockResolvedValue([{id:17,network_id:1,reference:'@17',target_user:'spookyAlice',account:'alice-account',presence:'online',target:'spookyAlice',sessions:['spookyAlice']}]);
  const state=useNetworkStore.getState() as any;
  expect(state.loadPrivateContacts, 'the store needs identity snapshots instead of nickname keys').toBeTypeOf('function');
  await state.loadPrivateContacts(1);
  const updated=useNetworkStore.getState() as any;
  expect(updated.privateContacts[1]['@17'].target_user).toBe('spookyAlice');
  expect(updated.selectedChannel).toBe('pm:@17');
  expect(updated.unreadCounts.get('1:pm:@17')).toBe(3);
  expect(updated.channelContextByPane.get('pm:@17')).toBe('#autumn');
  expect(updated.viewMode).toBe('anchored');
  expect(updated.anchoredMessageId).toBe(42);
  expect(updated.atBottom).toBe(false);
});
