// Pure helpers for editing a text input's value around its current selection.
// They return the new value plus where the selection should land, so the caller
// (a React component) can update state and restore the caret without these
// helpers touching the DOM — which keeps them trivially testable.

export interface InsertResult {
  value: string;
  selectionStart: number;
  selectionEnd: number;
}

/**
 * Wrap the selected range [start, end) with `before`/`after` delimiters.
 * With a real selection the inner text stays selected (so toggles can chain);
 * with a collapsed caret the delimiters are inserted empty and the caret lands
 * between them, ready to type.
 */
export function wrapSelection(
  value: string,
  start: number,
  end: number,
  before: string,
  after: string,
): InsertResult {
  const newValue = value.slice(0, start) + before + value.slice(start, end) + after + value.slice(end);
  return {
    value: newValue,
    selectionStart: start + before.length,
    selectionEnd: end + before.length,
  };
}

/** Replace the selected range [start, end) with `text`, caret after it. */
export function insertAtCaret(value: string, start: number, end: number, text: string): InsertResult {
  const newValue = value.slice(0, start) + text + value.slice(end);
  const caret = start + text.length;
  return { value: newValue, selectionStart: caret, selectionEnd: caret };
}

/**
 * Apply an InsertResult to a controlled input: push the new value through the
 * React setter, then restore focus and selection on the next tick (the same
 * setTimeout idiom input-area.tsx already uses for tab-completion).
 */
export function applyToInput(
  input: HTMLInputElement | null,
  result: InsertResult,
  setValue: (value: string) => void,
): void {
  setValue(result.value);
  setTimeout(() => {
    if (!input) return;
    input.focus();
    input.setSelectionRange(result.selectionStart, result.selectionEnd);
  }, 0);
}
