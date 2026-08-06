import { describe, expect, it, vi } from 'vitest';
import { runWhenDocumentIsLoaded } from './registerServiceWorker';

describe('service worker registration scheduling', () => {
  it('registers immediately when the load event has already fired', () => {
    const register = vi.fn();
    const addLoadListener = vi.fn();

    runWhenDocumentIsLoaded(register, 'complete', addLoadListener);

    expect(register).toHaveBeenCalledOnce();
    expect(addLoadListener).not.toHaveBeenCalled();
  });

  it('waits for the load event while the document is still loading', () => {
    const register = vi.fn();
    let loadListener: (() => void) | undefined;

    runWhenDocumentIsLoaded(register, 'loading', (listener) => {
      loadListener = listener;
    });

    expect(register).not.toHaveBeenCalled();
    loadListener?.();
    expect(register).toHaveBeenCalledOnce();
  });
});
