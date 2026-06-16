import { RefObject } from 'react';
import { Bold, Italic, Underline, RemoveFormatting, Type } from 'lucide-react';
import { usePreferencesStore } from '../stores/preferences';
import { wrapSelection, insertAtCaret, applyToInput, type InsertResult } from '../lib/input-insert';
import { stripMarkup } from '../lib/irc-markup';
import { ColorPicker } from './color-picker';
import { EmojiPickerPopover } from './emoji-picker-popover';
import { MentionPicker } from './mention-picker';

interface Props {
  inputRef: RefObject<HTMLInputElement | null>;
  value: string;
  setValue: (v: string) => void;
  networkId?: number | null;
  channelName?: string | null;
}

const BTN = 'text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/50 transition-colors';

/**
 * Composer toolbar. The B/I/U/colour/clear strip is gated by the persisted
 * preference; emoji, mention, and the "Aa" visibility toggle are always shown.
 * All buttons preventDefault on mousedown so the input keeps focus and its
 * selection while a formatting action is applied.
 */
export function FormattingToolbar({ inputRef, value, setValue, networkId, channelName }: Props) {
  const showStrip = usePreferencesStore((s) => s.showFormattingToolbar);
  const toggleStrip = usePreferencesStore((s) => s.toggleFormattingToolbar);

  const edit = (fn: (value: string, start: number, end: number) => InsertResult) => {
    const input = inputRef.current;
    const start = input?.selectionStart ?? value.length;
    const end = input?.selectionEnd ?? value.length;
    applyToInput(input, fn(value, start, end), setValue);
  };
  const wrap = (before: string, after: string) =>
    edit((v, s, e) => wrapSelection(v, s, e, before, after));
  const insert = (text: string) => edit((v, s, e) => insertAtCaret(v, s, e, text));
  const mention = (nick: string) =>
    edit((v, s, e) => insertAtCaret(v, s, e, nick + (s === 0 ? ': ' : ' ')));

  const clearFormatting = () =>
    edit((v, s, e) => {
      if (s === e) {
        const stripped = stripMarkup(v);
        return { value: stripped, selectionStart: stripped.length, selectionEnd: stripped.length };
      }
      return insertAtCaret(v, s, e, stripMarkup(v.slice(s, e)));
    });

  return (
    <div className="flex items-center gap-1 px-1 pb-2" data-testid="formatting-toolbar">
      {showStrip && (
        <>
          <button type="button" title="Bold (Ctrl/Cmd+B)" onMouseDown={(e) => e.preventDefault()} onClick={() => wrap('*', '*')} className={BTN}>
            <Bold size={16} />
          </button>
          <button type="button" title="Italic (Ctrl/Cmd+I)" onMouseDown={(e) => e.preventDefault()} onClick={() => wrap('_', '_')} className={BTN}>
            <Italic size={16} />
          </button>
          <button type="button" title="Underline (Ctrl/Cmd+U)" onMouseDown={(e) => e.preventDefault()} onClick={() => wrap('__', '__')} className={BTN}>
            <Underline size={16} />
          </button>
          <ColorPicker onPick={(fg, bg) => wrap(`#${fg}${bg != null ? ',' + bg : ''}(`, ')')} />
          <button type="button" title="Clear formatting" onMouseDown={(e) => e.preventDefault()} onClick={clearFormatting} className={BTN}>
            <RemoveFormatting size={16} />
          </button>
          <span className="w-px h-4 bg-border mx-1" />
        </>
      )}

      <EmojiPickerPopover onPick={insert} />
      <MentionPicker networkId={networkId} channelName={channelName} onPick={mention} />

      <span className="flex-1" />

      <button
        type="button"
        title={showStrip ? 'Hide formatting toolbar' : 'Show formatting toolbar'}
        aria-pressed={showStrip}
        onMouseDown={(e) => e.preventDefault()}
        onClick={toggleStrip}
        className={`${BTN} ${showStrip ? 'text-foreground bg-accent/40' : ''}`}
      >
        <Type size={16} />
      </button>
    </div>
  );
}
