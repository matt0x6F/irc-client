import { describe, it, expect } from 'vitest';
import { mergeMessagesById } from './network';
import { storage } from '../../wailsjs/go/models';

// mergeMessagesById folds newly-loaded live messages into the buffer while the user
// is scrolled up reading history. It must NEVER drop the older rows the user is
// looking at — dropping the oldest end is exactly the "scroll up and it skips an
// hour ahead" bug (a busy channel's live traffic grew the buffer past an internal
// cap, which trimmed the oldest messages out from under the reader).

const msg = (id: number, tsSeconds: number): storage.Message =>
  storage.Message.createFrom({ id, timestamp: new Date(tsSeconds * 1000).toISOString() });

describe('mergeMessagesById', () => {
  it('de-dupes by id, incoming copy wins', () => {
    const a = msg(1, 10);
    const bOld = msg(2, 20);
    const bNew = storage.Message.createFrom({ ...bOld, message: 'edited' });
    const merged = mergeMessagesById([a, bOld], [bNew]);
    expect(merged.map((m) => m.id)).toEqual([1, 2]);
    expect(merged.find((m) => m.id === 2)!.message).toBe('edited');
  });

  it('reconciles an optimistic reply with its canonical server echo', () => {
    const optimistic = storage.Message.createFrom({
      id: 1_783_815_469_300,
      network_id: 1,
      channel_id: 42,
      user: 'matt0x6f',
      message: 'one message on the wire',
      message_type: 'privmsg',
      timestamp: '2026-07-12T00:17:49.300Z',
      raw_line: '',
      msgid: '',
      reply_msgid: 'parent-msgid',
    });
    const canonical = storage.Message.createFrom({
      ...optimistic,
      id: 255246,
      timestamp: '2026-07-12T00:17:49.406Z',
      raw_line: '@msgid=server-msgid :matt0x6f!u@h PRIVMSG ##programming :one message on the wire',
      msgid: 'server-msgid',
      reply_msgid: '', // Some servers omit client-only tags from echo-message.
    });

    const merged = mergeMessagesById([optimistic], [canonical]);

    expect(merged).toEqual([canonical]);
  });

  it('keeps the result in timestamp order', () => {
    const merged = mergeMessagesById([msg(3, 30), msg(1, 10)], [msg(2, 20)]);
    expect(merged.map((m) => m.id)).toEqual([1, 2, 3]);
  });

  it('appends newer messages below without dropping the oldest, even for a huge buffer', () => {
    // A long read on a busy channel: the buffer has grown well past any prior cap.
    const existing = Array.from({ length: 1500 }, (_, i) => msg(i + 1, i + 1));
    // A burst of newer live messages arrives while the user reads the top.
    const incoming = Array.from({ length: 50 }, (_, i) => msg(2000 + i, 2000 + i));

    const merged = mergeMessagesById(existing, incoming);

    // REGRESSION: the oldest rows the user is reading must survive the merge.
    expect(merged.length).toBe(existing.length + incoming.length);
    expect(merged[0].id).toBe(1); // oldest retained — not trimmed off the top
    expect(merged[merged.length - 1].id).toBe(2049); // newest appended at the bottom
  });
});
