import {render,screen,fireEvent,waitFor} from '@testing-library/react';
import {it,expect,vi} from 'vitest';
import {PrivateContactHeader} from '../private-contact-header';
import {useNetworkStore} from '../../stores/network';

const prefer=vi.fn().mockResolvedValue(undefined);
vi.mock('../../../wailsjs/go/main/App',()=>({PreferPrivateContactSession:(...args:unknown[])=>prefer(...args)}));

it('shows the current nickname and lets the user choose another verified session',async()=>{
  const load=vi.fn().mockResolvedValue([]);
  useNetworkStore.setState({loadPrivateContacts:load,selectedChannel:'pm:@17'});
  render(<PrivateContactHeader networkId={1} fallback="@17" contact={{id:17,reference:'@17',target:'alice',target_user:'alice',account:'alice-account',sessions:['alice','ghostAlice'],presence:'online'} as any}/>);
  expect(screen.getByText('PM: alice')).toBeVisible();
  fireEvent.change(screen.getByRole('combobox',{name:'Send to session'}),{target:{value:'ghostAlice'}});
  await waitFor(()=>expect(prefer).toHaveBeenCalledWith(1,'@17','ghostAlice'));
  expect(load).toHaveBeenCalledWith(1);
  expect(useNetworkStore.getState().selectedChannel).toBe('pm:@17');
});

it('keeps local reference IDs out of the displayed label before a snapshot loads',()=>{
  render(<PrivateContactHeader networkId={1} fallback="@17"/>);
  expect(screen.getByText('Private message')).toBeVisible();
  expect(screen.queryByText(/@17/)).toBeNull();
});
