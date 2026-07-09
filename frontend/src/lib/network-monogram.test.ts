import { describe, it, expect } from 'vitest';
import { networkMonogram } from './network-monogram';

describe('networkMonogram', () => {
  it('takes first letters of the first two words', () => {
    expect(networkMonogram('Open Free Node')).toBe('OF');
    expect(networkMonogram('Libera Chat')).toBe('LC');
  });
  it('splits on punctuation too', () => {
    expect(networkMonogram('Libera.Chat')).toBe('LC');
  });
  it('uses first two letters of a single word', () => {
    expect(networkMonogram('Libera')).toBe('Li');
    expect(networkMonogram('x')).toBe('X');
  });
  it('falls back to ? for empty', () => {
    expect(networkMonogram('   ')).toBe('?');
    expect(networkMonogram('')).toBe('?');
  });
});
