import { IRCFormattedText } from './irc-formatted-text';
import { markdownToIrc } from '../lib/irc-markup';

/**
 * Live "what will be sent" line. Renders the converted message through the same
 * IRC parser used for incoming messages, so the preview matches the wire exactly.
 * Shown only when the text actually contains markup.
 */
export function MessagePreview({ message }: { message: string }) {
  const wire = markdownToIrc(message);
  if (wire === message) return null;

  return (
    <div className="px-1 pb-2" data-testid="message-preview">
      <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-1">Preview</div>
      <div className="text-sm px-3 py-1.5 rounded-lg bg-muted/40 border border-border break-words whitespace-pre-wrap">
        <IRCFormattedText text={wire} />
      </div>
    </div>
  );
}
