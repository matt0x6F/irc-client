import { describe, it, expect, vi, beforeEach } from 'vitest';

const listScripts = vi.fn();
const enableScript = vi.fn((_id: string) => Promise.resolve());
const disableScript = vi.fn((_id: string) => Promise.resolve());
const reloadScript = vi.fn((_id: string) => Promise.resolve());
const newScript = vi.fn((_name: string) => Promise.resolve('/scripts/foo/foo.go'));
const openScriptsDir = vi.fn(() => Promise.resolve());
let lifecycleCb: (() => void) | null = null;

vi.mock('../../wailsjs/go/main/App', () => ({
  ListScripts: () => listScripts(),
  EnableScript: (id: string) => enableScript(id),
  DisableScript: (id: string) => disableScript(id),
  ReloadScript: (id: string) => reloadScript(id),
  NewScript: (name: string) => newScript(name),
  OpenScriptsDir: () => openScriptsDir(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((_name: string, cb: () => void) => {
    lifecycleCb = cb;
    return () => {};
  }),
}));

import { useScriptsStore, initScripts } from './scripts';

const row = (over: Record<string, unknown> = {}) => ({
  id: 'greeter', name: 'greeter', description: '', status: 'loaded',
  enabled: true, error: '', perms: [], ...over,
});

describe('scripts store', () => {
  beforeEach(() => {
    listScripts.mockReset().mockResolvedValue([row()]);
    enableScript.mockClear(); disableScript.mockClear(); reloadScript.mockClear();
    newScript.mockClear(); openScriptsDir.mockClear();
    lifecycleCb = null;
    useScriptsStore.setState({ scripts: [], loading: false, busy: new Set(), lastCreatedPath: null, error: null });
  });

  it('fetch() populates scripts from ListScripts', async () => {
    await useScriptsStore.getState().fetch();
    expect(listScripts).toHaveBeenCalled();
    expect(useScriptsStore.getState().scripts).toHaveLength(1);
    expect(useScriptsStore.getState().scripts[0].id).toBe('greeter');
  });

  it('disable() calls DisableScript then refetches', async () => {
    await useScriptsStore.getState().disable('greeter');
    expect(disableScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('enable() calls EnableScript then refetches', async () => {
    await useScriptsStore.getState().enable('greeter');
    expect(enableScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('reload() calls ReloadScript then refetches', async () => {
    await useScriptsStore.getState().reload('greeter');
    expect(reloadScript).toHaveBeenCalledWith('greeter');
    expect(listScripts).toHaveBeenCalled();
  });

  it('create() stores the returned path and refetches', async () => {
    await useScriptsStore.getState().create('foo');
    expect(newScript).toHaveBeenCalledWith('foo');
    expect(useScriptsStore.getState().lastCreatedPath).toBe('/scripts/foo/foo.go');
  });

  it('create() surfaces backend errors and does not set a path', async () => {
    newScript.mockRejectedValueOnce(new Error('script "foo" already exists'));
    await useScriptsStore.getState().create('foo');
    expect(useScriptsStore.getState().error).toContain('already exists');
    expect(useScriptsStore.getState().lastCreatedPath).toBeNull();
  });

  it('initScripts() subscribes to script-lifecycle and refetches on it', async () => {
    initScripts();
    expect(lifecycleCb).toBeTypeOf('function');
    listScripts.mockClear();
    lifecycleCb!();
    await Promise.resolve();
    expect(listScripts).toHaveBeenCalled();
  });
});
