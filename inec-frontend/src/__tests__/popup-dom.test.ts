import { describe, it, expect } from 'vitest';
import { popupEl } from '../lib/popup-dom';

describe('popupEl (map popup sanitizer)', () => {
  it('renders server strings as text, never markup', () => {
    const xss = '<img src=x onerror="window.__pwned=1"><script>window.__pwned=2</script>';
    const node = popupEl('div', xss);
    expect(node.textContent).toBe(xss);
    expect(node.querySelector('img')).toBeNull();
    expect(node.querySelector('script')).toBeNull();
    expect((window as unknown as Record<string, unknown>).__pwned).toBeUndefined();
  });

  it('applies cssText via the style attribute only', () => {
    const node = popupEl('span', 'ok', 'font-weight:600;color:#16a34a');
    expect(node.tagName).toBe('SPAN');
    expect(node.style.fontWeight).toBe('600');
    expect(node.textContent).toBe('ok');
  });

  it('nests safely when composed into a popup tree', () => {
    const popup = popupEl('div', '');
    const row = popupEl('div', 'Role: ');
    row.appendChild(popupEl('b', '<b>admin</b>'));
    popup.appendChild(row);
    // The injected "<b>" is literal text — exactly one real <b> element exists.
    expect(popup.querySelectorAll('b')).toHaveLength(1);
    expect(popup.querySelector('b')!.textContent).toBe('<b>admin</b>');
  });
});
