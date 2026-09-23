import DOMPurify from 'dompurify';

export interface SanitizeHtmlOptions {
  allowedTags?: Set<string>;
  forbiddenTags?: Set<string>;
}

export function escapeHtml(value: unknown): string {
  return String(value ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]!));
}

export function escapeSelector(value: string): string {
  // Hex escapes work for both identifiers and quoted attribute selectors.
  return Array.from(value, char => `\\${char.codePointAt(0)!.toString(16)} `).join('');
}

// Rich content is presentation only. Custom elements must never be upgraded
// with attacker-supplied config after sanitization.
const forbiddenTags = [
  'style', 'script', 'iframe', 'object', 'embed', 'link', 'meta', 'base', 'form',
  'input', 'button', 'select', 'textarea', 'option', 'svg', 'math', 'template',
];

export function sanitizeHtml(html: string, options: SanitizeHtmlOptions = {}): string {
  const fragment = DOMPurify.sanitize(String(html ?? ''), {
    RETURN_DOM_FRAGMENT: true,
    ...(options.allowedTags ? { ALLOWED_TAGS: [...options.allowedTags] } : { USE_PROFILES: { html: true } }),
    FORBID_TAGS: [...forbiddenTags, ...(options.forbiddenTags ?? [])],
    FORBID_ATTR: ['srcdoc', 'is', 'config'],
    ALLOW_DATA_ATTR: false,
    SANITIZE_NAMED_PROPS: true,
  });
  for (const el of fragment.querySelectorAll<HTMLElement>('[style]')) {
    // CSSOM normalizes escaped function names before checking URL-bearing CSS.
    for (const prop of Array.from(el.style)) {
      if (prop.startsWith('--') || /url\s*\(|expression\s*\(|javascript:|vbscript:/i.test(el.style.getPropertyValue(prop))) {
        el.style.removeProperty(prop);
      }
    }
  }
  const template = document.createElement('template');
  template.content.append(fragment);
  return template.innerHTML;
}
