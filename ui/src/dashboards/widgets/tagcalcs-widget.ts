import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import {
  listTagCalcs, createTagCalc, updateTagCalc, deleteTagCalc, testTagCalc,
} from '../../api';
import type { TagCalc } from '../../api';
import { showConfirm } from '../../components/app-dialog';

registerPermissions('tagcalcs', 'Tag Calcs', [
  { name: 'view', description: 'View tag calcs' },
  { name: 'manage', description: 'Create, edit, and delete tag calcs' },
], 'Controls access to Tag Calcs - roles with view can inspect computed tag expressions; roles with manage can create, edit, and delete them.');

registerWidgetType({
  type: 'tagcalcs-widget',
  name: 'Tag Calcs',
  icon: '⨍',
  category: 'System',
  defaultW: 12,
  defaultH: 24,
  minW: 8,
  minH: 10,
});

// ── CodeMirror loader (same pattern as html-editor) ─────────────────────────

let cmReady: Promise<void> | null = null;

function loadCodeMirror(): Promise<void> {
  if (cmReady) return cmReady;
  cmReady = new Promise((resolve, reject) => {
    if ((window as any).CodeMirror) { resolve(); return; }

    const css = (href: string) => {
      if (document.querySelector(`link[href="${href}"]`)) return;
      const el = document.createElement('link');
      el.rel = 'stylesheet'; el.href = href;
      document.head.appendChild(el);
    };

    css('https://unpkg.com/codemirror@5.65.16/lib/codemirror.css');

    const script = (src: string) =>
      new Promise<void>((res, rej) => {
        if (document.querySelector(`script[src="${src}"]`)) { res(); return; }
        const el = document.createElement('script');
        el.src = src;
        el.onload = () => res();
        el.onerror = () => rej(new Error(`Failed to load ${src}`));
        document.head.appendChild(el);
      });

    script('https://unpkg.com/codemirror@5.65.16/lib/codemirror.js')
      .then(() => resolve())
      .catch(reject);
  });
  return cmReady;
}

// ── Custom CodeMirror mode for tag expressions ────────────────────────────────

function defineTagcalcsMode() {
  const CM = (window as any).CodeMirror;
  if (!CM || CM.modes['tagcalcs']) return;

  const FUNCTIONS = /^(avg|sum|min|max|count|countWhere|abs|round|sqrt|pow|floor|ceil|log|log10|sin|cos|tan|if)\b/;
  const NUMBER    = /^[+-]?(\d+\.?\d*|\.\d+)/;
  const TAG_REF   = /^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_*?]+)+/;
  const OPERATOR  = /^[+\-*/%^<>=!&|(),]/;

  CM.defineMode('tagcalcs', () => ({
    token(stream: any) {
      if (stream.eatSpace()) return null;
      if (stream.peek() === '"' || stream.peek() === "'") {
        const quote = stream.next();
        while (!stream.eol() && stream.peek() !== quote) {
          if (stream.next() === '\\') stream.next();
        }
        stream.next();
        return 'string';
      }
      if (FUNCTIONS.test(stream.current() + stream.peek())) {
        if (/[A-Za-z_]/.test(stream.peek() || '')) {
          stream.match(FUNCTIONS);
          return 'keyword';
        }
      }
      if (TAG_REF.test(stream.current() + stream.peek())) {
        if (/[A-Za-z_]/.test(stream.peek() || '')) {
          if (stream.match(TAG_REF)) {
            return stream.current().includes('*') || stream.current().includes('?')
              ? 'tag'
              : 'variable';
          }
        }
      }
      if (NUMBER.test(stream.current() + stream.peek())) {
        if (/[\d.]/.test(stream.peek() || '')) {
          stream.match(NUMBER);
          return 'number';
        }
      }
      stream.next();
      return OPERATOR.test(stream.current()) ? 'operator' : null;
    },
  }));
}

// ── Widget ────────────────────────────────────────────────────────────────────

interface Dialog {
  open: boolean;
  mode: 'create' | 'edit';
  script: Partial<TagCalc>;
  error: string;
  saving: boolean;
  testResult: string;
  testing: boolean;
}

export class TagCalcsWidget extends BaseComponent {
  private scripts: TagCalc[] = [];
  private loading = true;
  private error = '';
  private dialog: Dialog = {
    open: false, mode: 'create', script: {}, error: '',
    saving: false, testResult: '', testing: false,
  };
  private editor: any = null;  // CodeMirror instance
  private canManage = false;

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canManage] = await Promise.all([can('tagcalcs.view'), can('tagcalcs.manage')]);
    this.canManage = canManage;
    if (!canView && !canManage) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      this.scripts = await listTagCalcs();
    } catch {
      this.error = 'Failed to load tag calcs';
    }
    this.loading = false;
    this.rerender();
  }

  private rerender(): void {
    this.detachEventListeners();
    if (this.dialog.open) this.captureFormState();
    if (this.editor) { this.editor.toTextArea(); this.editor = null; }
    this.render();
    this.attachEventListeners();
    if (this.dialog.open) this.initEditor();
  }

  private captureFormState(): void {
    this.dialog.script = {
      ...this.dialog.script,
      name:            (this.querySelector<HTMLInputElement>('#ts-name')?.value     ?? this.dialog.script.name        ?? '').trim(),
      description:     (this.querySelector<HTMLInputElement>('#ts-desc')?.value     ?? this.dialog.script.description ?? '').trim(),
      outputTag:       (this.querySelector<HTMLInputElement>('#ts-output')?.value   ?? this.dialog.script.outputTag   ?? '').trim(),
      expression:      this.getExprValue() || (this.dialog.script.expression ?? ''),
      intervalSeconds: Number(this.querySelector<HTMLInputElement>('#ts-interval')?.value) || this.dialog.script.intervalSeconds || 60,
      enabled:         this.querySelector<HTMLInputElement>('#ts-enabled')?.checked ?? this.dialog.script.enabled ?? true,
    };
  }

  protected detachEventListeners(): void {}

  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Loading…</div>`;
      return;
    }
    if (this.error) {
      this.innerHTML = `<div class="p-8 text-center text-red-400 text-sm">${this.error}</div>`;
      return;
    }

    this.innerHTML = `
      <style>
        tagcalcs-widget .ts-table { width: 100%; border-collapse: collapse; }
        tagcalcs-widget .ts-table th {
          padding: 4px 8px;
          text-align: left;
          font-size: 0.75rem;
          font-weight: 500;
          letter-spacing: 0.08em;
          text-transform: uppercase;
          color: var(--accent-color);
          opacity: 0.7;
          border-bottom: 1px solid var(--border-color);
          font-family: var(--widget-font-family);
          white-space: nowrap;
        }
        tagcalcs-widget .ts-table td {
          padding: 4px 8px;
          border-bottom: 1px solid color-mix(in srgb, var(--border-color) 30%, transparent);
          font-family: var(--widget-font-family);
          font-size: 0.75rem;
          vertical-align: middle;
          line-height: 1.4;
        }
        tagcalcs-widget .ts-table tbody tr:hover td {
          background: color-mix(in srgb, var(--content-text) 3%, transparent);
        }
        tagcalcs-widget .ts-expr {
          font-family: monospace;
          font-size: calc(var(--widget-label-font-size));
          opacity: 0.8;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
        tagcalcs-widget .ts-badge {
          display: inline-flex;
          align-items: center;
          gap: 4px;
          padding: 1px 7px;
          border-radius: 2px;
          font-family: var(--widget-font-family);
          font-size: calc(var(--widget-label-font-size));
          letter-spacing: 0.04em;
        }
        tagcalcs-widget .ts-badge.enabled {
          background: var(--status-good-bg);
          color: var(--status-good-color);
          border: 1px solid color-mix(in srgb, var(--status-good-color) 25%, transparent);
        }
        tagcalcs-widget .ts-badge.disabled {
          background: color-mix(in srgb, var(--content-text) 4%, transparent);
          color: color-mix(in srgb, var(--content-text) 30%, transparent);
          border: 1px solid color-mix(in srgb, var(--content-text) 8%, transparent);
        }
        tagcalcs-widget .ts-action {
          padding: 2px 6px;
          border-radius: 3px;
          opacity: 0.6;
          transition: opacity 0.12s;
          cursor: pointer;
          background: none;
          border: none;
          color: inherit;
          font-size: 0.75rem;
          line-height: 1;
        }
        tagcalcs-widget .ts-action:hover { opacity: 1; }

        /* Dialog */
        tagcalcs-widget .ts-dialog-overlay {
          position: absolute; inset: 0;
          background: color-mix(in srgb, var(--widget-bg) 80%, transparent);
          display: flex; align-items: center; justify-content: center;
          z-index: 100;
          backdrop-filter: blur(2px);
        }
        tagcalcs-widget .ts-dialog {
          width: min(680px, 96vw);
          max-height: 90vh;
          overflow-y: auto;
          border-radius: 4px;
          border: 1px solid;
          border-color: var(--border-color);
          background: var(--widget-bg);
        }
        tagcalcs-widget .ts-dialog-title {
          font-family: var(--widget-font-family);
          font-size: var(--widget-label-font-size);
          letter-spacing: 0.1em;
          text-transform: uppercase;
          opacity: 0.8;
          padding: 14px 18px 0;
        }
        tagcalcs-widget .ts-field label {
          display: block;
          font-size: calc(var(--widget-label-font-size));
          letter-spacing: 0.06em;
          text-transform: uppercase;
          opacity: 0.8;
          margin-bottom: 5px;
          font-family: var(--widget-font-family);
        }
        tagcalcs-widget .ts-input {
          width: 100%;
          background: color-mix(in srgb, var(--content-text) 4%, transparent);
          border: 1px solid;
          border-color: var(--border-color);
          border-radius: 3px;
          padding: 7px 10px;
          font-size: var(--widget-label-font-size);
          color: inherit;
          outline: none;
          font-family: var(--widget-font-family);
          transition: border-color 0.12s;
        }
        tagcalcs-widget .ts-input:focus {
          border-color: var(--accent-color);
        }
        tagcalcs-widget .ts-input.mono {
          font-family: monospace;
          font-size: var(--widget-label-font-size);
        }
        tagcalcs-widget .ts-path-control {
          display: flex;
          gap: 4px;
        }
        tagcalcs-widget .ts-path-control .ts-input {
          min-width: 0;
        }
        tagcalcs-widget .ts-browse-btn {
          width: 32px;
          flex: 0 0 32px;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          background: color-mix(in srgb, var(--content-text) 4%, transparent);
          border: 1px solid var(--border-color);
          border-radius: 3px;
          color: inherit;
          opacity: 0.75;
          cursor: pointer;
          font-family: var(--widget-font-family);
          font-size: var(--widget-label-font-size);
        }
        tagcalcs-widget .ts-browse-btn:hover {
          opacity: 1;
          border-color: color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
        }
        tagcalcs-widget .ts-browse-btn:disabled {
          opacity: 0.35;
          cursor: not-allowed;
        }

        /* CodeMirror theme override */
        tagcalcs-widget .CodeMirror {
          font-family: monospace !important;
          font-size: var(--widget-label-font-size) !important;
          border-radius: 3px;
          border: 1px solid var(--border-color);
          background: var(--widget-header-bg) !important;
          height: 80px !important;
          color: var(--content-text) !important;
        }
        tagcalcs-widget .CodeMirror-scroll { min-height: 60px; }
        tagcalcs-widget .cm-keyword { color: var(--accent-color) !important; }
        tagcalcs-widget .cm-variable { color: var(--status-good-color) !important; }
        tagcalcs-widget .cm-tag { color: var(--status-warn-color) !important; }
        tagcalcs-widget .cm-number { color: color-mix(in srgb, var(--accent-color) 70%, var(--content-text)) !important; }
        tagcalcs-widget .cm-string { color: color-mix(in srgb, var(--status-warn-color) 80%, var(--content-text)) !important; }
        tagcalcs-widget .cm-operator { color: color-mix(in srgb, var(--content-text) 50%, transparent) !important; }
        tagcalcs-widget .CodeMirror-cursor { border-left-color: var(--accent-color) !important; }
        tagcalcs-widget .CodeMirror-selected { background: color-mix(in srgb, var(--accent-color) 20%, transparent) !important; }

        /* Test result */
        tagcalcs-widget .ts-result {
          font-family: monospace;
          font-size: var(--widget-label-font-size);
          padding: 6px 10px;
          border-radius: 3px;
          background: var(--widget-header-bg);
          border: 1px solid var(--border-color);
          min-height: 30px;
        }
        tagcalcs-widget .ts-result.ok { color: var(--status-good-color); border-color: color-mix(in srgb, var(--status-good-color) 25%, transparent); }
        tagcalcs-widget .ts-result.err { color: var(--error-color); border-color: color-mix(in srgb, var(--error-color) 25%, transparent); }
      </style>

      <div class="flex flex-col h-full relative" style="position: relative;">
        <!-- Header -->
        <div class="flex items-center justify-between px-4 py-3 shrink-0 border-b"
             style="border-color: var(--border-color); font-family: var(--widget-font-family);">
          <div class="flex items-center gap-3">
            <span style="font-size: var(--widget-label-font-size); letter-spacing: 0.08em; opacity: 0.65; text-transform: uppercase;">Tag Calcs</span>
            <span class="ts-badge opacity-60 ">${this.scripts.length} defined</span>
          </div>
          ${this.canManage ? `<button id="ts-new-btn"
                  class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background: color-mix(in srgb, var(--accent-color) 15%, transparent);
                         color: var(--accent-color);
                         border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
                         font-family: var(--widget-font-family); font-size: var(--widget-label-font-size);">
            + New Script
          </button>` : `<span class="text-xs opacity-50">Read only</span>`}
        </div>

        <!-- Table -->
        <div class="flex-1 overflow-auto">
          <table class="ts-table">
            <thead>
              <tr>
                <th>Name / Output</th>
                <th>Expression</th>
                <th>Interval</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              ${this.scripts.length === 0
                ? `<tr><td colspan="5">
                     <div class="flex flex-col items-center justify-center py-16 opacity-50"
                          style="font-family: var(--widget-font-family); font-size: var(--widget-label-font-size);">
                       <div style="font-size: 28px; margin-bottom: 8px; opacity: 0.4;">⨍</div>
                       no scripts defined
                     </div>
                   </td></tr>`
                : this.scripts.map(s => this.renderRow(s)).join('')
              }
            </tbody>
          </table>
        </div>

        ${this.dialog.open ? this.renderDialog() : ''}
      </div>
    `;
  }

  private renderRow(s: TagCalc): string {
    return `
      <tr>
        <td>
          <div>${this.esc(s.name)}</div>
          <div style="font-size: calc(var(--widget-label-font-size)); opacity: 0.45; margin-top: 1px;">${this.esc(s.outputTag)}</div>
        </td>
        <td>
          <span class="ts-expr" title="${this.esc(s.expression)}">${this.esc(s.expression)}</span>
        </td>
        <td>
          <span style="font-family: monospace; font-size: calc(var(--widget-label-font-size)); opacity: 0.6;">${s.intervalSeconds}s</span>
        </td>
        <td>
          <span class="ts-badge ${s.enabled ? 'enabled' : 'disabled'}">
            ${s.enabled ? '● on' : '○ off'}
          </span>
        </td>
        <td>
          <div class="flex items-center gap-1">
            ${this.canManage ? `<button class="ts-action ts-toggle-btn" data-id="${s.id}" data-enabled="${s.enabled}" title="${s.enabled ? 'Disable' : 'Enable'}">${s.enabled ? '⏸' : '▶'}</button>` : ''}
            <button class="ts-action ts-edit-btn" data-id="${s.id}" title="${this.canManage ? 'Edit' : 'View'}">✏️</button>
            ${this.canManage ? `<button class="ts-action ts-delete-btn" data-id="${s.id}" title="Delete" style="color: var(--danger-color);">🗑️</button>` : ''}
          </div>
        </td>
      </tr>
    `;
  }

  private renderDialog(): string {
    const s = this.dialog.script;
    const isEdit = this.dialog.mode === 'edit';
    const disabled = this.canManage ? '' : 'disabled';
    return `
      <div class="ts-dialog-overlay">
        <div class="ts-dialog">
          <div class="ts-dialog-title">${isEdit ? (this.canManage ? 'edit script' : 'view script') : 'new script'}</div>
          <div class="p-5 flex flex-col gap-4">

            <div class="grid gap-4" style="grid-template-columns: 0.75fr 1.25fr;">
              <div class="ts-field">
                <label>Name</label>
                <input id="ts-name" class="ts-input" placeholder="e.g. System Health" value="${this.esc(s.name ?? '')}" ${disabled}>
              </div>
              <div class="ts-field">
                <label>Output Tag</label>
                <div class="ts-path-control">
                  <input id="ts-output" class="ts-input mono" placeholder="CUSTOM.System.Health" value="${this.esc(s.outputTag ?? '')}" ${disabled}>
                  <button id="ts-output-browse" class="ts-browse-btn" type="button" title="Browse tags" aria-label="Browse output tag" ${disabled}>…</button>
                </div>
              </div>
            </div>

            <div class="ts-field">
              <label>Description</label>
              <input id="ts-desc" class="ts-input" placeholder="What does this script compute?" value="${this.esc(s.description ?? '')}" ${disabled}>
            </div>

            <div class="ts-field">
              <div style="display: flex; align-items: center; gap: 6px; margin-bottom: 5px;">
                <span style="font-size: calc(var(--widget-label-font-size)); letter-spacing: 0.06em; text-transform: uppercase; opacity: 0.8; font-family: var(--widget-font-family);">Expression</span>
                <button id="ts-expr-help-btn"
                        title="Syntax help"
                        style="display: flex; align-items: center; justify-content: center; width: 14px; height: 14px; border-radius: 50%; background: color-mix(in srgb, var(--accent-color) 20%, transparent); color: var(--accent-color); font-size: 9px; font-weight: bold; cursor: pointer; border: none; font-family: sans-serif; opacity: 0.8; line-height: 1; flex-shrink: 0;">?</button>
              </div>
              <textarea id="ts-expr-editor" style="display:none;">${this.esc(s.expression ?? '')}</textarea>
              <div id="ts-expr-loading" class="ts-input mono" style="min-height:80px; opacity:0.4; font-size:11px; font-family: monospace;">Loading editor…</div>

              <!-- Expression help popup -->
              <div id="ts-expr-help" class="hidden"
                   style="margin-top: 8px; border-radius: 3px; border: 1px solid; padding: 12px 14px;
                          border-color: color-mix(in srgb, var(--accent-color) 25%, transparent);
                          background: color-mix(in srgb, var(--accent-color) 5%, var(--widget-bg));
                          font-family: var(--widget-font-family);
                          font-size: calc(var(--widget-label-font-size));
                          line-height: 1.5;">

                <div style="font-weight: 600; color: var(--accent-color); margin-bottom: 10px; letter-spacing: 0.05em; text-transform: uppercase;">Expression Examples</div>

                <div style="display: grid; grid-template-columns: auto 1fr; gap: 3px 16px; align-items: baseline;">
                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">SITE.Device.temperature</code>
                  <span style="opacity: 0.7;">Raw value of a single tag</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">avg(SITE.*.temperature)</code>
                  <span style="opacity: 0.7;">Average across all matched tags</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">sum(PLANT.Tank*.level)</code>
                  <span style="opacity: 0.7;">Total of all matching tank levels</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">max(CTRL.*.pressure)</code>
                  <span style="opacity: 0.7;">Highest pressure value across devices</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">count(MOTOR.*.running)</code>
                  <span style="opacity: 0.7;">Number of tags with a non-zero value</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">round(FLOW.rate * 60, 2)</code>
                  <span style="opacity: 0.7;">Scale a value, rounded to 2 decimal places</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">if(PUMP.status &gt; 0, 100, 0)</code>
                  <span style="opacity: 0.7;">Conditional - 100 when on, 0 when off</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">if(B.input &gt; 0, B.output / B.input * 100, 0)</code>
                  <span style="opacity: 0.7;">Efficiency % (guard against divide-by-zero)</span>
                </div>

                <div style="margin-top: 10px; padding-top: 8px; border-top: 1px solid; border-color: color-mix(in srgb, var(--border-color) 60%, transparent);">
                  <span style="opacity: 0.5; margin-right: 8px; text-transform: uppercase; letter-spacing: 0.06em; font-size: calc(var(--widget-label-font-size) * 0.8);">Functions</span>
                  <span style="font-family: monospace; color: var(--accent-color);">avg  sum  min  max  count  countWhere  abs  round  sqrt  pow  floor  ceil  log  sin  cos  if</span>
                </div>
                <div style="margin-top: 6px; display: grid; grid-template-columns: auto 1fr; gap: 3px 16px; align-items: baseline;">
                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">countWhere(A.*.alarm, false)</code>
                  <span style="opacity: 0.7;">Count devices where boolean tag is false (true/false or 1/0 accepted)</span>

                  <code style="font-family: monospace; color: var(--status-good-color); white-space: nowrap;">countWhere(Incidents.*.kpi.type, 'Accident')</code>
                  <span style="opacity: 0.7;">Count tags matching an exact text value</span>
                </div>
                <div style="margin-top: 6px;">
                  <span style="opacity: 0.5; margin-right: 8px; text-transform: uppercase; letter-spacing: 0.06em; font-size: calc(var(--widget-label-font-size) * 0.8);">Wildcards</span>
                  <span style="font-family: monospace; color: var(--status-warn-color);">*</span><span style="opacity: 0.6;"> matches any path segment &nbsp;</span>
                  <span style="font-family: monospace; color: var(--status-warn-color);">?</span><span style="opacity: 0.6;"> matches a single character</span>
                </div>
              </div>
            </div>

            <div class="grid gap-4" style="grid-template-columns: 1fr 2fr;">
              <div class="ts-field">
                <label>Interval (seconds)</label>
                <input id="ts-interval" class="ts-input mono" type="number" min="1" value="${s.intervalSeconds ?? 60}" ${disabled}>
              </div>
              <div class="ts-field">
                <label>Status</label>
                <div class="flex items-center gap-3 h-9">
                  <label class="flex items-center gap-2 cursor-pointer" style="font-size: var(--widget-label-font-size);">
                    <input id="ts-enabled" type="checkbox" ${(s.enabled ?? true) ? 'checked' : ''} ${disabled}>
                    Enabled
                  </label>
                </div>
              </div>
            </div>

            <!-- Test -->
            ${this.canManage ? `<div class="flex items-start gap-3">
              <button id="ts-test-btn"
                      class="flex items-center gap-1.5 px-3 py-1.5 text-xs rounded shrink-0 transition-colors"
                      style="font-family: var(--widget-font-family); font-size: calc(var(--widget-label-font-size)); letter-spacing: 0.06em;
                             border: 1px solid var(--border-color); opacity: ${this.dialog.testing ? '0.5' : '0.8'};">
                ${this.dialog.testing ? '⟳ running…' : '▶ test'}
              </button>
              <div class="ts-result flex-1 ${this.dialog.testResult.startsWith('Error') ? 'err' : this.dialog.testResult ? 'ok' : ''}">
                ${this.dialog.testResult || '<span style="opacity:0.3;">run test to see live result</span>'}
              </div>
            </div>` : ''}

            ${this.dialog.error
              ? `<div class="text-xs px-3 py-2 rounded" style="background: var(--error-bg); color: var(--error-color); font-family: var(--widget-font-family); font-size: calc(var(--widget-label-font-size) * 0.85);">${this.esc(this.dialog.error)}</div>`
              : ''}

            <!-- Actions -->
            <div class="flex items-center justify-end gap-2 pt-1">
              <button id="ts-cancel-btn"
                      class="px-4 py-2 text-xs rounded transition-colors"
                      style="border: 1px solid var(--border-color); opacity: 0.6; font-family: var(--widget-font-family); font-size: var(--widget-label-font-size);">
                ${this.canManage ? 'Cancel' : 'Close'}
              </button>
              ${this.canManage ? `<button id="ts-save-btn"
                      class="px-4 py-2 text-xs rounded font-medium transition-colors"
                      style="background: color-mix(in srgb, var(--accent-color) 20%, transparent);
                             color: var(--accent-color);
                             border: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
                             font-family: var(--widget-font-family); font-size: var(--widget-label-font-size);
                             opacity: ${this.dialog.saving ? '0.5' : '1'};"
                      ${this.dialog.saving ? 'disabled' : ''}>
                ${this.dialog.saving ? 'Saving…' : (isEdit ? 'Update' : 'Create')}
              </button>` : ''}
            </div>
          </div>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    if (this.canManage) this.querySelector('#ts-new-btn')?.addEventListener('click', () => this.openDialog('create'));

    this.querySelectorAll('.ts-edit-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number((btn as HTMLElement).dataset.id);
        const s = this.scripts.find(x => x.id === id);
        if (s) this.openDialog('edit', s);
      });
    });

    if (this.canManage) this.querySelectorAll('.ts-delete-btn').forEach(btn => {
      btn.addEventListener('click', async () => {
        const id = Number((btn as HTMLElement).dataset.id);
        const confirmed = await showConfirm('Delete this tag calc?', {
          title: 'Delete tag calc',
          confirmLabel: 'Delete',
          cancelLabel: 'Keep',
          tone: 'danger',
        });
        if (!confirmed) return;
        await deleteTagCalc(id).catch(() => {});
        await this.loadData();
      });
    });

    if (this.canManage) this.querySelectorAll('.ts-toggle-btn').forEach(btn => {
      btn.addEventListener('click', async () => {
        const id = Number((btn as HTMLElement).dataset.id);
        const enabled = (btn as HTMLElement).dataset.enabled === 'true';
        const s = this.scripts.find(x => x.id === id);
        if (!s) return;
        await updateTagCalc(id, { ...s, enabled: !enabled }).catch(() => {});
        await this.loadData();
      });
    });

    this.querySelector('#ts-expr-help-btn')?.addEventListener('click', () => {
      this.querySelector('#ts-expr-help')?.classList.toggle('hidden');
    });

    this.querySelector('#ts-cancel-btn')?.addEventListener('click', () => {
      this.dialog.open = false;
      this.rerender();
    });

    if (this.canManage) this.querySelector('#ts-save-btn')?.addEventListener('click', () => this.saveDialog());
    if (this.canManage) this.querySelector('#ts-test-btn')?.addEventListener('click', () => this.runTest());
    if (this.canManage) this.querySelector('#ts-output-browse')?.addEventListener('click', () => this.openOutputTagPicker());
  }

  private openDialog(mode: 'create' | 'edit', script?: TagCalc): void {
    if (mode === 'create' && !this.canManage) return;
    this.dialog = {
      open: true, mode,
      script: script ? { ...script } : { enabled: true, intervalSeconds: 60 },
      error: '', saving: false, testResult: '', testing: false,
    };
    this.rerender();
  }

  private async initEditor(): Promise<void> {
    try {
      await loadCodeMirror();
      defineTagcalcsMode();
      const CM = (window as any).CodeMirror;
      const textarea = this.querySelector<HTMLTextAreaElement>('#ts-expr-editor');
      const placeholder = this.querySelector('#ts-expr-loading');
      if (!textarea) return;
      if (placeholder) (placeholder as HTMLElement).style.display = 'none';
      textarea.style.display = '';
      this.editor = CM.fromTextArea(textarea, {
        mode: 'tagcalcs',
        lineWrapping: true,
        lineNumbers: false,
        matchBrackets: true,
        autofocus: this.canManage,
        readOnly: !this.canManage,
      });
      this.editor.setSize('100%', '80px');
    } catch {
      // fallback: show plain textarea
      const ta = this.querySelector<HTMLTextAreaElement>('#ts-expr-editor');
      const ph = this.querySelector('#ts-expr-loading');
      if (ta) { ta.style.display = ''; ta.className = 'ts-input mono'; ta.rows = 3; }
      if (ph) (ph as HTMLElement).style.display = 'none';
    }
  }

  private getExprValue(): string {
    if (this.editor) return this.editor.getValue().trim();
    const ta = this.querySelector<HTMLTextAreaElement>('#ts-expr-editor');
    return ta?.value.trim() ?? '';
  }

  private openOutputTagPicker(): void {
    const input = this.querySelector<HTMLInputElement>('#ts-output');
    const selectedPath = input?.value.trim() ?? '';
    const expandTo = selectedPath.replace(/:[A-Z]$/, '');
    getTreeBrowserDialog().open('', 'Select Output Tag', (path) => {
      if (input) input.value = path.replace(/:[A-Z]$/, '');
    }, true, expandTo, expandTo);
  }

  private readDialog(): Partial<TagCalc> {
    return {
      name: (this.querySelector<HTMLInputElement>('#ts-name')?.value ?? '').trim(),
      description: (this.querySelector<HTMLInputElement>('#ts-desc')?.value ?? '').trim(),
      outputTag: (this.querySelector<HTMLInputElement>('#ts-output')?.value ?? '').trim(),
      expression: this.getExprValue(),
      intervalSeconds: Number(this.querySelector<HTMLInputElement>('#ts-interval')?.value) || 60,
      enabled: this.querySelector<HTMLInputElement>('#ts-enabled')?.checked ?? true,
    };
  }

  private async runTest(): Promise<void> {
    if (!this.canManage) return;
    const expr = this.getExprValue();
    if (!expr) { this.dialog.testResult = 'Error: expression is empty'; this.rerender(); return; }
    this.dialog.testing = true;
    this.dialog.testResult = '';
    this.rerender();
    try {
      const res = await testTagCalc(expr);
      this.dialog.testResult = res.error ? `Error: ${res.error}` : `Result: ${res.result}`;
    } catch (e: any) {
      this.dialog.testResult = `Error: ${e.message}`;
    }
    this.dialog.testing = false;
    this.rerender();
  }

  private async saveDialog(): Promise<void> {
    if (!this.canManage) return;
    const data = this.readDialog();
    if (!data.name)        { this.dialog.error = 'Name is required';         this.rerender(); return; }
    if (!data.outputTag)   { this.dialog.error = 'Output tag is required';   this.rerender(); return; }
    if (!data.expression)  { this.dialog.error = 'Expression is required';   this.rerender(); return; }

    this.dialog.saving = true;
    this.dialog.error = '';
    this.rerender();

    try {
      if (this.dialog.mode === 'edit' && this.dialog.script.id) {
        await updateTagCalc(this.dialog.script.id, data);
      } else {
        await createTagCalc(data);
      }
      this.dialog.open = false;
      await this.loadData();
    } catch (e: any) {
      this.dialog.saving = false;
      this.dialog.error = e.message || 'Failed to save script';
      this.rerender();
    }
  }

  private esc(s?: string): string {
    return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
}

customElements.define('tagcalcs-widget', TagCalcsWidget);
