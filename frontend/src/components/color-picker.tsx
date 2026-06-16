import { useRef, useState } from 'react';
import { Palette } from 'lucide-react';
import { IRC_COLORS } from './irc-formatted-text';
import { useDismiss } from '../hooks/useDismiss';

const COLOR_INDEXES = Array.from({ length: 16 }, (_, i) => i);

/**
 * 16-colour IRC picker. Foreground is required (clicking a text swatch applies
 * and closes); background is optional and set first. Emits the chosen indexes;
 * the toolbar turns them into `#fg(...)` / `#fg,bg(...)` markup.
 */
export function ColorPicker({ onPick }: { onPick: (fg: number, bg: number | null) => void }) {
  const [open, setOpen] = useState(false);
  const [bg, setBg] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  useDismiss(open, () => setOpen(false), ref);

  const swatchStyle = (idx: number, selected: boolean) => ({
    background: IRC_COLORS[idx],
    boxShadow: selected ? '0 0 0 2px var(--card), 0 0 0 4px var(--primary)' : undefined,
  });

  return (
    <div ref={ref} className="relative inline-flex">
      <button
        type="button"
        title="Text colour"
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setOpen((v) => !v)}
        className="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/50 transition-colors"
      >
        <Palette size={16} />
      </button>

      {open && (
        <div className="absolute bottom-full left-0 mb-2 z-50 w-56 p-3 rounded-lg border border-border bg-card shadow-[var(--shadow-lg)]">
          <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-1.5">Text colour</div>
          <div className="grid grid-cols-8 gap-1.5">
            {COLOR_INDEXES.map((i) => (
              <button
                key={`fg-${i}`}
                type="button"
                title={`Colour ${i}`}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => { onPick(i, bg); setOpen(false); }}
                className="w-6 h-6 rounded-md border border-border/70 transition-transform hover:scale-110"
                style={swatchStyle(i, false)}
              />
            ))}
          </div>

          <div className="text-[11px] uppercase tracking-wide text-muted-foreground mt-3 mb-1.5">Background (optional)</div>
          <div className="grid grid-cols-8 gap-1.5">
            <button
              type="button"
              title="No background"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => setBg(null)}
              className="w-6 h-6 rounded-md border border-border/70 flex items-center justify-center text-[10px] text-muted-foreground"
              style={{ boxShadow: bg === null ? '0 0 0 2px var(--card), 0 0 0 4px var(--primary)' : undefined }}
            >
              ∅
            </button>
            {COLOR_INDEXES.slice(1).map((i) => (
              <button
                key={`bg-${i}`}
                type="button"
                title={`Background ${i}`}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => setBg((cur) => (cur === i ? null : i))}
                className="w-6 h-6 rounded-md border border-border/70 transition-transform hover:scale-110"
                style={swatchStyle(i, bg === i)}
              />
            ))}
          </div>

          <p className="text-[11px] text-muted-foreground mt-2.5">
            Pick a text colour to apply{bg !== null ? ` on background ${bg}` : ''}.
          </p>
        </div>
      )}
    </div>
  );
}
