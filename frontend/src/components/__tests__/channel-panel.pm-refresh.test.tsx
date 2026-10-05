import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, act, fireEvent } from '@testing-library/react';
// `storage` must import before `ChannelPanel` (see channel-panel.test.tsx for why).
import { storage } from '../../../wailsjs/go/models';
import { ChannelPanel } from '../channel-panel';

// Capture EventsOn callbacks by event name so a test can fire a synthetic event.
const handlers: Record<string, ((data: unknown) => void)[]> = {};
vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (data: unknown) => void) => {
    (handlers[name] ||= []).push(cb);
    return () => {};
  },
}));

const getContacts = vi.fn();
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetOpenChannels: vi.fn().mockResolvedValue([]),
  GetPrivateContacts: (...args: unknown[]) => getContacts(...args),
  GetChannels: vi.fn().mockResolvedValue([]),
  GetJoinedChannels: vi.fn().mockResolvedValue([]),
  GetMonitorPresence: vi.fn().mockResolvedValue({}),
  CloseChannel: vi.fn(),
  LeaveChannel: vi.fn(),
  SendCommand: vi.fn(),
  SetPrivateMessageOpen: vi.fn(),
  ClearPaneFocus: vi.fn(),
  ToggleChannelAutoJoin: vi.fn(),
}));

const makeContact = (nick: string) => storage.PrivateMessageConversation.createFrom({id:17,network_id:1,reference:'@17',target_user:nick,target:nick,presence:'unknown',sessions:[]});

const network = storage.Network.createFrom({ id: 1, name: 'e2e' });

function fire(data: unknown) {
  return act(async () => {
    handlers['message-event']?.forEach((cb) => cb(data));
  });
}

function renderPanel(unreadCounts = new Map<string, number>()) {
  return render(
    <ChannelPanel
      network={network}
      selectedChannel="status"
      connected
      currentNick="e2euser"
      unreadCounts={unreadCounts}
      onSelectChannel={() => {}}
      onShowUserInfo={() => {}}
    />,
  );
}

beforeEach(() => {
  for (const k of Object.keys(handlers)) delete handlers[k];
  getContacts.mockReset();
  getContacts.mockResolvedValue([]);
});

describe('ChannelPanel PM refresh on message-event', () => {
  it('disables recipient actions when the saved account has no verified current nickname',async()=>{
    getContacts.mockResolvedValue([storage.PrivateMessageConversation.createFrom({id:17,network_id:1,reference:'@17',target_user:'alice',account:'alice-account',presence:'unknown',target:'',sessions:[]})]);
    const panel=renderPanel();
    const row=await panel.findByText('alice');
    await act(async()=>{fireEvent.contextMenu(row);});
    expect(panel.getByRole('button',{name:'Whois'})).toBeDisabled();
    expect(panel.getByRole('button',{name:'Close'})).toBeEnabled();
  });
  it('selects a stable contact pane and shows verified presence without relying on its old nickname monitor', async () => {
    getContacts.mockResolvedValue([{id:17,network_id:1,reference:'@17',target_user:'alice',target:'spookyAlice',account:'alice-account',presence:'online',sessions:['spookyAlice']}]);
    const onSelect=vi.fn();
    const panel=render(<ChannelPanel network={network} selectedChannel="pm:@17" connected currentNick="e2euser" unreadCounts={new Map([['1:pm:@17',3]])} onSelectChannel={onSelect} onShowUserInfo={()=>{}} />);
    const peer=await panel.findByText('spookyAlice');
    expect(panel.getByTitle('Online',{exact:true})).toBeVisible();
    expect(panel.getByTitle('Unread messages')).toHaveTextContent('3');
    fireEvent.click(peer);
    expect(onSelect).toHaveBeenCalledWith(1,'pm:@17');
  });
  it('refreshes PMs on a PM message-event — a channel-context PM sets `channel` to our own nick, so it is keyed by pmTarget', async () => {
    renderPanel();
    await waitFor(() => expect(getContacts).toHaveBeenCalledTimes(1)); // mount load
    // channel-context PM: channel is the recipient (our nick), pmTarget is the peer.
    await fire({
      type: 'message.received',
      data: { networkId: 1, channel: 'e2euser', channelContext: '#test', pmTarget: 'ctxbot', messageType: 'privmsg', user: 'ctxbot' },
    });
    await waitFor(() => expect(getContacts).toHaveBeenCalledTimes(2));
    expect(getContacts).toHaveBeenLastCalledWith(1);
  });

  it('ignores a channel message (no pmTarget) — no extra PM refresh', async () => {
    renderPanel();
    await waitFor(() => expect(getContacts).toHaveBeenCalledTimes(1));
    await fire({
      type: 'message.received',
      data: { networkId: 1, channel: '#chan', pmTarget: '', messageType: 'privmsg', user: 'bob' },
    });
    // Give any errant async refresh a chance to run, then assert it did not.
    await new Promise((r) => setTimeout(r, 20));
    expect(getContacts).toHaveBeenCalledTimes(1);
  });

  it('ignores a PM for a different network', async () => {
    renderPanel();
    await waitFor(() => expect(getContacts).toHaveBeenCalledTimes(1));
    await fire({
      type: 'message.received',
      data: { networkId: 2, channel: 'e2euser', pmTarget: 'ctxbot', messageType: 'privmsg', user: 'ctxbot' },
    });
    await new Promise((r) => setTimeout(r, 20));
    expect(getContacts).toHaveBeenCalledTimes(1);
  });

  it('replaces the sidebar peer after an observed nick change without waiting for another message', async () => {
    getContacts.mockResolvedValue([makeContact('alice')]);
    const panel = renderPanel();
    await waitFor(() => expect(panel.getByText('alice')).toBeVisible());
    getContacts.mockResolvedValue([makeContact('spookyalice')]);
    await fire({ type: 'user.nick', data: { networkId: 1, oldNick: 'alice', newNick: 'spookyAlice', pmRenamed: true } });
    await waitFor(() => expect(panel.getByText('spookyalice')).toBeVisible());
    expect(panel.queryByText('alice')).not.toBeInTheDocument();
  });

  it('shows migrated unread badges even before a mixed-case renamed peer sends another message', async () => {
    getContacts.mockResolvedValue([makeContact('spookyalice')]);
    const panel = renderPanel(new Map([['1:pm:@17', 2], ['2:pm:@17', 8]]));
    await waitFor(() => expect(panel.getByText('spookyalice')).toBeVisible());
    expect(panel.getByTitle('Unread messages')).toHaveTextContent('2');
  });
});
