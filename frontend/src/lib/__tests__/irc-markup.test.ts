import { describe, it, expect } from 'vitest'
import { markdownToIrc, hasMarkup, stripMarkup } from '../irc-markup'

// IRC control codes the converter emits.
const B = '\x02' // bold
const I = '\x1D' // italic
const U = '\x1F' // underline
const C = '\x03' // color

describe('markdownToIrc — plain text', () => {
  it('leaves text without markup untouched', () => {
    expect(markdownToIrc('hello world')).toBe('hello world')
  })

  it('returns empty string unchanged', () => {
    expect(markdownToIrc('')).toBe('')
  })
})

describe('markdownToIrc — bold / italic / underline', () => {
  it('wraps *bold* in the bold control code', () => {
    expect(markdownToIrc('*team*')).toBe(`${B}team${B}`)
  })

  it('wraps _italic_ in the italic control code', () => {
    expect(markdownToIrc('_ok_')).toBe(`${I}ok${I}`)
  })

  it('wraps __underline__ in the underline control code (longest-first, not italic)', () => {
    expect(markdownToIrc('__note__')).toBe(`${U}note${U}`)
  })

  it('formats a span surrounded by other text', () => {
    expect(markdownToIrc('hey *team* now')).toBe(`hey ${B}team${B} now`)
  })

  it('allows internal spaces inside a span', () => {
    expect(markdownToIrc('*two words*')).toBe(`${B}two words${B}`)
  })

  it('handles multiple spans in one string', () => {
    expect(markdownToIrc('*a* and _b_')).toBe(`${B}a${B} and ${I}b${I}`)
  })
})

describe('markdownToIrc — color', () => {
  it('wraps #N(text) as a foreground color span', () => {
    expect(markdownToIrc('#4(ship it)')).toBe(`${C}4ship it${C}`)
  })

  it('wraps #N,M(text) as foreground+background', () => {
    expect(markdownToIrc('#0,1(x)')).toBe(`${C}0,1x${C}`)
  })

  it('accepts two-digit color numbers up to 15', () => {
    expect(markdownToIrc('#12(hi)')).toBe(`${C}12hi${C}`)
  })

  it('does not treat numbers > 15 as color', () => {
    expect(markdownToIrc('#16(x)')).toBe('#16(x)')
    expect(markdownToIrc('#99(x)')).toBe('#99(x)')
  })

  it('requires the paren immediately after the number (space disables it)', () => {
    expect(markdownToIrc('issue #4 (later)')).toBe('issue #4 (later)')
  })

  it('leaves a bare #N without parens untouched', () => {
    expect(markdownToIrc('see #4 done')).toBe('see #4 done')
  })
})

describe('markdownToIrc — hug-non-space rule', () => {
  it('does not format when the opening delimiter is followed by a space', () => {
    expect(markdownToIrc('a * b * c')).toBe('a * b * c')
  })

  it('does not format when the closing delimiter is preceded by a space', () => {
    expect(markdownToIrc('*team *')).toBe('*team *')
  })

  it('leaves lone arithmetic asterisks alone', () => {
    expect(markdownToIrc('3 * 4 * 5')).toBe('3 * 4 * 5')
  })
})

describe('markdownToIrc — unclosed / empty', () => {
  it('leaves an unclosed delimiter literal', () => {
    expect(markdownToIrc('*hello')).toBe('*hello')
  })

  it('leaves empty delimiters literal', () => {
    expect(markdownToIrc('**')).toBe('**')
  })
})

describe('markdownToIrc — escapes', () => {
  it('treats \\* as a literal asterisk and strips the backslash', () => {
    expect(markdownToIrc('\\*not bold\\*')).toBe('*not bold*')
  })

  it('escapes an arithmetic asterisk', () => {
    expect(markdownToIrc('2 \\* 3')).toBe('2 * 3')
  })

  it('escapes underscore and hash', () => {
    expect(markdownToIrc('a\\_b \\#4(x)')).toBe('a_b #4(x)')
  })

  it('treats a double backslash as a literal backslash', () => {
    expect(markdownToIrc('a\\\\b')).toBe('a\\b')
  })
})

describe('markdownToIrc — URLs are skipped', () => {
  it('does not italicize underscores inside a URL', () => {
    expect(markdownToIrc('http://foo_bar_baz')).toBe('http://foo_bar_baz')
  })

  it('leaves a URL untouched while still formatting around it', () => {
    expect(markdownToIrc('_hi_ http://a.com/x_y_z')).toBe(`${I}hi${I} http://a.com/x_y_z`)
  })
})

describe('markdownToIrc — nesting', () => {
  it('converts formatting inside a color span (recursive)', () => {
    expect(markdownToIrc('#4(*bold red*)')).toBe(`${C}4${B}bold red${B}${C}`)
  })
})

describe('stripMarkup', () => {
  it('removes bold/italic/underline markup, keeping the text', () => {
    expect(stripMarkup('*bold* _it_ __u__')).toBe('bold it u')
  })

  it('removes color markup, keeping the text', () => {
    expect(stripMarkup('#4(red) and #0,1(inv)')).toBe('red and inv')
  })

  it('leaves plain text unchanged', () => {
    expect(stripMarkup('nothing here')).toBe('nothing here')
  })

  it('resolves escapes to their literal characters', () => {
    expect(stripMarkup('\\*kept\\*')).toBe('*kept*')
  })
})

describe('hasMarkup', () => {
  it('is false for plain text', () => {
    expect(hasMarkup('just text')).toBe(false)
  })

  it('is false for a URL with underscores', () => {
    expect(hasMarkup('http://a_b_c')).toBe(false)
  })

  it('is true when markup is present', () => {
    expect(hasMarkup('*hi*')).toBe(true)
    expect(hasMarkup('#4(x)')).toBe(true)
  })
})
