import {test,expect} from '../lib/fixtures';
import {
  openSettings,addNetwork,selectNetwork,joinChannel,networkTile,
  connectViaContextMenu,disconnectViaContextMenu,deleteNetwork,
} from '../lib/actions';
import {IrcPeer} from '../lib/irc-peer';
import {getFreePort} from '../lib/ports';
import {waitForTcp} from '../lib/wait';
import {spawnSync} from 'child_process';

const NAME='inspircd';

// The suite shares one backend DB. Remove this network even after a failed or
// timed-out test so later specs do not inherit an extra disconnected network.
test.afterEach(async({page})=>{
  await deleteNetwork(page,NAME);
  await expect(networkTile(page,NAME)).toHaveCount(0);
});

// A second real server with no services: live NICK continuity works, while a
// missed rename has no durable account evidence and must remain unresolved.
test('InspIRCd follows live nick changes and avoids guessing a missed unidentified rename',async({page,runtime})=>{
  test.setTimeout(90_000);
  const port=await getFreePort();
  const container=`cascade-contact-inspircd-${port}`;
  const up=spawnSync('docker',['run','--rm','-d','--name',container,'-p',`127.0.0.1:${port}:6667`,'-e','INSP_ENABLE_DNSBL=no','-e','INSP_NET_NAME=CascadeIdentityFixture','inspircd/inspircd-docker:4'],{encoding:'utf8'});
  expect(up.status,up.stderr).toBe(0);
  let peer:IrcPeer|undefined;
  try {
    await waitForTcp('localhost',port,30_000);
    await page.goto(runtime.bridgeUrl);
    const settings=await openSettings(page,runtime);
    await addNetwork(settings,{...runtime,ergoPort:port},{name:NAME,nick:'inspuser'});
    await settings.close();
    await connectViaContextMenu(page,NAME);
    await selectNetwork(page,NAME);
    await joinChannel(page,'#identity');
    peer=new IrcPeer('localhost',port,'autumnPeer');
    await peer.connect();
    const joined=peer.waitForJoin('#identity');
    peer.join('#identity');
    await joined;
    peer.say('inspuser','inspircd-original-history');
    const original=page.locator('[data-testid="pm-node"][data-peer="autumnPeer"]');
    await original.click();
    const id=await original.getAttribute('data-contact-id');
    const saved=page.locator(`[data-testid="pm-node"][data-contact-id="${id}"]`);
    let renamed=peer.waitForLine(/ NICK :?spookyPeer$/);
    peer.sendRaw('NICK spookyPeer');
    await renamed;
    await expect(saved).toHaveAttribute('data-peer','spookyPeer');
    await expect(saved.getByTitle('Online',{exact:true})).toBeVisible();
    await expect(saved).toHaveClass(/cc-active-pane/);

    // Disconnect Cascade, so its new client cannot witness the next rename.
    await disconnectViaContextMenu(page,NAME);
    await expect(saved.getByTitle('Presence unknown',{exact:true})).toBeVisible();
    renamed=peer.waitForLine(/ NICK :?winterPeer$/);
    peer.sendRaw('NICK winterPeer');
    await renamed;
    await connectViaContextMenu(page,NAME);
    await joinChannel(page,'#identity');
    peer.say('inspuser','inspircd-unidentified-after-reconnect');
    const separate=page.locator('[data-testid="pm-node"][data-peer="winterPeer"]');
    await expect(separate).toBeVisible();
    await expect(separate).not.toHaveAttribute('data-contact-id',id!);
    await saved.click();
    await expect(saved.getByTitle('Presence unknown',{exact:true})).toBeVisible();
    await expect(page.getByTestId('message-list')).toContainText('inspircd-original-history');
    await expect(page.getByTestId('message-list')).not.toContainText('inspircd-unidentified-after-reconnect');
  } finally {
    peer?.close();
    spawnSync('docker',['rm','-f',container],{stdio:'ignore'});
  }
});
