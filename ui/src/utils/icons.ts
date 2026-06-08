/**
 * Self-hosted Iconify icon utilities.
 * Icon sets are served from /icons/{prefix}.json (copied from @iconify/json at build time).
 * Icon names use format `{prefix}:{name}` e.g. `mdi:water-pump`.
 */

export interface IconSetMeta {
  prefix: string;
  label: string;
}

export const ICON_SETS: IconSetMeta[] = [
  { prefix: 'mdi',              label: 'Material Design' },
  { prefix: 'material-symbols', label: 'Material Symbols' },
  { prefix: 'carbon',           label: 'IBM Carbon'       },
  { prefix: 'tabler',           label: 'Tabler'            },
  { prefix: 'ph',               label: 'Phosphor'          },
  { prefix: 'lucide',           label: 'Lucide'            },
];

// ── Internal cache ────────────────────────────────────────────────────────────

interface IconifyJSON {
  prefix: string;
  icons: Record<string, { body: string; width?: number; height?: number }>;
  aliases?: Record<string, { parent: string; hFlip?: boolean; vFlip?: boolean; rotate?: number }>;
  width?: number;
  height?: number;
}

const loadedSets = new Map<string, IconifyJSON>();
const pendingLoads = new Map<string, Promise<void>>();

// ── Public API ─────────────────────────────────────────────────────────────────

/**
 * Fetch and cache an icon set JSON.
 * Deduplicates concurrent fetches via Promise cache.
 */
export function loadIconSet(prefix: string): Promise<void> {
  if (loadedSets.has(prefix)) return Promise.resolve();
  if (pendingLoads.has(prefix)) return pendingLoads.get(prefix)!;

  const p = fetch(`/xact/icons/${prefix}.json`)
    .then(r => {
      if (!r.ok) throw new Error(`Failed to load icon set ${prefix}: ${r.status}`);
      return r.json() as Promise<IconifyJSON>;
    })
    .then(data => {
      loadedSets.set(prefix, data);
      pendingLoads.delete(prefix);
    })
    .catch(err => {
      pendingLoads.delete(prefix);
      console.warn('[icons]', err);
    });

  pendingLoads.set(prefix, p);
  return p;
}

/** Synchronous check - true if the set is already loaded and cached. */
export function isIconSetLoaded(prefix: string): boolean {
  return loadedSets.has(prefix);
}

/** Fire-and-forget background preload. */
export function preloadIconSet(prefix: string): void {
  loadIconSet(prefix).catch(() => {});
}

/**
 * Resolve alias one level deep.
 */
function resolveIconBody(set: IconifyJSON, iconName: string): string | null {
  let icon = set.icons[iconName];
  if (icon) return icon.body;

  // Check aliases
  const alias = set.aliases?.[iconName];
  if (alias) {
    icon = set.icons[alias.parent];
    if (icon) return icon.body;
  }
  return null;
}

/**
 * Return an SVG string for the given icon name (`prefix:name`).
 * Returns empty string if the set is not loaded or icon not found.
 * Falls back gracefully so callers can render a <span> instead.
 */
export function getIconSVG(name: string, color = 'currentColor', size = 24): string {
  if (!name || !name.includes(':')) return '';

  const colonIdx = name.indexOf(':');
  const prefix = name.slice(0, colonIdx);
  const iconName = name.slice(colonIdx + 1);

  const set = loadedSets.get(prefix);
  if (!set) return '';

  const body = resolveIconBody(set, iconName);
  if (!body) return '';

  const w = set.width ?? 24;
  const h = set.height ?? 24;

  const safeColor = escapeAttr(color);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${w} ${h}" fill="${safeColor}" color="${safeColor}" style="color:${safeColor};">${body}</svg>`;
}

export interface IconResult {
  name: string;    // full `prefix:iconName`
  prefix: string;
  iconName: string;
}

/**
 * Synchronous search on cached icon set.
 * Returns up to `limit` matches (default 50).
 */
export function searchIcons(query: string, prefix: string, limit = 50): IconResult[] {
  const set = loadedSets.get(prefix);
  if (!set) return [];

  const q = query.toLowerCase().trim();
  const results: IconResult[] = [];

  const allNames = [
    ...Object.keys(set.icons),
    ...Object.keys(set.aliases ?? {}),
  ];

  for (const iconName of allNames) {
    if (results.length >= limit) break;
    if (!q || iconName.includes(q)) {
      results.push({ name: `${prefix}:${iconName}`, prefix, iconName });
    }
  }

  return results;
}

function escapeAttr(value: string): string {
  return String(value).replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]!));
}
