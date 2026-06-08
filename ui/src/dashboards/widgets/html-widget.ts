import { BaseComponent } from '../../components/base-component';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import '../../components/html-editor';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import { sanitizeHtml } from '../../utils/html-sanitize';

interface Config {
  html: string;
  tagPrefix?: string;
  devicePath?: string;
  deviceName?: string;
  deviceType?: string;
  orgName?: string;
}

const DEFAULT_CONFIG: Config = {
  html: `<div style="padding:12px;">
  <h3>{deviceName}</h3>
  <p>Organisation: {orgName}</p>
  <p>Value: {tag:meta.online}</p>
</div>`,
};

function esc(s: any): string {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function htmlValue(s: any): string {
  return String(s ?? '');
}

export class HtmlWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private subscribedPaths = new Set<string>();
  private registeredPaths = new Set<string>();
  private cfgOverlay: HTMLDivElement | null = null;
  private expanded = false;
  private uiUnsubs: Array<() => void> = [];

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    this.rerender();
  }

  getConfig(): Config {
    return { ...this.config };
  }

  openConfig(): void {
    if (this.cfgOverlay) return;
    this.expanded = false;
    this.renderConfigOverlay();
  }

  protected render(): void {
    this.innerHTML = `
      <style>
        .html-widget-content :where(h1, h2, h3, h4, h5, h6) {
          display: block;
          margin: 0.67em 0;
          font-weight: 700;
          line-height: 1.2;
        }
        .html-widget-content :where(h1) { font-size: 2em; }
        .html-widget-content :where(h2) { font-size: 1.5em; }
        .html-widget-content :where(h3) { font-size: 1.17em; }
        .html-widget-content :where(h4) { font-size: 1em; }
        .html-widget-content :where(h5) { font-size: 0.83em; }
        .html-widget-content :where(h6) { font-size: 0.67em; }
        .html-widget-content :where(p) { margin: 1em 0; }
        .html-widget-content :where(ul, ol) {
          margin: 1em 0;
          padding-left: 2em;
        }
        .html-widget-content :where(ul) { list-style: disc; }
        .html-widget-content :where(ol) { list-style: decimal; }
        .html-widget-content :where(strong, b) { font-weight: 700; }
        .html-widget-content :where(em, i) { font-style: italic; }
      </style>
      <div class="html-widget-content" style="
        height:100%;width:100%;overflow:auto;
        color:var(--content-text);font-family:var(--widget-font-family);
        font-size:var(--widget-label-font-size);line-height:1.45;
      "></div>
    `;
    this.updateContent();
    const card = this.closest('widget-card') as any;
    card?.setHeaderVisible?.(false);
  }

  protected attachEventListeners(): void {
    this.subscribeToReferencedTags();
    const ui = getUiStore();
    this.uiUnsubs = [
      ui.subscribe('orgName', () => this.handleUiVariableChanged()),
      ui.subscribe('deviceType', () => this.handleUiVariableChanged()),
      ui.subscribe('deviceName', () => this.handleUiVariableChanged()),
      ui.subscribe('timeStart', () => this.updateContent()),
      ui.subscribe('timeEnd', () => this.updateContent()),
    ];
  }

  protected detachEventListeners(): void {
    this.subscribedPaths.clear();
    this.uiUnsubs.forEach(u => u());
    this.uiUnsubs = [];
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.closeConfig();
  }

  private handleUiVariableChanged(): void {
    this.subscribeToReferencedTags();
    this.updateContent();
  }

  private updateContent(): void {
    const content = this.querySelector<HTMLElement>('.html-widget-content');
    if (!content) return;
    content.innerHTML = sanitizeHtml(this.resolveHtml());
  }

  private resolveHtml(): string {
    const ui = getUiStore();
    const vars: Record<string, any> = {
      orgName: ui.get('orgName') || getMirrorStore().getOrg() || '',
      deviceType: this.config.deviceType ?? ui.get('deviceType') ?? '',
      deviceName: this.config.deviceName ?? this.config.devicePath?.split('.').pop() ?? ui.get('deviceName') ?? '',
      timeStart: ui.get('timeStart') ?? '',
      timeEnd: ui.get('timeEnd') ?? '',
    };
    if (this.config.orgName !== undefined) vars.orgName = this.config.orgName;

    let html = this.config.html || '';
    html = html.replace(/\{\{\s*(orgName|deviceType|deviceName|timeStart|timeEnd)\s*\}\}/g, (_m, key) => esc(vars[key]));
    html = html.replace(/\{\s*(orgName|deviceType|deviceName|timeStart|timeEnd)\s*\}/g, (_m, key) => esc(vars[key]));
    html = html.replace(/\{\{\s*tag:([^}]+)\s*\}\}/g, (_m, path) => htmlValue(this.readTag(path)));
    html = html.replace(/\{\s*tag:([^}]+)\s*\}/g, (_m, path) => htmlValue(this.readTag(path)));
    html = html.replace(/\$\{\s*tag\(\s*['"`]([^'"`]+)['"`]\s*\)\s*\}/g, (_m, path) => htmlValue(this.readTag(path)));
    return html;
  }

  private readTag(path: string): any {
    const resolved = this.resolveTagPath(path);
    if (!resolved) return '';
    return getMirrorStore().resolveTagReference(resolved) ?? '';
  }

  private resolveTagPath(path: string): string {
    const trimmed = String(path ?? '').trim();
    if (!trimmed) return '';
    const store = getMirrorStore();
    const deviceName = this.config.deviceName ?? this.config.devicePath?.split('.').pop() ?? getUiStore().get('deviceName') ?? '';
    const prefix = String(this.config.tagPrefix || this.config.devicePath || '').trim();
    let resolved = trimmed.replace(/\*/g, deviceName);
    const org = store.getOrg();
    const isAbsolute = !!org && (resolved === org || resolved.startsWith(org + '.'));
    if (prefix && !isAbsolute && resolved !== prefix && !resolved.startsWith(prefix + '.')) {
      resolved = `${prefix}.${resolved}`;
    }
    return store.toAbsolute(resolved);
  }

  private referencedTagPaths(): Set<string> {
    const paths = new Set<string>();
    const html = this.config.html || '';
    const patterns = [
      /\{\{\s*tag:([^}]+)\s*\}\}/g,
      /\{\s*tag:([^}]+)\s*\}/g,
      /\$\{\s*tag\(\s*['"`]([^'"`]+)['"`]\s*\)\s*\}/g,
    ];
    for (const re of patterns) {
      let m: RegExpExecArray | null;
      while ((m = re.exec(html)) !== null) {
        const resolved = this.resolveTagPath(m[1]);
        if (resolved) paths.add(resolved);
      }
    }
    return paths;
  }

  private subscribeToReferencedTags(): void {
    const nextPaths = this.referencedTagPaths();
    this.subscribedPaths = nextPaths;
    const store = getMirrorStore();
    for (const path of nextPaths) {
      if (this.registeredPaths.has(path)) continue;
      this.registeredPaths.add(path);
      store.subscribeTagReference(path, () => {
        if (!this.isConnected || !this.subscribedPaths.has(path)) return;
        this.updateContent();
      });
    }
  }

  private renderConfigOverlay(): void {
    this.closeConfig();
    const overlay = document.createElement('div');
    this.cfgOverlay = overlay;
    document.body.appendChild(overlay);
    this.paintConfigOverlay();
  }

  private paintConfigOverlay(): void {
    const overlay = this.cfgOverlay;
    if (!overlay) return;
    const size = this.expanded
      ? 'width:min(1180px,96vw);height:min(860px,94vh);'
      : 'width:min(760px,94vw);height:min(560px,88vh);';
    overlay.innerHTML = `
      <div style="position:fixed;inset:0;z-index:20000;background:rgba(0,0,0,0.62);display:flex;align-items:center;justify-content:center;padding:1rem;">
        <div style="${size}display:flex;flex-direction:column;background:var(--content-bg);color:var(--content-text);border:1px solid var(--border-color);border-radius:8px;box-shadow:0 24px 60px rgba(0,0,0,0.55);overflow:hidden;">
          <div style="display:flex;align-items:center;gap:8px;padding:12px 14px;border-bottom:1px solid var(--border-color);">
            <div style="font-size:14px;font-weight:600;color:var(--accent-color);flex:1;">HTML Widget</div>
            <button id="hw-insert-tag" type="button" style="${this.buttonStyle()}">Insert tag</button>
            <button id="hw-expand" type="button" style="${this.buttonStyle()}">${this.expanded ? 'Restore' : 'Expand'}</button>
            <button id="hw-close" type="button" style="background:none;border:none;color:var(--content-text);font-size:22px;line-height:1;cursor:pointer;opacity:0.7;">&times;</button>
          </div>
          <div style="padding:10px 14px;border-bottom:1px solid var(--border-color);font-size:12px;opacity:0.72;">
            Supports {orgName}, {deviceType}, {deviceName}, {timeStart}, {timeEnd}, and tag values like {tag:device.meta.online} or {tag:meta.online}.
          </div>
          <div style="flex:1;min-height:0;padding:14px;display:flex;flex-direction:column;">
            <html-editor id="hw-editor" style="flex:1;min-height:0;"></html-editor>
          </div>
          <div style="display:flex;gap:8px;justify-content:flex-end;padding:12px 14px;border-top:1px solid var(--border-color);">
            <button id="hw-cancel" type="button" style="${this.buttonStyle()}">Cancel</button>
            <button id="hw-save" type="button" style="${this.buttonStyle(true)}">Apply</button>
          </div>
        </div>
      </div>
    `;

    const editor = overlay.querySelector<any>('#hw-editor');
    editor?.setValue(this.config.html || '');
    editor?.refresh?.();
    overlay.querySelector('#hw-close')?.addEventListener('click', () => this.closeConfig());
    overlay.querySelector('#hw-cancel')?.addEventListener('click', () => this.closeConfig());
    overlay.querySelector('#hw-expand')?.addEventListener('click', () => {
      this.config.html = editor?.getValue?.() ?? this.config.html;
      this.expanded = !this.expanded;
      this.paintConfigOverlay();
    });
    overlay.querySelector('#hw-save')?.addEventListener('click', () => {
      this.config.html = editor?.getValue?.() ?? '';
      this.emit('widget-config-save', { config: this.getConfig() });
      this.closeConfig();
      this.rerender();
    });
    overlay.querySelector('#hw-insert-tag')?.addEventListener('click', () => {
      getTreeBrowserDialog().open('', 'Select Tag', (path) => {
        overlay.querySelector<any>('#hw-editor')?.insertText(`{tag:${path}}`);
      }, true);
    });
  }

  private buttonStyle(primary = false): string {
    return primary
      ? 'border:1px solid var(--accent-color);background:var(--accent-color);color:var(--accent-text);border-radius:4px;padding:5px 12px;font-size:12px;cursor:pointer;'
      : 'border:1px solid var(--border-color);background:color-mix(in srgb,var(--border-color) 24%,transparent);color:var(--content-text);border-radius:4px;padding:5px 12px;font-size:12px;cursor:pointer;';
  }

  private closeConfig(): void {
    this.cfgOverlay?.remove();
    this.cfgOverlay = null;
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }
}

registerWidgetType({
  type: 'html-widget',
  name: 'HTML',
  icon: '🌐',
  category: 'General',
  defaultW: 8,
  defaultH: 4,
  minW: 2,
  minH: 1,
});

customElements.define('html-widget', HtmlWidget);
