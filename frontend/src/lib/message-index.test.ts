import { describe, it, expect } from 'vitest'
import { indexByMsgid, indexById } from './message-index'

const rows = [
  { id: 10, msgid: 'a' },
  { id: 11, msgid: '' },
  { id: 12, msgid: 'c' },
]

describe('indexByMsgid', () => {
  it('maps each non-empty msgid to its row index', () => {
    const m = indexByMsgid(rows)
    expect(m.get('a')).toBe(0)
    expect(m.get('c')).toBe(2)
  })

  it('skips empty msgids', () => {
    expect(indexByMsgid(rows).has('')).toBe(false)
  })

  it('returns undefined for an unknown msgid', () => {
    expect(indexByMsgid(rows).get('nope')).toBeUndefined()
  })
})

describe('indexById', () => {
  it('maps each row id to its index', () => {
    const m = indexById(rows)
    expect(m.get(10)).toBe(0)
    expect(m.get(12)).toBe(2)
    expect(m.get(999)).toBeUndefined()
  })
})
