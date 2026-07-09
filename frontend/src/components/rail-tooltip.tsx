import { useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

// Hover tooltip for rail tiles. The tile column is an overflow-clipped scroll
// container (overflow-y forces overflow-x out of `visible` too), so anything
// positioned inside it gets cut off at the rail edge — the tooltip must render
// through a portal with fixed positioning to sit beside the rail.
export function RailTooltip({ label, children }: { label: string; children: ReactNode }) {
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);

  return (
    <div
      onMouseEnter={(e) => {
        const r = e.currentTarget.getBoundingClientRect();
        setPos({ x: r.right + 10, y: r.top + r.height / 2 });
      }}
      onMouseLeave={() => setPos(null)}
      // HTML5 drags suppress mouseleave, so a drag would strand the tooltip.
      onDragStart={() => setPos(null)}
    >
      {children}
      {pos !== null && createPortal(
        <div
          role="tooltip"
          style={{
            position: 'fixed', left: pos.x, top: pos.y, transform: 'translateY(-50%)',
            zIndex: 60, pointerEvents: 'none', whiteSpace: 'nowrap',
            background: 'var(--popover)', color: 'var(--popover-foreground)',
            border: '1px solid var(--border)', borderRadius: 6, padding: '4px 10px',
            fontSize: 12, fontWeight: 500, boxShadow: '0 4px 12px rgb(0 0 0 / 0.15)',
          }}
        >
          {label}
        </div>,
        document.body,
      )}
    </div>
  );
}
