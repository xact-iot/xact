/**
 * manual-widget - Displays the XACT user manual with a table-of-contents
 * sidebar, markdown rendering (via `marked`), and full-text search.
 *
 * Chapters are stored as individual .md files under /manual/ and listed
 * in /manual/manifest.json. Content is fetched on demand, parsed at
 * runtime, and styled using theme CSS variables.
 */

import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { marked } from 'marked';

// ── Types ────────────────────────────────────────────────────────────────────

interface ChapterMeta {
  id: string;
  file: string;
  title: string;
}

interface Manifest {
  title: string;
  chapters: ChapterMeta[];
}

interface SubHeading {
  text: string;
  slug: string;
}

interface Config {
  chapter: string; // last-viewed chapter id
}

const DEFAULT_CONFIG: Config = { chapter: '' };

const MANUAL_BASE = '/xact/manual';

// ── Styles (injected once) ───────────────────────────────────────────────────

function ensureStyles(): void {
  if (document.getElementById('manual-widget-styles')) return;
  const s = document.createElement('style');
  s.id = 'manual-widget-styles';
  s.textContent = `
    /* ── Layout ─────────────────────────────────────────────── */
    .mw-root {
      display: flex;
      height: 100%;
      width: 100%;
      overflow: hidden;
      font-family: var(--widget-font-family);
      color: var(--content-text);
      background: var(--widget-bg);
    }

    /* ── Sidebar ────────────────────────────────────────────── */
    .mw-sidebar {
      width: 240px;
      min-width: 240px;
      background: var(--widget-header-bg);
      border-right: 1px solid var(--widget-border);
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .mw-sidebar-header {
      padding: 14px 14px 0 14px;
      flex-shrink: 0;
    }
    .mw-sidebar-title {
      font-size: 0.7rem;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: var(--accent-color);
      margin-bottom: 12px;
    }
    .mw-search {
      width: 100%;
      padding: 7px 10px;
      font-size: 0.7rem;
      font-family: var(--widget-font-family);
      color: var(--content-text);
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      outline: none;
      transition: border-color 0.15s;
    }
    .mw-search:focus {
      border-color: var(--accent-color);
    }
    .mw-search::placeholder {
      color: color-mix(in srgb, var(--content-text) 40%, transparent);
    }
    .mw-toc {
      flex: 1;
      overflow-y: auto;
      padding: 10px 0;
    }
    .mw-toc-item {
      display: flex;
      align-items: center;
      padding: 8px 14px;
      font-size: 0.7rem;
      cursor: pointer;
      color: var(--content-text);
      border-left: 3px solid transparent;
      transition: background 0.12s, border-color 0.12s, color 0.12s;
      user-select: none;
    }
    .mw-toc-item:hover {
      background: color-mix(in srgb, var(--accent-color) 8%, var(--widget-header-bg));
    }
    .mw-toc-item.active {
      border-left-color: var(--accent-color);
      color: var(--accent-color);
      background: color-mix(in srgb, var(--accent-color) 12%, var(--widget-header-bg));
      font-weight: 600;
    }
    .mw-toc-chevron {
      display: inline-block;
      width: 12px;
      font-size: 0.55rem;
      flex-shrink: 0;
      transition: transform 0.15s;
      color: color-mix(in srgb, var(--content-text) 50%, transparent);
    }
    .mw-toc-chevron.expanded {
      transform: rotate(90deg);
    }
    .mw-toc-sub {
      display: flex;
      align-items: center;
      padding: 5px 14px 5px 42px;
      font-size: 0.65rem;
      cursor: pointer;
      color: color-mix(in srgb, var(--content-text) 70%, transparent);
      border-left: 3px solid transparent;
      transition: background 0.12s, color 0.12s;
      user-select: none;
    }
    .mw-toc-sub:hover {
      color: var(--content-text);
      background: color-mix(in srgb, var(--accent-color) 6%, var(--widget-header-bg));
    }
    .mw-toc-sub.active {
      color: var(--accent-color);
      border-left-color: color-mix(in srgb, var(--accent-color) 50%, transparent);
    }
    .mw-toc-badge {
      margin-left: auto;
      font-size: 0.6rem;
      padding: 1px 6px;
      border-radius: 8px;
      background: color-mix(in srgb, var(--accent-color) 20%, transparent);
      color: var(--accent-color);
      font-weight: 600;
    }

    /* ── Content ────────────────────────────────────────────── */
    .mw-content {
      flex: 1;
      overflow-y: auto;
      padding: 32px 48px 48px 48px;
      min-width: 0;
    }
    .mw-loading {
      color: color-mix(in srgb, var(--content-text) 50%, transparent);
      font-style: italic;
      font-size: 0.8rem;
      padding-top: 24px;
    }

    /* ── Rendered markdown ──────────────────────────────────── */
    .mw-md h1 {
      font-size: 1.4rem;
      font-weight: 700;
      color: var(--widget-header-text);
      margin: 0 0 20px 0;
      padding-bottom: 10px;
      border-bottom: 1px solid var(--widget-border);
    }
    .mw-md h2 {
      font-size: 1.05rem;
      font-weight: 600;
      color: var(--accent-color);
      margin: 28px 0 12px 0;
    }
    .mw-md h3 {
      font-size: 0.9rem;
      font-weight: 600;
      color: var(--widget-header-text);
      margin: 22px 0 8px 0;
    }
    .mw-md p {
      font-size: 0.75rem;
      line-height: 1.75;
      margin: 0 0 14px 0;
      color: var(--content-text);
    }
    .mw-md a {
      color: var(--accent-color);
      text-decoration: underline;
      text-underline-offset: 2px;
    }
    .mw-md a:hover {
      color: var(--accent-hover, var(--accent-color));
    }
    .mw-md strong {
      font-weight: 700;
      color: var(--widget-header-text);
    }
    .mw-md em {
      font-style: italic;
    }
    .mw-md ul, .mw-md ol {
      font-size: 0.75rem;
      line-height: 1.75;
      margin: 0 0 14px 0;
      padding-left: 24px;
      color: var(--content-text);
    }
    .mw-md li {
      margin-bottom: 4px;
    }
    .mw-md li > ul, .mw-md li > ol {
      margin: 4px 0 0 0;
    }
    .mw-md code {
      font-family: 'SF Mono', 'Cascadia Code', 'Fira Code', Consolas, monospace;
      font-size: 0.68rem;
      background: color-mix(in srgb, var(--accent-color) 8%, var(--widget-bg));
      padding: 2px 6px;
      border-radius: 3px;
      color: var(--widget-header-text);
    }
    .mw-md pre {
      background: var(--widget-header-bg);
      border: 1px solid var(--widget-border);
      border-radius: 6px;
      padding: 14px 18px;
      margin: 0 0 16px 0;
      overflow-x: auto;
    }
    .mw-md pre code {
      background: none;
      padding: 0;
      font-size: 0.68rem;
      line-height: 1.6;
    }
    .mw-md blockquote {
      border-left: 3px solid var(--accent-color);
      margin: 0 0 16px 0;
      padding: 10px 16px;
      background: color-mix(in srgb, var(--accent-color) 5%, var(--widget-bg));
      border-radius: 0 6px 6px 0;
    }
    .mw-md blockquote p {
      margin: 0;
      font-size: 0.72rem;
      color: color-mix(in srgb, var(--content-text) 85%, var(--accent-color));
    }
    .mw-md table {
      width: 100%;
      border-collapse: collapse;
      margin: 0 0 16px 0;
      font-size: 0.7rem;
    }
    .mw-md th {
      text-align: left;
      padding: 8px 12px;
      background: var(--widget-header-bg);
      color: var(--widget-header-text);
      font-weight: 600;
      border: 1px solid var(--widget-border);
    }
    .mw-md td {
      padding: 8px 12px;
      border: 1px solid var(--widget-border);
      color: var(--content-text);
    }
    .mw-md tr:hover td {
      background: color-mix(in srgb, var(--accent-color) 4%, var(--widget-bg));
    }
    .mw-md img {
      max-width: 100%;
      border-radius: 6px;
      border: 1px solid var(--widget-border);
      margin: 8px 0 16px 0;
      box-shadow: 0 2px 12px rgba(0,0,0,0.2);
    }
    .mw-md hr {
      border: none;
      border-top: 1px solid var(--widget-border);
      margin: 24px 0;
    }

    /* ── Search highlight ───────────────────────────────────── */
    mark.mw-hit {
      background: color-mix(in srgb, var(--accent-color) 30%, transparent);
      color: var(--widget-header-text);
      padding: 1px 2px;
      border-radius: 2px;
    }

    /* ── Scrollbar ──────────────────────────────────────────── */
    .mw-toc::-webkit-scrollbar,
    .mw-content::-webkit-scrollbar {
      width: 6px;
    }
    .mw-toc::-webkit-scrollbar-track,
    .mw-content::-webkit-scrollbar-track {
      background: transparent;
    }
    .mw-toc::-webkit-scrollbar-thumb,
    .mw-content::-webkit-scrollbar-thumb {
      background: color-mix(in srgb, var(--content-text) 20%, transparent);
      border-radius: 3px;
    }
    .mw-toc::-webkit-scrollbar-thumb:hover,
    .mw-content::-webkit-scrollbar-thumb:hover {
      background: color-mix(in srgb, var(--content-text) 35%, transparent);
    }
  `;
  document.head.appendChild(s);
}

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Strip markdown syntax for plain-text search. */
function stripMarkdown(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, ' ')      // fenced code blocks
    .replace(/`[^`]+`/g, ' ')             // inline code
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '') // images
    .replace(/\[[^\]]*\]\([^)]*\)/g, '$&'.replace(/\[([^\]]*)\].*/, '$1')) // links → text
    .replace(/#{1,6}\s+/g, '')            // headings
    .replace(/[*_~|>-]+/g, ' ')           // emphasis, blockquote, hr
    .replace(/\s+/g, ' ')
    .trim();
}

/** Escape string for use in RegExp. */
function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Generate a URL-friendly slug from heading text. */
function slugify(text: string): string {
  return text.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-').replace(/-+/g, '-').trim();
}

/** Extract ## headings from raw markdown. */
function extractSubHeadings(md: string): SubHeading[] {
  const results: SubHeading[] = [];
  for (const m of md.matchAll(/^##\s+(.+)$/gm)) {
    const text = m[1].replace(/[*_`#]+/g, '').trim();
    results.push({ text, slug: slugify(text) });
  }
  return results;
}

// ── Configure marked ─────────────────────────────────────────────────────────

const renderer = new marked.Renderer();

// Add id attributes to h2 headings for scroll-to-heading support.
const origHeading = renderer.heading.bind(renderer);
renderer.heading = function (token) {
  if (token.depth === 2) {
    const slug = slugify(token.text);
    return `<h2 id="${slug}">${token.text}</h2>`;
  }
  return origHeading(token);
};

// Rewrite image src to be relative to the manual base path.
const origImage = renderer.image.bind(renderer);
renderer.image = function (token) {
  // If href is relative (no protocol), prepend the manual base.
  if (token.href && !token.href.match(/^https?:\/\//)) {
    token.href = `${MANUAL_BASE}/${token.href}`;
  }
  return origImage(token);
};

// Intercept links to other chapters (href="#chapter-id") so they navigate
// within the widget rather than changing the page URL.
renderer.link = function (token) {
  const href = token.href || '';
  if (href.startsWith('#')) {
    return `<a href="${href}" class="mw-chapter-link" data-chapter="${href.slice(1)}">${token.text}</a>`;
  }
  return `<a href="${href}" target="_blank" rel="noopener">${token.text}</a>`;
};

marked.setOptions({ renderer, gfm: true, breaks: false });

// ── Widget ───────────────────────────────────────────────────────────────────

export class ManualWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private manifest: Manifest | null = null;
  private activeChapterId = '';
  private chapterCache = new Map<string, string>();   // id → raw markdown
  private chapterHeadings = new Map<string, SubHeading[]>(); // id → h2 list
  private expandedChapters = new Set<string>();
  private activeSubSlug = '';  // currently highlighted sub-heading slug
  private searchQuery = '';
  private searchResults: Map<string, number> | null = null; // id → match count
  private _handlers: Array<[EventTarget, string, EventListener]> = [];
  private manifestPromise: Promise<void> | null = null;
  private chapterPromises = new Map<string, Promise<string>>();

  // ── Public API ─────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    if (this.isConnected) this.rerender();
  }

  // ── Lifecycle ──────────────────────────────────────────────────────────

  protected render(): void {
    ensureStyles();
    this.innerHTML = `
      <div class="mw-root">
        <div class="mw-sidebar">
          <div class="mw-sidebar-header">
            <div class="mw-sidebar-title">User Manual</div>
            <input class="mw-search" type="text" placeholder="Search manual\u2026" />
          </div>
          <div class="mw-toc"></div>
        </div>
        <div class="mw-content">
          <div class="mw-loading">Loading manual\u2026</div>
        </div>
      </div>
    `;

    // Hide widget-card header for full-bleed layout.
    const card = this.closest('widget-card') as any;
    card?.setHeaderVisible?.(false);

    this.ensureManifestLoaded();
  }

  protected attachEventListeners(): void {
    const searchInput = this.querySelector('.mw-search') as HTMLInputElement | null;
    if (searchInput) {
      const onSearch = () => this.handleSearch(searchInput.value);
      searchInput.addEventListener('input', onSearch);
      this._handlers.push([searchInput, 'input', onSearch]);
    }

    const toc = this.querySelector('.mw-toc');
    if (toc) {
      const onTocClick = (e: Event) => {
        // Sub-heading click - navigate to chapter + scroll to heading.
        const sub = (e.target as HTMLElement).closest('.mw-toc-sub') as HTMLElement | null;
        if (sub?.dataset.chapter && sub.dataset.slug) {
          this.navigateTo(sub.dataset.chapter, sub.dataset.slug);
          return;
        }
        // Chapter click - toggle expand and navigate.
        const item = (e.target as HTMLElement).closest('.mw-toc-item') as HTMLElement | null;
        if (item?.dataset.id) {
          this.toggleChapter(item.dataset.id);
        }
      };
      toc.addEventListener('click', onTocClick);
      this._handlers.push([toc, 'click', onTocClick]);
    }

    const content = this.querySelector('.mw-content');
    if (content) {
      const onContentClick = (e: Event) => {
        const link = (e.target as HTMLElement).closest('.mw-chapter-link') as HTMLElement | null;
        if (link) {
          e.preventDefault();
          const chapterId = link.dataset.chapter;
          if (chapterId) this.navigateTo(chapterId);
        }
      };
      content.addEventListener('click', onContentClick);
      this._handlers.push([content, 'click', onContentClick]);
    }
  }

  protected detachEventListeners(): void {
    for (const [el, evt, fn] of this._handlers) el.removeEventListener(evt, fn);
    this._handlers = [];
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Data loading ───────────────────────────────────────────────────────

  private ensureManifestLoaded(): Promise<void> {
    if (this.manifest) {
      this.renderLoadedManual();
      return Promise.resolve();
    }
    if (!this.manifestPromise) {
      this.manifestPromise = this.loadManifest();
    }
    return this.manifestPromise;
  }

  private renderLoadedManual(): void {
    if (!this.manifest) return;
    this.renderToc();

    const current = this.activeChapterId
      || (this.config.chapter && this.manifest.chapters.some(c => c.id === this.config.chapter)
        ? this.config.chapter
        : this.manifest.chapters[0]?.id || '');
    if (current) this.navigateTo(current, this.activeSubSlug || undefined);
  }

  private async loadManifest(): Promise<void> {
    try {
      const resp = await fetch(`${MANUAL_BASE}/manifest.json`);
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      this.manifest = await resp.json() as Manifest;
      this.renderToc();

      // Navigate to saved chapter or first chapter.
      const initial = this.config.chapter
        && this.manifest.chapters.some(c => c.id === this.config.chapter)
        ? this.config.chapter
        : this.manifest.chapters[0]?.id || '';
      if (initial) await this.navigateTo(initial);
    } catch (err) {
      const content = this.querySelector('.mw-content');
      if (content) content.innerHTML = `<div class="mw-loading">Failed to load manual index.</div>`;
    }
  }

  private async fetchChapter(id: string): Promise<string> {
    if (this.chapterCache.has(id)) return this.chapterCache.get(id)!;
    const existing = this.chapterPromises.get(id);
    if (existing) return existing;

    const request = (async () => {
      const chapter = this.manifest?.chapters.find(c => c.id === id);
      if (!chapter) throw new Error(`Unknown chapter: ${id}`);

      const resp = await fetch(`${MANUAL_BASE}/${chapter.file}`);
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const md = await resp.text();
      this.chapterCache.set(id, md);
      this.chapterHeadings.set(id, extractSubHeadings(md));
      return md;
    })();
    this.chapterPromises.set(id, request);
    request.then(
      () => this.chapterPromises.delete(id),
      () => this.chapterPromises.delete(id),
    );
    return request;
  }

  // ── Navigation ─────────────────────────────────────────────────────────

  /** Toggle expand/collapse on a chapter and navigate to it. */
  private async toggleChapter(id: string): Promise<void> {
    // If already the active chapter, just toggle expand.
    if (id === this.activeChapterId) {
      if (this.expandedChapters.has(id)) this.expandedChapters.delete(id);
      else this.expandedChapters.add(id);
      this.renderToc();
      return;
    }
    // Fetch the chapter first so headings are available before rendering TOC.
    await this.fetchChapter(id).catch(() => {});
    this.expandedChapters.add(id);
    this.navigateTo(id);
  }

  private async navigateTo(id: string, slug?: string): Promise<void> {
    const chapterChanged = id !== this.activeChapterId;
    this.activeChapterId = id;
    this.activeSubSlug = slug || '';
    this.config.chapter = id;

    // Ensure this chapter is expanded and update TOC.
    this.expandedChapters.add(id);
    this.renderToc();

    const content = this.querySelector('.mw-content');
    if (!content) return;

    // Only re-render content if the chapter changed.
    if (chapterChanged || !content.querySelector('.mw-md')) {
      content.innerHTML = `<div class="mw-loading">Loading\u2026</div>`;

      try {
        const md = await this.fetchChapter(id);
        let html = await marked.parse(md);

        if (this.searchQuery) {
          html = this.highlightMatches(html, this.searchQuery);
        }

        content.innerHTML = `<div class="mw-md">${html}</div>`;
      } catch {
        content.innerHTML = `<div class="mw-loading">Failed to load chapter.</div>`;
        return;
      }
    }

    // Scroll to the target.
    if (slug) {
      const heading = content.querySelector(`#${CSS.escape(slug)}`);
      if (heading) heading.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } else if (this.searchQuery) {
      const firstHit = content.querySelector('mark.mw-hit');
      if (firstHit) firstHit.scrollIntoView({ behavior: 'smooth', block: 'center' });
    } else if (chapterChanged) {
      content.scrollTop = 0;
    }

    this.emit('widget-config-save', { config: { chapter: id }, silent: true });
  }

  // ── TOC rendering ──────────────────────────────────────────────────────

  private renderToc(): void {
    const toc = this.querySelector('.mw-toc');
    if (!toc || !this.manifest) return;

    const chapters = this.manifest.chapters;
    toc.innerHTML = chapters
      .map(ch => {
        const isActive = ch.id === this.activeChapterId;
        const isExpanded = this.expandedChapters.has(ch.id);
        const matchCount = this.searchResults?.get(ch.id);
        const hidden = this.searchResults && !this.searchResults.has(ch.id);
        const headings = this.chapterHeadings.get(ch.id) || [];
        const hasChildren = headings.length > 0;
        const badge = matchCount != null
          ? `<span class="mw-toc-badge">${matchCount}</span>`
          : '';
        const chevron = hasChildren
          ? `<span class="mw-toc-chevron${isExpanded ? ' expanded' : ''}">&#9654;</span>`
          : `<span class="mw-toc-chevron"></span>`;

        let html = `<div class="mw-toc-item${isActive ? ' active' : ''}" data-id="${ch.id}"
                         style="${hidden ? 'display:none' : ''}">${chevron}${ch.title}${badge}</div>`;

        // Sub-headings (visible only when expanded).
        if (isExpanded && hasChildren) {
          html += headings.map(h =>
            `<div class="mw-toc-sub${isActive && this.activeSubSlug === h.slug ? ' active' : ''}"
                  data-chapter="${ch.id}" data-slug="${h.slug}"
                  style="${hidden ? 'display:none' : ''}">${h.text}</div>`
          ).join('');
        }

        return html;
      })
      .join('');
  }

  // ── Search ─────────────────────────────────────────────────────────────

  private searchTimeout: ReturnType<typeof setTimeout> | null = null;

  private handleSearch(query: string): void {
    if (this.searchTimeout) clearTimeout(this.searchTimeout);
    this.searchTimeout = setTimeout(() => this.executeSearch(query.trim()), 200);
  }

  private async executeSearch(query: string): Promise<void> {
    this.searchQuery = query;

    if (!query) {
      this.searchResults = null;
      this.renderToc();
      // Re-render current chapter to remove highlights.
      if (this.activeChapterId) this.navigateTo(this.activeChapterId);
      return;
    }

    if (!this.manifest) return;

    // Fetch all chapters that aren't cached yet.
    await Promise.all(
      this.manifest.chapters.map(ch => this.fetchChapter(ch.id).catch(() => ''))
    );

    // Search across all chapters.
    const results = new Map<string, number>();
    const lowerQuery = query.toLowerCase();

    for (const ch of this.manifest.chapters) {
      const md = this.chapterCache.get(ch.id) || '';
      const plain = stripMarkdown(md).toLowerCase();
      let count = 0;
      let idx = 0;
      while ((idx = plain.indexOf(lowerQuery, idx)) !== -1) {
        count++;
        idx += lowerQuery.length;
      }
      if (count > 0) results.set(ch.id, count);
    }

    this.searchResults = results;
    this.renderToc();

    // Navigate to first result if current chapter has no matches.
    if (results.size > 0 && !results.has(this.activeChapterId)) {
      const firstMatch = results.keys().next().value as string;
      this.navigateTo(firstMatch);
    } else if (results.has(this.activeChapterId)) {
      // Re-render current chapter with highlights.
      this.navigateTo(this.activeChapterId);
    }
  }

  /** Highlight search terms in rendered HTML without breaking tags. */
  private highlightMatches(html: string, query: string): string {
    const escaped = escapeRegex(query);
    const regex = new RegExp(`(?<=>)([^<]*?)(${escaped})`, 'gi');

    return html.replace(regex, (_match, before: string, term: string) => {
      return `${before}<mark class="mw-hit">${term}</mark>`;
    });
  }
}

// ── Registration ─────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'manual-widget',
  name: 'Help Manual',
  icon: '\u{1F4D6}',
  category: 'System',
  defaultW: 24,
  defaultH: 28,
  minW: 12,
  minH: 8,
});

customElements.define('manual-widget', ManualWidget);
