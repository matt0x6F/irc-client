import {test,expect} from '../lib/fixtures';
import {addNetworkAndConnect,selectNetwork,joinChannel,openSettings,connect} from '../lib/actions';
import {IrcPeer} from '../lib/irc-peer';
import {repoRoot,writeRuntime} from '../lib/runtime';
import {waitForHttp200} from '../lib/wait';
import {spawn} from 'child_process';
import * as fs from 'fs';
import * as path from 'path';

test('a different account at the saved nickname cannot receive its reply or inherit its history',async({page,runtime})=>{
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page,runtime);
  await selectNetwork(page);
  await joinChannel(page,'#contact-reuse');
  const suffix=Date.now()%100000;
  const alias=`leaf${suffix}`;
  const alice=new IrcPeer('localhost',runtime.ergoPort,`owner${suffix}`);
  const bob=new IrcPeer('localhost',runtime.ergoPort,`other${suffix}`);
  await alice.connect();
  await bob.connect();
  try {
    for(const peer of [alice,bob]) {
      const registered=peer.waitForLine(/ 900 |logged in as|registered/i);
      peer.sendRaw('PRIVMSG NickServ :REGISTER season-test-password account@example.com');
      await registered;
    }
    let renamed=alice.waitForLine(new RegExp(` NICK :?${alias}$`));
    alice.sendRaw(`NICK ${alias}`);
    await renamed;
    const joined=alice.waitForJoin('#contact-reuse');
    alice.join('#contact-reuse');
    await joined;
    alice.say('e2euser',`original-owner-${suffix}`);
    const row=page.locator(`[data-testid="pm-node"][data-peer="${alias}"]`);
    await row.click();
    const id=await row.getAttribute('data-contact-id');
    const saved=page.locator(`[data-testid="pm-node"][data-contact-id="${id}"]`);
    await expect(saved.getByTitle('Online',{exact:true})).toBeVisible();
    alice.close();
    await expect(saved.getByTitle('Presence unknown',{exact:true})).toBeVisible();
    renamed=bob.waitForLine(new RegExp(` NICK :?${alias}$`));
    bob.sendRaw(`NICK ${alias}`);
    await renamed;
    bob.say('e2euser',`replacement-owner-${suffix}`);
    const replacement=page.locator(`[data-testid="pm-node"][data-peer="${alias}"]:not([data-contact-id="${id}"])`);
    await expect(replacement.getByTitle('Online',{exact:true})).toBeVisible();
    await saved.click();
    await expect(page.getByTestId('message-list')).toContainText(`original-owner-${suffix}`);
    await expect(page.getByTestId('message-list')).not.toContainText(`replacement-owner-${suffix}`);
    const input=page.getByTestId('message-input');
    await input.fill(`private-for-original-${suffix}`);
    await input.press('Enter');
    await expect(page.getByRole('alert')).toContainText('current nickname is unknown');
    await expect(input).toHaveValue(`private-for-original-${suffix}`);
    await replacement.click();
    await expect(page.getByTestId('message-list')).toContainText(`replacement-owner-${suffix}`);
    await expect(page.getByTestId('message-list')).not.toContainText(`original-owner-${suffix}`);
  } finally {alice.close();bob.close();}
});

test('one account can choose between two current sessions without splitting the conversation', async ({page,runtime}) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page,runtime);
  await selectNetwork(page);
  await joinChannel(page,'#contact-sessions');
  const suffix=Date.now()%100000;
  const primaryNick=`primary${suffix}`;
  const secondaryNick=`session${suffix}`;
  const primary=new IrcPeer('localhost',runtime.ergoPort,primaryNick);
  const secondary=new IrcPeer('localhost',runtime.ergoPort,secondaryNick);
  await primary.connect();
  await secondary.connect();
  try {
    let identified=primary.waitForLine(/ 900 |logged in as|registered/i);
    primary.sendRaw('PRIVMSG NickServ :REGISTER season-test-password account@example.com');
    await identified;
    const joined=primary.waitForJoin('#contact-sessions');
    primary.join('#contact-sessions');
    await joined;
    primary.say('e2euser',`first-session-${suffix}`);
    const row=page.locator(`[data-testid="pm-node"][data-peer="${primaryNick}"]`);
    await row.click();
    const id=await row.getAttribute('data-contact-id');
    const saved=page.locator(`[data-testid="pm-node"][data-contact-id="${id}"]`);

    identified=secondary.waitForLine(/ 900 |logged in as/i);
    secondary.sendRaw(`PRIVMSG NickServ :IDENTIFY ${primaryNick} season-test-password`);
    await identified;
    const secondaryJoined=secondary.waitForJoin('#contact-sessions');
    secondary.join('#contact-sessions');
    await secondaryJoined;
    const sessions=page.getByRole('combobox',{name:'Send to session'});
    await expect(sessions).toHaveValue(primaryNick);
    await sessions.selectOption(secondaryNick);
    await expect(saved).toHaveAttribute('data-peer',secondaryNick);
    await expect(page.getByTestId('message-list')).toContainText(`first-session-${suffix}`);
    const reply=`chosen-session-${suffix}`;
    const received=secondary.waitForLine(new RegExp(` PRIVMSG ${secondaryNick} :${reply}$`));
    await page.getByTestId('message-input').fill(reply);
    await page.getByTestId('message-input').press('Enter');
    await received;
    secondary.say('e2euser',`second-session-${suffix}`);
    await expect(page.getByTestId('message-list')).toContainText(`second-session-${suffix}`);
    await expect(page.locator(`[data-testid="pm-node"][data-peer="${primaryNick}"]`)).toHaveCount(0);
  } finally {primary.close();secondary.close();}
});

test('saved account recovers after Cascade is stopped during a nickname change',async({page,runtime})=>{
  test.setTimeout(90_000);
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page,runtime);
  await selectNetwork(page);
  await joinChannel(page,'#contact-recovery');
  const suffix=Date.now()%100000;
  const oldNick=`account${suffix}`;
  const newNick=`Ghost${suffix}`;
  const peer=new IrcPeer('localhost',runtime.ergoPort,oldNick);
  await peer.connect();
  let serverStopped=false;
  const restartServer=async()=>{
    const fd=fs.openSync(runtime.logFile,'a');
    const serverBin=process.env.CASCADE_SERVER_BIN || path.join(repoRoot,'bin','cascade-server');
    const child=spawn(serverBin,[],{cwd:repoRoot,env:{...process.env,CASCADE_DATA_DIR:runtime.dataDir,WAILS_SERVER_HOST:'localhost',WAILS_SERVER_PORT:String(runtime.serverPort)},detached:true,stdio:['ignore',fd,fd]});
    fs.closeSync(fd);
    child.unref();
    runtime.serverPid=child.pid!;
    writeRuntime(runtime);
    serverStopped=false;
    await waitForHttp200(`${runtime.bridgeUrl}/health`,30_000);
  };
  try {
    const registered=peer.waitForLine(/ 900 |logged in as|registered/i);
    peer.sendRaw('PRIVMSG NickServ :REGISTER season-test-password account@example.com');
    await registered;
    const joined=peer.waitForJoin('#contact-recovery');
    peer.join('#contact-recovery');
    await joined;
    peer.say('e2euser',`saved-before-close-${suffix}`);
    const row=page.locator(`[data-testid="pm-node"][data-peer="${oldNick}"]`);
    await row.click();
    await expect(row.getByTitle('Online',{exact:true})).toBeVisible();
    await expect(row).toContainText(oldNick);
    const id=await row.getAttribute('data-contact-id');
    await expect(page.getByTestId('message-list')).toContainText(`saved-before-close-${suffix}`);

    // Kill the actual Wails backend, leaving the peer and IRC server alive.
    process.kill(-runtime.serverPid,'SIGTERM');
    serverStopped=true;
    await expect.poll(()=>{
      try {process.kill(runtime.serverPid,0); return false;} catch {return true;}
    }).toBe(true);
    const renamed=peer.waitForLine(new RegExp(` NICK :?${newNick}$`));
    peer.sendRaw(`NICK ${newNick}`);
    await renamed;

    await restartServer();
    await page.reload();
    await selectNetwork(page);
    const saved=page.locator(`[data-testid="pm-node"][data-contact-id="${id}"]`);
    await expect(saved.getByTitle('Presence unknown',{exact:true})).toBeVisible();
    await saved.click();
    const input=page.getByTestId('message-input');
    await input.fill(`draft-for-saved-account-${suffix}`);
    await input.press('Enter');
    await expect(input).toHaveValue(`draft-for-saved-account-${suffix}`);
    await expect(page.getByRole('alert')).toContainText(/not connected|current nickname is unknown/i);

    const settings=await openSettings(page,runtime);
    await connect(page,settings);
    await joinChannel(page,'#contact-recovery');
    await expect(saved).toHaveAttribute('data-peer',newNick);
    await expect(saved.getByTitle('Online',{exact:true})).toBeVisible();
    await expect(page.locator(`[data-testid="pm-node"][data-peer="${oldNick}"]`)).toHaveCount(0);
    await saved.click();
    await expect(page.getByTestId('message-list')).toContainText(`saved-before-close-${suffix}`);
    const reply=`reply-after-restart-${suffix}`;
    const received=peer.waitForLine(new RegExp(` PRIVMSG ${newNick} :${reply}$`));
    await input.fill(reply);
    await input.press('Enter');
    await received;
  } finally {
    peer.close();
    if(serverStopped) await restartServer();
  }
});
