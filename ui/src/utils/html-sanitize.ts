const DEFAULT_FORBIDDEN_TAGS = new Set([
  'script', 'object', 'embed', 'link', 'meta', 'base', 'form',
  'input', 'button', 'select', 'textarea', 'option', 'svg', 'math',
  'header', 'footer', 'nav', 'main', 'section', 'article', 'aside',
  'details', 'dialog', 'summary', 'template', 'slot', 'canvas',
]);

const URL_ATTRS = new Set(['href', 'src', 'xlink:href', 'formaction', 'poster']);
const SAFE_URL_RE = /^(https?:|mailto:|tel:|\/|#|\.\/|\.\.\/)/i;

export interface SanitizeHtmlOptions {
  allowedTags?: Set<string>;
  forbiddenTags?: Set<string>;
}

function sanitizeStyle(value: string): string {
  const lower = value.toLowerCase();
  if (lower.includes('expression(') || lower.includes('javascript:') || lower.includes('vbscript:')) return '';
  if (lower.includes('url(')) return '';
  return value;
}

export function sanitizeHtml(html: string, options: SanitizeHtmlOptions = {}): string {
  const template = document.createElement('template');
  template.innerHTML = html;
  const forbiddenTags = options.forbiddenTags ?? DEFAULT_FORBIDDEN_TAGS;

  const walk = (node: Node): void => {
    if (node.nodeType === Node.ELEMENT_NODE) {
      const el = node as HTMLElement;
      const tag = el.tagName.toLowerCase();
      if (forbiddenTags.has(tag)) {
        el.remove();
        return;
      }
      if (options.allowedTags && !options.allowedTags.has(tag)) {
        for (const child of Array.from(node.childNodes)) walk(child);
        el.replaceWith(...Array.from(el.childNodes));
        return;
      }

      for (const attr of Array.from(el.attributes)) {
        const name = attr.name.toLowerCase();
        const value = attr.value.trim();
        if (name.startsWith('on')) {
          el.removeAttribute(attr.name);
          continue;
        }
        if (name === 'style') {
          const clean = sanitizeStyle(value);
          if (clean) el.setAttribute(attr.name, clean);
          else el.removeAttribute(attr.name);
          continue;
        }
        if (URL_ATTRS.has(name)) {
          if (!SAFE_URL_RE.test(value)) el.removeAttribute(attr.name);
          continue;
        }
        if (name === 'srcdoc') {
          el.removeAttribute(attr.name);
        }
      }
    }

    for (const child of Array.from(node.childNodes)) walk(child);
  };

  walk(template.content);
  return template.innerHTML;
}
