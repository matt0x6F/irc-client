import { useEffect, type ReactNode } from 'react';

interface ModalProps {
  onClose: () => void;
  title?: string;
  size?: 'sm' | 'md';
  children: ReactNode;
}

const SIZE: Record<'sm' | 'md', string> = {
  sm: 'max-w-md',
  md: 'max-w-2xl',
};

export function Modal({ onClose, title, size = 'md', children }: ModalProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`flex max-h-[85vh] w-full ${SIZE[size]} min-w-0 flex-col overflow-hidden rounded-lg border border-border bg-card shadow-[var(--shadow-lg)]`}
        style={{ backgroundColor: 'var(--card)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {title !== undefined && (
          <div className="flex flex-shrink-0 items-center justify-between border-b border-border px-4 py-3">
            <h3 className="font-semibold">{title}</h3>
            <button
              onClick={onClose}
              aria-label="Close"
              className="text-muted-foreground hover:text-foreground"
            >
              ×
            </button>
          </div>
        )}
        <div className="min-w-0 overflow-y-auto p-4">{children}</div>
      </div>
    </div>
  );
}
