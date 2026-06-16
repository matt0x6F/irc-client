import { Suspense, lazy, useRef, useState } from 'react';
import { Smile } from 'lucide-react';
import type { EmojiClickData, EmojiStyle, Theme } from 'emoji-picker-react';
import { useDismiss } from '../hooks/useDismiss';

// The heavy picker (and its bundled emoji data) load only when first opened.
const EmojiPicker = lazy(() => import('emoji-picker-react'));

// Enum string values cast to the prop types. Using `import type` above keeps the
// enums' runtime objects out of this module, so the package stays code-split.
const NATIVE_STYLE = 'native' as unknown as EmojiStyle;
const DARK_THEME = 'dark' as unknown as Theme;
const LIGHT_THEME = 'light' as unknown as Theme;

/** Emoji button + lazy popover. Native (system) glyphs — no image CDN, works offline. */
export function EmojiPickerPopover({ onPick }: { onPick: (emoji: string) => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(open, () => setOpen(false), ref);

  const isDark =
    typeof document !== 'undefined' && document.documentElement.classList.contains('dark');

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        type="button"
        title="Emoji"
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setOpen((v) => !v)}
        className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/50 transition-colors"
      >
        <Smile size={16} />
      </button>

      {open && (
        <div className="absolute bottom-full left-0 mb-2 z-50">
          <Suspense
            fallback={
              <div className="p-4 text-sm text-muted-foreground bg-card border border-border rounded-lg shadow-[var(--shadow-lg)]">
                Loading emoji…
              </div>
            }
          >
            <EmojiPicker
              onEmojiClick={(d: EmojiClickData) => { onPick(d.emoji); setOpen(false); }}
              emojiStyle={NATIVE_STYLE}
              theme={isDark ? DARK_THEME : LIGHT_THEME}
              skinTonesDisabled
              width={320}
              height={400}
            />
          </Suspense>
        </div>
      )}
    </div>
  );
}
