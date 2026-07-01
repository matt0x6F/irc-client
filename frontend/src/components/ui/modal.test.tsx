import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { Modal } from './modal';

describe('Modal', () => {
  it('renders title and children inside a dialog', () => {
    render(<Modal title="User Info" onClose={() => {}}>hello body</Modal>);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('User Info')).toBeInTheDocument();
    expect(screen.getByText('hello body')).toBeInTheDocument();
  });

  it('closes on Escape', () => {
    const onClose = vi.fn();
    render(<Modal onClose={onClose}>x</Modal>);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes on backdrop click but not on card click', () => {
    const onClose = vi.fn();
    render(<Modal title="T" onClose={onClose}>body</Modal>);
    fireEvent.click(screen.getByText('body')); // inside card
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('dialog').parentElement as HTMLElement); // backdrop
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('bounds height and scrolls its body', () => {
    render(<Modal title="T" onClose={() => {}}><div data-testid="b">c</div></Modal>);
    const body = screen.getByTestId('b').parentElement as HTMLElement;
    expect(body.className).toContain('overflow-y-auto');
    const card = screen.getByRole('dialog');
    expect(card.className).toContain('max-h-[85vh]');
    expect(card.className).toContain('min-w-0');
  });
});
