import React from 'react';

interface IRCFormatState {
  bold: boolean;
  italic: boolean;
  underline: boolean;
  reverse: boolean;
  fgColor: number | null;
  bgColor: number | null;
}

// IRC color palette (standard 16 colors)
const IRC_COLORS: Record<number, string> = {
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

interface IRCFormattedTextProps {
  text: string;
  className?: string;
}

export function IRCFormattedText({ text, className = '' }: IRCFormattedTextProps) {
  const segments = parseIRCFormatting(text);

  if (segments.length === 0) {
    return <span className={className}>{text}</span>;
  }

  return (
    <span className={className}>
      {segments.map((segment, index) => {
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
            {segment.text}
          </span>
        );
      })}
    </span>
  );
}

