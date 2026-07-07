import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { IRCFormattedText, tokenizeText, stripIRCFormatting } from '../irc-formatted-text'
import { useNetworkStore } from '../../stores/network'

describe('IRCFormattedText', () => {
  it('renders plain text without formatting', () => {
    render(<IRCFormattedText text="Hello, world!" />)
    expect(screen.getByText('Hello, world!')).toBeInTheDocument()
  })

  it('renders empty string without error', () => {
    const { container } = render(<IRCFormattedText text="" />)
    expect(container.querySelector('span')).toBeInTheDocument()
  })

  it('applies custom className to wrapper', () => {
    const { container } = render(
      <IRCFormattedText text="test" className="my-class" />
    )
    expect(container.firstChild).toHaveClass('my-class')
  })

  describe('bold formatting (0x02)', () => {
    it('renders bold text', () => {
      const text = 'normal \x02bold\x02 normal'
      const { container } = render(<IRCFormattedText text={text} />)
      const boldSpan = container.querySelector('.font-bold')
      expect(boldSpan).toBeInTheDocument()
      expect(boldSpan).toHaveTextContent('bold')
    })

    it('toggles bold off', () => {
      const text = '\x02bold\x02not bold'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveClass('font-bold')
      expect(spans[1]).not.toHaveClass('font-bold')
    })
  })

  describe('italic formatting (0x1D)', () => {
    it('renders italic text', () => {
      const text = '\x1Ditalic\x1D'
      const { container } = render(<IRCFormattedText text={text} />)
      const italicSpan = container.querySelector('.italic')
      expect(italicSpan).toBeInTheDocument()
      expect(italicSpan).toHaveTextContent('italic')
    })
  })

  describe('underline formatting (0x1F)', () => {
    it('renders underlined text', () => {
      const text = '\x1Funderline\x1F'
      const { container } = render(<IRCFormattedText text={text} />)
      const underlineSpan = container.querySelector('.underline')
      expect(underlineSpan).toBeInTheDocument()
      expect(underlineSpan).toHaveTextContent('underline')
    })
  })

  describe('combined formatting', () => {
    it('applies bold and italic together', () => {
      const text = '\x02\x1Dbold italic\x1D\x02'
      const { container } = render(<IRCFormattedText text={text} />)
      const span = container.querySelector('.font-bold.italic')
      expect(span).toBeInTheDocument()
      expect(span).toHaveTextContent('bold italic')
    })
  })

  describe('color formatting (0x03)', () => {
    it('applies foreground color', () => {
      // \x03 followed by color number 4 (red) then text
      const text = '\x034red text\x03'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      const coloredSpan = spans[0]
      expect(coloredSpan).toHaveTextContent('red text')
      expect(coloredSpan).toHaveStyle({ color: '#FF0000' })
    })

    it('applies foreground and background color', () => {
      // \x03 followed by fg,bg (4,2 = red on blue)
      const text = '\x034,2colored\x03'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      const coloredSpan = spans[0]
      expect(coloredSpan).toHaveTextContent('colored')
      expect(coloredSpan).toHaveStyle({
        color: '#FF0000',
        backgroundColor: '#00007F',
      })
    })

    it('handles two-digit color codes', () => {
      // Color 12 = Light Blue
      const text = '\x0312light blue\x03'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveStyle({ color: '#0000FC' })
    })

    it('resets color when bare 0x03 is used', () => {
      const text = '\x034red\x03normal'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveStyle({ color: '#FF0000' })
      // Second span should have no inline color
      expect((spans[1] as HTMLElement).style.color).toBe('')
    })
  })

  describe('reset formatting (0x0F)', () => {
    it('resets all formatting', () => {
      const text = '\x02\x1D\x034bold italic red\x0Fnormal'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      // First segment has bold + italic + color
      expect(spans[0]).toHaveClass('font-bold')
      expect(spans[0]).toHaveClass('italic')
      expect(spans[0]).toHaveStyle({ color: '#FF0000' })
      // Second segment after reset has none
      expect(spans[1]).not.toHaveClass('font-bold')
      expect(spans[1]).not.toHaveClass('italic')
      expect((spans[1] as HTMLElement).style.color).toBe('')
    })
  })

  describe('reverse video (0x16)', () => {
    it('swaps foreground and background when both are set', () => {
      // fg=4 (red), bg=2 (blue), then reverse
      const text = '\x034,2\x16reversed\x16'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      // After reverse: fg should become bg and bg should become fg
      expect(spans[0]).toHaveStyle({
        color: '#00007F',       // was bg (blue)
        backgroundColor: '#FF0000', // was fg (red)
      })
    })
  })

  describe('extended color palette (16-98)', () => {
    it('applies an extended foreground color', () => {
      // Color 20 = dark green in the mIRC extended palette
      const text = '\x0320extended\x03'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveTextContent('extended')
      expect(spans[0]).toHaveStyle({ color: '#004700' })
    })

    it('applies an extended background color', () => {
      // fg=52 (bright red), bg=94 (grey) in the extended palette
      const text = '\x0352,94extended bg\x03'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveStyle({
        color: '#FF0000',
        backgroundColor: '#818181',
      })
    })
  })

  describe('hex color (0x04)', () => {
    it('applies a hex foreground color', () => {
      const text = '\x04FF00AAhex\x04'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveTextContent('hex')
      expect(spans[0]).toHaveStyle({ color: '#FF00AA' })
    })

    it('applies hex foreground and background colors', () => {
      const text = '\x04FF00AA,00FF00hex\x04'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveStyle({
        color: '#FF00AA',
        backgroundColor: '#00FF00',
      })
    })

    it('resets hex color when bare 0x04 is used', () => {
      const text = '\x04FF00AAhex\x04normal'
      const { container } = render(<IRCFormattedText text={text} />)
      const spans = container.querySelectorAll('span > span')
      expect(spans[0]).toHaveStyle({ color: '#FF00AA' })
      expect((spans[1] as HTMLElement).style.color).toBe('')
    })
  })

  describe('strikethrough (0x1E)', () => {
    it('renders struck-through text', () => {
      const text = '\x1Estruck\x1E'
      const { container } = render(<IRCFormattedText text={text} />)
      const span = container.querySelector('.line-through')
      expect(span).toBeInTheDocument()
      expect(span).toHaveTextContent('struck')
    })
  })

  describe('monospace (0x11)', () => {
    it('renders monospace text', () => {
      const text = '\x11code\x11'
      const { container } = render(<IRCFormattedText text={text} />)
      const span = container.querySelector('.font-mono')
      expect(span).toBeInTheDocument()
      expect(span).toHaveTextContent('code')
    })
  })
})

describe('IRCFormattedText channel links', () => {
  it('renders a #channel as a clickable button when networkId is set', () => {
    render(<IRCFormattedText text="join #test please" networkId={1} />)
    const btn = screen.getByRole('button', { name: '#test' })
    expect(btn).toBeInTheDocument()
  })

  it('renders #channel as plain text when networkId is absent', () => {
    render(<IRCFormattedText text="join #test please" />)
    expect(screen.queryByRole('button', { name: '#test' })).toBeNull()
    expect(screen.getByText(/#test/)).toBeInTheDocument()
  })

  it('clicking a channel calls openOrJoinChannel with the network and channel', () => {
    const spy = vi
      .spyOn(useNetworkStore.getState(), 'openOrJoinChannel')
      .mockResolvedValue(undefined)
    render(<IRCFormattedText text="see #test" networkId={7} />)
    fireEvent.click(screen.getByRole('button', { name: '#test' }))
    expect(spy).toHaveBeenCalledWith(7, '#test')
    spy.mockRestore()
  })
})

describe('tokenizeText', () => {
  it('returns a single text token for plain text', () => {
    expect(tokenizeText('hello world')).toEqual([
      { type: 'text', value: 'hello world' },
    ])
  })

  it('detects a #channel token', () => {
    expect(tokenizeText('join #test now')).toEqual([
      { type: 'text', value: 'join ' },
      { type: 'channel', value: '#test', trailing: '' },
      { type: 'text', value: ' now' },
    ])
  })

  it('strips trailing punctuation from a channel into trailing', () => {
    expect(tokenizeText('see #test.')).toEqual([
      { type: 'text', value: 'see ' },
      { type: 'channel', value: '#test', trailing: '.' },
    ])
  })

  it('does not treat a bare # as a channel', () => {
    expect(tokenizeText('a # b')).toEqual([{ type: 'text', value: 'a # b' }])
  })

  it('does not treat a #fragment inside a URL as a channel', () => {
    const tokens = tokenizeText('go https://x.com/p#section ok')
    expect(tokens.some((t) => t.type === 'channel')).toBe(false)
    expect(tokens.find((t) => t.type === 'url')?.value).toBe('https://x.com/p#section')
  })

  it('detects both a url and a channel in one string', () => {
    const tokens = tokenizeText('https://x.com and #chan')
    expect(tokens.map((t) => t.type)).toEqual(['url', 'text', 'channel'])
  })

  it('strips trailing ] from a channel in square brackets', () => {
    expect(tokenizeText('see [#test]')).toEqual([
      { type: 'text', value: 'see [' },
      { type: 'channel', value: '#test', trailing: ']' },
    ])
  })

  it('strips trailing " from a channel in double quotes', () => {
    expect(tokenizeText('say "#test"')).toEqual([
      { type: 'text', value: 'say "' },
      { type: 'channel', value: '#test', trailing: '"' },
    ])
  })

  it('strips trailing > from a channel in angle brackets', () => {
    expect(tokenizeText('go <#test>')).toEqual([
      { type: 'text', value: 'go <' },
      { type: 'channel', value: '#test', trailing: '>' },
    ])
  })

  it('preserves uppercase in channel names', () => {
    expect(tokenizeText('join #FooBar')).toEqual([
      { type: 'text', value: 'join ' },
      { type: 'channel', value: '#FooBar', trailing: '' },
    ])
  })
})

describe('stripIRCFormatting', () => {
  it('returns plain text unchanged', () => {
    expect(stripIRCFormatting('Hello, world!')).toBe('Hello, world!')
  })

  it('strips bold, italic, underline, strikethrough, monospace, reverse, reset', () => {
    expect(stripIRCFormatting('\x02bold\x02 \x1Dit\x1D \x1Fund\x1F \x1Estrk\x1E \x11mono\x11 \x16rev\x16 \x0Freset'))
      .toBe('bold it und strk mono rev reset')
  })

  it('strips indexed color codes with optional background', () => {
    expect(stripIRCFormatting('\x0304red\x03 and \x0304,01onblack\x03 done')).toBe('red and onblack done')
  })

  it('strips a bare color reset', () => {
    expect(stripIRCFormatting('a\x03b')).toBe('ab')
  })

  it('strips hex color codes (0x04) with optional background', () => {
    expect(stripIRCFormatting('\x04FF0000red\x04 \x04FF0000,0000FFpair\x04')).toBe('red pair')
  })

  it('leaves ordinary digits after stripped text intact', () => {
    // The hostname-style payload from the screenshot: services wrap it in bold.
    expect(stripIRCFormatting('Last login from: \x02~matt0x6f@75.164.85.141\x02 on Jul 06'))
      .toBe('Last login from: ~matt0x6f@75.164.85.141 on Jul 06')
  })

  it('covers 0x04 and 0x1E that the outbound markup stripper omits', () => {
    expect(stripIRCFormatting('\x1Egone\x1E \x04ABCDEFhex')).toBe('gone hex')
  })
})
