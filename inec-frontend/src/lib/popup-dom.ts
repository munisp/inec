/**
 * popup-dom.ts — safe popup DOM construction for maplibre popups.
 *
 * All server-supplied strings MUST reach the map through textContent /
 * createTextNode — never innerHTML or Popup.setHTML — so API data can never
 * inject markup (XSS). Popup.setDOMContent accepts a node built this way.
 *
 * Used by MapPage.tsx (same pattern as GeoLibreMapPage.tsx).
 */
export function popupEl(tag: string, text: string, cssText?: string): HTMLElement {
  const node = document.createElement(tag);
  node.textContent = text;
  if (cssText) node.style.cssText = cssText;
  return node;
}
