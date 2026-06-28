import { describe, it, expect } from 'vitest'
import { buildMsgidIndex, resolveParent, quoteSnippet } from '../reply'
import { storage } from '../../../wailsjs/go/models'

const mk = (o: Partial<storage.Message>) =>
  storage.Message.createFrom({
    id: 1,
    network_id: 1,
    channel_id: null,
    user: 'a',
    message: 'hi',
    message_type: 'privmsg',
    timestamp: '',
    raw_line: '',
    pm_target: '',
    msgid: '',
    reply_msgid: '',
    channel_context: '',
    ...o,
  })

describe('reply helpers', () => {
  it('indexes by non-empty msgid and resolves a parent', () => {
    const idx = buildMsgidIndex([
      mk({ msgid: 'p1', message: 'parent' }),
      mk({ msgid: '' }),
    ])
    expect(idx.size).toBe(1)
    expect(resolveParent('p1', idx)?.message).toBe('parent')
    expect(resolveParent('nope', idx)).toBeNull()
  })

  it('resolveParent returns null for empty replyMsgid', () => {
    const idx = buildMsgidIndex([mk({ msgid: 'p1', message: 'parent' })])
    expect(resolveParent('', idx)).toBeNull()
  })

  it('truncates a long snippet to one line', () => {
    expect(quoteSnippet(mk({ message: 'x'.repeat(200) })).length).toBeLessThanOrEqual(81)
    expect(quoteSnippet(mk({ message: 'a\nb' }))).toBe('a b')
  })

  it('collapses multiple whitespace in quoteSnippet', () => {
    expect(quoteSnippet(mk({ message: 'foo  bar\t\nbaz' }))).toBe('foo bar baz')
  })

  it('respects a custom max in quoteSnippet', () => {
    const snippet = quoteSnippet(mk({ message: 'x'.repeat(200) }), 10)
    expect(snippet.length).toBeLessThanOrEqual(10)
  })
})
