import { useEffect, RefObject } from 'react';

/**
 * Close a popover on Escape or a pointer-down outside `ref`. Mirrors the
 * hand-rolled overlay pattern used by the app's modals (no Radix dependency).
 * `ref` should wrap both the trigger and the panel so clicking the trigger to
 * toggle isn't treated as an outside click.
 */
export function useDismiss(
  open: boolean,
  onClose: () => void,
  ref: RefObject<HTMLElement | null>,
): void {
  useEffect(() => {
    if (!open) return;

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    const onPointerDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };

    window.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onPointerDown);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onPointerDown);
    };
  }, [open, onClose, ref]);
}
