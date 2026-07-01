import { describe, it, expect } from 'vitest';
import { isChannelName, DEFAULT_CHANTYPES } from './channel-name';

describe('isChannelName', () => {
  it('treats # and & as channels by default', () => {
    expect(isChannelName('#chan')).toBe(true);
    expect(isChannelName('&local')).toBe(true);
  });

  it('treats a nick or empty string as not a channel', () => {
    expect(isChannelName('alice')).toBe(false);
    expect(isChannelName('')).toBe(false);
  });

  it('does not treat + or ! as channels under the default set', () => {
    expect(isChannelName('+modeless')).toBe(false);
    expect(isChannelName('!safe')).toBe(false);
  });

  it('honors a server-advertised CHANTYPES set', () => {
    expect(isChannelName('+modeless', '#&+!')).toBe(true);
    expect(isChannelName('!safe', '#&+!')).toBe(true);
  });

  it('falls back to the default when CHANTYPES is empty', () => {
    expect(isChannelName('#chan', '')).toBe(true);
    expect(isChannelName('+modeless', '')).toBe(false);
  });

  it('exposes the conventional default set', () => {
    expect(DEFAULT_CHANTYPES).toBe('#&');
  });
});
