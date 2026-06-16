import { describe, it, expect } from 'vitest'
import { wrapSelection, insertAtCaret } from '../input-insert'

describe('wrapSelection', () => {
  it('wraps a selection and keeps the inner text selected', () => {
    // "hello world", select "world" (6..11)
    const r = wrapSelection('hello world', 6, 11, '*', '*')
    expect(r.value).toBe('hello *world*')
    expect(r.selectionStart).toBe(7)
    expect(r.selectionEnd).toBe(12)
  })

  it('inserts an empty pair and places the caret between the delimiters', () => {
    const r = wrapSelection('ab', 1, 1, '*', '*')
    expect(r.value).toBe('a**b')
    expect(r.selectionStart).toBe(2)
    expect(r.selectionEnd).toBe(2)
  })

  it('supports multi-character delimiters (underline)', () => {
    const r = wrapSelection('note', 0, 4, '__', '__')
    expect(r.value).toBe('__note__')
    expect(r.selectionStart).toBe(2)
    expect(r.selectionEnd).toBe(6)
  })

  it('supports asymmetric delimiters (color)', () => {
    const r = wrapSelection('red', 0, 3, '#4(', ')')
    expect(r.value).toBe('#4(red)')
    expect(r.selectionStart).toBe(3)
    expect(r.selectionEnd).toBe(6)
  })
})

describe('insertAtCaret', () => {
  it('inserts text at a collapsed caret', () => {
    const r = insertAtCaret('ab', 1, 1, 'X')
    expect(r.value).toBe('aXb')
    expect(r.selectionStart).toBe(2)
    expect(r.selectionEnd).toBe(2)
  })

  it('replaces the current selection', () => {
    const r = insertAtCaret('hello', 0, 5, 'hi')
    expect(r.value).toBe('hi')
    expect(r.selectionStart).toBe(2)
    expect(r.selectionEnd).toBe(2)
  })

  it('accounts for multi-code-unit emoji length when placing the caret', () => {
    const r = insertAtCaret('ab', 1, 1, '😊')
    expect(r.value).toBe('a😊b')
    expect(r.selectionStart).toBe(1 + '😊'.length)
  })
})
