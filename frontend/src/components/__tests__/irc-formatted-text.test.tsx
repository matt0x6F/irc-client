import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { IRCFormattedText } from '../irc-formatted-text'

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
})
