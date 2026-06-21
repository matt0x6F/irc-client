import { describe, it, expect } from 'vitest';
import { eventPaneKey, eventMatchesPane } from './pane-routing';

describe('eventPaneKey', () => {
  it('routes a PM event to pm:<peer> using the backend-computed pmTarget', () => {
    // Inbound DM from alice: channel is *our* nick, but pmTarget is the peer.
    expect(eventPaneKey({ channel: 'matt0x6f', pmTarget: 'alice' })).toBe('pm:alice');
  });

  it('routes an echoed (sent) PM to the recipient via pmTarget', () => {
    // echo-message: we sent to bob, server echoes it back; pmTarget is bob.
    expect(eventPaneKey({ channel: 'bob', pmTarget: 'bob' })).toBe('pm:bob');
  });

  it('routes a channel event to the raw channel name', () => {
    expect(eventPaneKey({ channel: '#ircv3', pmTarget: '' })).toBe('#ircv3');
    expect(eventPaneKey({ channel: '&local', pmTarget: '' })).toBe('&local');
  });

  it('routes a null/empty channel (server message) to status', () => {
    expect(eventPaneKey({ channel: null, pmTarget: '' })).toBe('status');
    expect(eventPaneKey({ channel: undefined, pmTarget: undefined })).toBe('status');
  });

  it('falls back to pm:<nick> for a legacy PM event missing pmTarget', () => {
    // Older events (pre-pmTarget) carried only the non-channel target.
    expect(eventPaneKey({ channel: 'alice' })).toBe('pm:alice');
  });

  it('uses the `target` field for sent events (which carry target, not channel)', () => {
    // message.sent events are shaped { target, message } — no `channel`.
    expect(eventPaneKey({ target: '#ircv3' })).toBe('#ircv3');
    expect(eventPaneKey({ target: 'alice' })).toBe('pm:alice');
  });

  it('prefers pmTarget over target when both are present', () => {
    expect(eventPaneKey({ target: 'alice', pmTarget: 'alice' })).toBe('pm:alice');
  });
});

describe('eventMatchesPane', () => {
  // This is the regression guard for the slow-PM bug: an inbound PM to the
  // focused conversation must be recognized as matching so the live event
  // triggers a reload instead of waiting for the 2s poll.
  it('matches an inbound PM against the focused pm: pane', () => {
    expect(eventMatchesPane({ channel: 'matt0x6f', pmTarget: 'alice' }, 'pm:alice')).toBe(true);
  });

  it('matches PM panes case-insensitively (IRC nicks)', () => {
    expect(eventMatchesPane({ channel: 'matt0x6f', pmTarget: 'Alice' }, 'pm:alice')).toBe(true);
  });

  it('does not match a PM event against a different PM pane', () => {
    expect(eventMatchesPane({ channel: 'matt0x6f', pmTarget: 'alice' }, 'pm:bob')).toBe(false);
  });

  it('does not match a PM event against a channel pane', () => {
    expect(eventMatchesPane({ channel: 'matt0x6f', pmTarget: 'alice' }, '#ircv3')).toBe(false);
  });

  it('matches a channel event against its channel pane', () => {
    expect(eventMatchesPane({ channel: '#ircv3', pmTarget: '' }, '#ircv3')).toBe(true);
  });

  it('matches a SENT channel message (target field) against its channel pane', () => {
    // Regression guard: sent events carry `target`, not `channel`; the channel
    // view must still refresh when we send to it.
    expect(eventMatchesPane({ target: '#ircv3', message: 'hi' }, '#ircv3')).toBe(true);
  });

  it('matches a SENT private message against the focused pm pane', () => {
    expect(eventMatchesPane({ target: 'alice', message: 'hi' }, 'pm:alice')).toBe(true);
  });

  it('matches a server message against the status pane', () => {
    expect(eventMatchesPane({ channel: null, pmTarget: '' }, 'status')).toBe(true);
  });

  it('does not match a channel event against the wrong channel', () => {
    expect(eventMatchesPane({ channel: '#ircv3', pmTarget: '' }, '#other')).toBe(false);
  });
});
