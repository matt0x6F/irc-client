import { describe, expect, it } from 'vitest';
import { rootViewForSearch } from './root-view';

describe('rootViewForSearch', () => {
  it('selects settings only for the explicit settings window route', () => {
    expect(rootViewForSearch('?view=settings')).toBe('settings');
    expect(rootViewForSearch('?view=chat')).toBe('chat');
    expect(rootViewForSearch('')).toBe('chat');
  });
});
