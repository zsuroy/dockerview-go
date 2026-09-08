// Same-tree SVG export. The button and the tests call this one function:
// it clones the live graph <svg> (the exact node on screen), inlines the
// computed colors so CSS variables still resolve in a standalone file,
// and serializes with XMLSerializer.
//
// Official reference:
//   "XMLSerializer: serializeToString() method - Web APIs | MDN"
//   https://developer.mozilla.org/en-US/docs/Web/API/XMLSerializer/serializeToString

const STYLE_PROPS = [
  'fill',
  'stroke',
  'stroke-width',
  'stroke-dasharray',
  'stroke-opacity',
  'fill-opacity',
  'opacity',
  'font-family',
  'font-size',
  'font-weight',
  'font-style',
] as const;

const SVG_NS = 'http://www.w3.org/2000/svg';

function readThemeVar(name: string): string {
  if (typeof window === 'undefined') return '';
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/**
 * Serialize the on-screen graph SVG to a standalone SVG string.
 * Every network and container name exists as <text> in this tree, so the
 * returned string is grep-able for all of them.
 */
export function serializeGraphSvg(source: SVGSVGElement): string {
  const clone = source.cloneNode(true) as SVGSVGElement;
  clone.setAttribute('xmlns', SVG_NS);
  clone.setAttribute('xmlns:xlink', 'http://www.w3.org/1999/xlink');
  clone.setAttribute('version', '1.1');

  const vb = source.viewBox.baseVal;
  if (vb && vb.width && vb.height) {
    clone.setAttribute('width', String(vb.width));
    clone.setAttribute('height', String(vb.height));
  }

  // Inline computed styles element-by-element so CSS custom properties
  // (var(--theme-*)) resolve in the downloaded file without the host page.
  const sourceEls = source.querySelectorAll('*');
  const cloneEls = clone.querySelectorAll('*');
  const count = Math.min(sourceEls.length, cloneEls.length);
  for (let i = 0; i < count; i++) {
    const cs = getComputedStyle(sourceEls[i]);
    const decls = STYLE_PROPS.map((prop) => {
      const value = cs.getPropertyValue(prop);
      return value ? `${prop}:${value}` : '';
    }).filter(Boolean);
    if (decls.length) cloneEls[i].setAttribute('style', decls.join(';'));
  }

  // Paint a themed background as the first child.
  const bg = readThemeVar('--bg') || readThemeVar('--theme-bg');
  if (bg && vb && vb.width && vb.height) {
    const rect = document.createElementNS(SVG_NS, 'rect');
    rect.setAttribute('x', '0');
    rect.setAttribute('y', '0');
    rect.setAttribute('width', String(vb.width));
    rect.setAttribute('height', String(vb.height));
    rect.setAttribute('fill', bg);
    clone.insertBefore(rect, clone.firstChild);
  }

  const body = new XMLSerializer().serializeToString(clone);
  return `<?xml version="1.0" encoding="UTF-8" standalone="no"?>\n${body}`;
}

/** Filename for the download; kept out of the pure serializer for tests. */
export function svgFilename(now: Date): string {
  const p = (n: number) => String(n).padStart(2, '0');
  const stamp =
    `${now.getFullYear()}${p(now.getMonth() + 1)}${p(now.getDate())}` +
    `-${p(now.getHours())}${p(now.getMinutes())}${p(now.getSeconds())}`;
  return `dockerview-network-${stamp}.svg`;
}

/** Trigger the browser download of the serialized graph. */
export function downloadGraphSvg(source: SVGSVGElement, filename: string): void {
  const xml = serializeGraphSvg(source);
  const blob = new Blob([xml], { type: 'image/svg+xml;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
