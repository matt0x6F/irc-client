import React from 'react';
import { LinkPreviewCard, PreviewChip } from './link-preview-card';

interface IRCFormatState {
  bold: boolean;
  italic: boolean;
  underline: boolean;
  reverse: boolean;
  fgColor: number | null;
  bgColor: number | null;
}

// IRC color palette (standard 16 colors)
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
};

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
    reverse: false,
    fgColor: null,
    bgColor: null,
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
        if (fg) {
          const fgNum = parseInt(fg, 10);
          format.fgColor = fgNum >= 0 && fgNum <= 15 ? fgNum : null;
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
            format.bgColor = bgNum >= 0 && bgNum <= 15 ? bgNum : null;
          }
        } else {
          format.bgColor = null;
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
          reverse: false,
          fgColor: null,
          bgColor: null,
        };
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
const URL_REGEX = new RegExp(URL_REGEX_SOURCE, 'g');

// Renders a text string, replacing URLs with clickable <a> tags.
// Returns an array of React nodes (strings and <a> elements).
// When onPreview is provided, an inline PreviewChip is emitted after each link.
function renderTextWithLinks(
  text: string,
  keyPrefix: string,
  onPreview?: (url: string) => void,
): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  // Reset regex state
  URL_REGEX.lastIndex = 0;

  while ((match = URL_REGEX.exec(text)) !== null) {
    // Add text before the URL
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }

    const url = match[0];
    // Strip trailing punctuation that's likely not part of the URL
    let cleanUrl = url;
    const trailingPunct = /[),.:;!?]+$/;
    const trailingMatch = cleanUrl.match(trailingPunct);
    let suffix = '';
    if (trailingMatch) {
      const stripped = trailingMatch[0];
      // Count open/close parens to handle URLs like https://en.wikipedia.org/wiki/Example_(disambiguation)
      const openParens = (cleanUrl.match(/\(/g) || []).length;
      const closeParens = (cleanUrl.match(/\)/g) || []).length;
      if (stripped === ')' && openParens >= closeParens) {
        // Parens are balanced, keep the URL as is
      } else {
        cleanUrl = cleanUrl.slice(0, cleanUrl.length - stripped.length);
        suffix = stripped;
      }
    }

    nodes.push(
      <a
        key={`${keyPrefix}-link-${match.index}`}
        href={cleanUrl}
        target="_blank"
        rel="noopener noreferrer"
        className="text-primary underline hover:text-primary/80"
      >
        {cleanUrl}
      </a>
    );

    if (onPreview) {
      const u = cleanUrl;
      nodes.push(
        <PreviewChip
          key={`${keyPrefix}-chip-${match.index}`}
          onClick={() => onPreview(u)}
        />,
      );
    }

    if (suffix) {
      nodes.push(suffix);
    }

    lastIndex = match.index + url.length;
  }

  // Add remaining text after the last URL
  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }

  return nodes.length > 0 ? nodes : [text];
}

interface IRCFormattedTextProps {
  text: string;
  className?: string;
  /** When true, each link gets an inline Preview chip that expands a card below the message. */
  enableUnfurls?: boolean;
}

export function IRCFormattedText({ text, className = '', enableUnfurls }: IRCFormattedTextProps) {
  const [expanded, setExpanded] = React.useState<string[]>([]);
  const segments = parseIRCFormatting(text);

  // Shared helper that builds the formatted segment nodes
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

      if (segment.format.fgColor !== null) {
        style.color = IRC_COLORS[segment.format.fgColor] || IRC_COLORS[0];
      }
      if (segment.format.bgColor !== null) {
        style.backgroundColor = IRC_COLORS[segment.format.bgColor] || IRC_COLORS[1];
      }

      // Reverse video swaps foreground and background
      if (segment.format.reverse) {
        if (segment.format.fgColor !== null && segment.format.bgColor !== null) {
          const temp = style.color;
          style.color = style.backgroundColor;
          style.backgroundColor = temp;
        } else if (segment.format.fgColor !== null) {
          style.backgroundColor = style.color;
          style.color = IRC_COLORS[1]; // Default to black background
        } else if (segment.format.bgColor !== null) {
          style.color = style.backgroundColor;
          style.backgroundColor = IRC_COLORS[0]; // Default to white background
        }
      }

      return (
        <span key={index} className={classes.join(' ')} style={style}>
          {renderTextWithLinks(segment.text, `seg-${index}`, onPreview)}
        </span>
      );
    });
  }

  // ── Path A: enableUnfurls is falsy (default) ──────────────────────────────
  // Return the EXISTING span structure unchanged so existing tests, type checks,
  // and message-preview.tsx are unaffected.
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

