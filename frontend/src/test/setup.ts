import '@testing-library/jest-dom/vitest';

// Node >= 22 exposes an experimental global `localStorage` that is undefined
// unless --localstorage-file is passed. In happy-dom it lands as an own
// undefined-valued property on window, shadowing the real storage.
if (typeof window !== 'undefined' && typeof localStorage?.getItem !== 'function') {
  const store = new Map<string, string>();
  const impl = {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => { store.set(String(k), String(v)); },
    removeItem: (k: string) => { store.delete(k); },
    clear: () => { store.clear(); },
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size; },
  };
  Object.defineProperty(globalThis, 'localStorage', { value: impl, configurable: true, writable: true });
  Object.defineProperty(window, 'localStorage', { value: impl, configurable: true, writable: true });
}
// jsdom lacks matchMedia; useTheme touches it on import paths.
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList;
}

// Scroll helpers occasionally invoked by components under test.
if (typeof window !== 'undefined') {
  window.scrollTo = window.scrollTo || (() => {});
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = () => {};
  }
}
