import React from 'react';
import { LinkPreviewCard, PreviewChip } from './link-preview-card';
import { useNetworkStore } from '../stores/network';

interface IRCFormatState {
  bold: boolean;
  italic: boolean;
  underline: boolean;
  strikethrough: boolean;
  monospace: boolean;
  reverse: boolean;
  fgColor: number | null;
  bgColor: number | null;
  // Hex colors (0x04 RRGGBB). When set, they take precedence over the indexed
  // palette for the same span.
  fgHex: string | null;
  bgHex: string | null;
}

// IRC color palette: the standard 16 colors (0-15) plus the extended 16-98
// palette defined at https://modern.ircdocs.horse/formatting.html#colors.
// 99 is "default" (rendered as no explicit color).
export const IRC_COLORS: Record<number, string> = {
  0: '#FFFFFF', // White
  1: '#000000', // Black
  2: '#00007F', // Blue
  3: '#007F00', // Green
  4: '#FF0000', // Red
  5: '#7F0000', // Brown/Maroon
  6: '#9C009C', // Magenta
  7: '#FC7F00', // Orange
  8: '#FFFF00', // Yellow
  9: '#00FC00', // Light Green
  10: '#009393', // Cyan
  11: '#00FFFF', // Light Cyan
  12: '#0000FC', // Light Blue
  13: '#FF00FF', // Light Magenta
  14: '#7F7F7F', // Gray
  15: '#CFCFCF', // Light Gray
  // Extended palette (16-98).
  16: '#470000', 17: '#472100', 18: '#474700', 19: '#324700', 20: '#004700',
  21: '#00472C', 22: '#004747', 23: '#002747', 24: '#000047', 25: '#2E0047',
  26: '#470047', 27: '#47002A',
  28: '#740000', 29: '#743A00', 30: '#747400', 31: '#517400', 32: '#007400',
  33: '#007449', 34: '#007474', 35: '#004074', 36: '#000074', 37: '#4B0074',
  38: '#740074', 39: '#740045',
  40: '#B50000', 41: '#B56300', 42: '#B5B500', 43: '#7DB500', 44: '#00B500',
  45: '#00B571', 46: '#00B5B5', 47: '#0063B5', 48: '#0000B5', 49: '#7500B5',
  50: '#B500B5', 51: '#B5006B',
  52: '#FF0000', 53: '#FF8C00', 54: '#FFFF00', 55: '#B2FF00', 56: '#00FF00',
  57: '#00FFA0', 58: '#00FFFF', 59: '#008CFF', 60: '#0000FF', 61: '#A500FF',
  62: '#FF00FF', 63: '#FF0098',
  64: '#FF5959', 65: '#FFB459', 66: '#FFFF71', 67: '#CFFF60', 68: '#6FFF6F',
  69: '#65FFC9', 70: '#6DFFFF', 71: '#59B4FF', 72: '#5959FF', 73: '#C459FF',
  74: '#FF66FF', 75: '#FF59BC',
  76: '#FF9C9C', 77: '#FFD39C', 78: '#FFFF9C', 79: '#E2FF9C', 80: '#9CFF9C',
  81: '#9CFFDB', 82: '#9CFFFF', 83: '#9CD3FF', 84: '#9C9CFF', 85: '#DC9CFF',
  86: '#FF9CFF', 87: '#FF94D3',
  88: '#000000', 89: '#131313', 90: '#282828', 91: '#363636', 92: '#4D4D4D',
  93: '#656565', 94: '#818181', 95: '#9F9F9F', 96: '#BCBCBC', 97: '#E2E2E2',
  98: '#FFFFFF',
};

// The largest indexed color; 99 (default) and anything higher render as no color.
const MAX_COLOR_INDEX = 98;
const HEX_COLOR_AT = /^[0-9A-Fa-f]{6}/;

interface FormattedSegment {
  text: string;
  format: IRCFormatState;
}

function parseIRCFormatting(text: string): FormattedSegment[] {
  if (!text) return [];

  const segments: FormattedSegment[] = [];
  let currentText = '';
  let format: IRCFormatState = {
    bold: false,
    italic: false,
    underline: false,
    strikethrough: false,
    monospace: false,
    reverse: false,
    fgColor: null,
    bgColor: null,
    fgHex: null,
    bgHex: null,
  };

  let i = 0;
  while (i < text.length) {
    const char = text[i];
    const code = char.charCodeAt(0);

    switch (code) {
      case 0x02: // Bold
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.bold = !format.bold;
        i++;
        break;

      case 0x03: // Color
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        i++; // Skip the color code itself

        // Parse foreground color (1-2 digits)
        let fg = '';
        while (i < text.length && text[i] >= '0' && text[i] <= '9') {
          fg += text[i];
          i++;
        }
        // An indexed color code overrides any active hex color.
        format.fgHex = null;
        format.bgHex = null;
        if (fg) {
          const fgNum = parseInt(fg, 10);
          format.fgColor = fgNum >= 0 && fgNum <= MAX_COLOR_INDEX ? fgNum : null;
        } else {
          format.fgColor = null;
          format.bgColor = null;
        }

        // Parse background color (comma + 1-2 digits)
        if (i < text.length && text[i] === ',') {
          i++; // Skip comma
          let bg = '';
          while (i < text.length && text[i] >= '0' && text[i] <= '9') {
            bg += text[i];
            i++;
          }
          if (bg) {
            const bgNum = parseInt(bg, 10);
            format.bgColor = bgNum >= 0 && bgNum <= MAX_COLOR_INDEX ? bgNum : null;
          }
        } else {
          format.bgColor = null;
        }
        break;

      case 0x04: // Hex color (RRGGBB[,RRGGBB]); a bare 0x04 resets color.
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        i++;
        {
          const fgMatch = HEX_COLOR_AT.exec(text.slice(i));
          if (fgMatch) {
            // A hex color overrides any active indexed color.
            format.fgColor = null;
            format.bgColor = null;
            format.fgHex = '#' + fgMatch[0];
            i += 6;
            if (text[i] === ',') {
              const bgMatch = HEX_COLOR_AT.exec(text.slice(i + 1));
              if (bgMatch) {
                format.bgHex = '#' + bgMatch[0];
                i += 7;
              }
            }
          } else {
            format.fgHex = null;
            format.bgHex = null;
            format.fgColor = null;
            format.bgColor = null;
          }
        }
        break;

      case 0x0F: // Reset
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format = {
          bold: false,
          italic: false,
          underline: false,
          strikethrough: false,
          monospace: false,
          reverse: false,
          fgColor: null,
          bgColor: null,
          fgHex: null,
          bgHex: null,
        };
        i++;
        break;

      case 0x11: // Monospace
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.monospace = !format.monospace;
        i++;
        break;

      case 0x1E: // Strikethrough
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.strikethrough = !format.strikethrough;
        i++;
        break;

      case 0x16: // Reverse
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.reverse = !format.reverse;
        i++;
        break;

      case 0x1D: // Italic
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.italic = !format.italic;
        i++;
        break;

      case 0x1F: // Underline
        if (currentText) {
          segments.push({ text: currentText, format: { ...format } });
          currentText = '';
        }
        format.underline = !format.underline;
        i++;
        break;

      default:
        currentText += char;
        i++;
        break;
    }
  }

  // Push remaining text
  if (currentText) {
    segments.push({ text: currentText, format: { ...format } });
  }

  return segments;
}

// URL regex pattern to detect http and https URLs in text.
// The source is exported so other modules (e.g. irc-markup) can build their own
// anchored matcher from the same pattern without sharing mutable lastIndex state.
export const URL_REGEX_SOURCE = 'https?:\\/\\/[^\\s<>"{}|\\\\^`\\[\\]]+';

// Channel references: '#' followed by chanstring chars (no space or comma).
// '#' only — '&'/'+'/'!' are excluded to avoid prose false positives.
const CHANNEL_REGEX_SOURCE = '#[^\\s,]+';

// Single combined matcher: a URL OR a channel. URL is listed first so an
// in-URL '#fragment' is consumed by the URL branch before the channel branch
// can see it.
const TOKEN_REGEX = new RegExp(`(${URL_REGEX_SOURCE})|(${CHANNEL_REGEX_SOURCE})`, 'g');

export type TextToken =
  | { type: 'text'; value: string }
  | { type: 'url'; value: string; trailing: string }
  | { type: 'channel'; value: string; trailing: string };

// Splits raw text into text / url / channel tokens in one pass.
export function tokenizeText(text: string): TextToken[] {
  const tokens: TextToken[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  TOKEN_REGEX.lastIndex = 0;

  while ((match = TOKEN_REGEX.exec(text)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({ type: 'text', value: text.slice(lastIndex, match.index) });
    }

    if (match[1]) {
      // URL branch — preserve existing trailing-punctuation / paren handling.
      const url = match[1];
      let value = url;
      let trailing = '';
      const trailingMatch = value.match(/[),.:;!?]+$/);
      if (trailingMatch) {
        const stripped = trailingMatch[0];
        const openParens = (value.match(/\(/g) || []).length;
        const closeParens = (value.match(/\)/g) || []).length;
        if (stripped === ')' && openParens >= closeParens) {
          // balanced parens — keep as-is
        } else {
          value = value.slice(0, value.length - stripped.length);
          trailing = stripped;
        }
      }
      tokens.push({ type: 'url', value, trailing });
    } else {
      // Channel branch — strip trailing punctuation into `trailing`.
      const raw = match[2];
      let value = raw;
      let trailing = '';
      const trailingMatch = value.match(/[.,!?;:)\]}>"'`]+$/);
      if (trailingMatch) {
        trailing = trailingMatch[0];
        value = value.slice(0, value.length - trailing.length);
      }
      tokens.push({ type: 'channel', value, trailing });
    }

    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < text.length) {
    tokens.push({ type: 'text', value: text.slice(lastIndex) });
  }

  return tokens;
}

// Renders a text string into React nodes, turning URLs into <a> links and
// #channel references into clickable buttons (only when networkId is provided).
// When onPreview is provided, an inline PreviewChip is emitted after each link.
function renderTextWithLinks(
  text: string,
  keyPrefix: string,
  networkId?: number,
  onPreview?: (url: string) => void
): React.ReactNode[] {
  const tokens = tokenizeText(text);
  const nodes: React.ReactNode[] = [];

  tokens.forEach((token, i) => {
    if (token.type === 'url') {
      nodes.push(
        <a
          key={`${keyPrefix}-link-${i}`}
          href={token.value}
          target="_blank"
          rel="noopener noreferrer"
          className="text-primary underline hover:text-primary/80"
        >
          {token.value}
        </a>
      );
      if (onPreview) {
        const u = token.value;
        nodes.push(
          <PreviewChip key={`${keyPrefix}-chip-${i}`} onClick={() => onPreview(u)} />
        );
      }
      if (token.trailing) nodes.push(token.trailing);
    } else if (token.type === 'channel') {
      if (networkId !== undefined) {
        const channel = token.value;
        nodes.push(
          <button
            key={`${keyPrefix}-chan-${i}`}
            type="button"
            title={`Open ${channel}`}
            className="text-primary underline hover:text-primary/80 cursor-pointer"
            onClick={() => {
              void useNetworkStore.getState().openOrJoinChannel(networkId, channel);
            }}
          >
            {channel}
          </button>
        );
      } else {
        nodes.push(token.value);
      }
      if (token.trailing) nodes.push(token.trailing);
    } else {
      nodes.push(token.value);
    }
  });

  return nodes.length > 0 ? nodes : [text];
}

interface IRCFormattedTextProps {
  text: string;
  className?: string;
  /** When set, #channel references become clickable (switch to / join the channel). */
  networkId?: number;
  /** When true, each link gets an inline Preview chip that expands a card below the message. */
  enableUnfurls?: boolean;
}

export function IRCFormattedText({
  text,
  className = '',
  networkId,
  enableUnfurls,
}: IRCFormattedTextProps) {
  const [expanded, setExpanded] = React.useState<string[]>([]);
  const segments = parseIRCFormatting(text);

  // Shared helper that builds the formatted segment nodes. networkId is threaded
  // through so #channel references stay clickable in both render paths; onPreview
  // is only passed in the unfurl path.
  function renderSegments(onPreview?: (url: string) => void) {
    return segments.map((segment, index) => {
      const style: React.CSSProperties = {};
      const classes: string[] = [];

      if (segment.format.bold) {
        classes.push('font-bold');
      }
      if (segment.format.italic) {
        classes.push('italic');
      }
      if (segment.format.underline) {
        classes.push('underline');
      }
      if (segment.format.strikethrough) {
        classes.push('line-through');
      }
      if (segment.format.monospace) {
        classes.push('font-mono');
      }

      // Resolve the effective colors: a hex color (0x04) takes precedence over
      // the indexed palette (0x03) for the same span.
      const fg =
        segment.format.fgHex ??
        (segment.format.fgColor !== null
          ? IRC_COLORS[segment.format.fgColor] || IRC_COLORS[0]
          : null);
      const bg =
        segment.format.bgHex ??
        (segment.format.bgColor !== null
          ? IRC_COLORS[segment.format.bgColor] || IRC_COLORS[1]
          : null);

      if (segment.format.reverse) {
        // Reverse video swaps foreground and background.
        if (fg !== null && bg !== null) {
          style.color = bg;
          style.backgroundColor = fg;
        } else if (fg !== null) {
          style.backgroundColor = fg;
          style.color = IRC_COLORS[1]; // Default to black background
        } else if (bg !== null) {
          style.color = bg;
          style.backgroundColor = IRC_COLORS[0]; // Default to white background
        }
      } else {
        if (fg !== null) style.color = fg;
        if (bg !== null) style.backgroundColor = bg;
      }

      return (
        <span key={index} className={classes.join(' ')} style={style}>
          {renderTextWithLinks(segment.text, `seg-${index}`, networkId, onPreview)}
        </span>
      );
    });
  }

  // ── Path A: enableUnfurls is falsy (default) ──────────────────────────────
  // Return the EXISTING span structure unchanged so existing tests, type checks,
  // and message-preview.tsx are unaffected. #channel links still work via networkId.
  if (!enableUnfurls) {
    if (segments.length === 0) {
      return <span className={className}>{text}</span>;
    }
    return <span className={className}>{renderSegments()}</span>;
  }

  // ── Path B: enableUnfurls is truthy ───────────────────────────────────────
  // Wrap in a flex-col div so card <div>s can appear below the text <span>
  // without invalid block-in-inline nesting.
  const onPreview = (url: string) =>
    setExpanded((prev) => (prev.includes(url) ? prev : [...prev, url]));

  if (segments.length === 0) {
    return (
      <div className={`${className} flex flex-col gap-1`}>
        <span>{text}</span>
      </div>
    );
  }

  return (
    <div className={`${className} flex flex-col gap-1`}>
      <span>{renderSegments(onPreview)}</span>
      {expanded.map((url) => (
        <LinkPreviewCard key={url} url={url} />
      ))}
    </div>
  );
}
