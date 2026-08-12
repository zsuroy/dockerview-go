import '@testing-library/jest-dom/vitest';

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
