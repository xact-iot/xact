import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import {
  listPDFTemplates, createPDFTemplate, updatePDFTemplate, deletePDFTemplate,
  previewPDFTemplate, generatePDF,
} from '../../api';
import type { PDFTemplate, PDFVariable } from '../../api';
import { showAlert } from '../../components/app-dialog';
import '../../components/json-editor';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import './pdf-template-widget.css';

registerPermissions('reports', 'PDF Reports', [
  { name: 'view', description: 'View and preview PDF report templates' },
  { name: 'manage', description: 'Create, edit, and generate PDF report templates' },
], 'Controls access to the PDF Report Template Manager - roles with view can inspect and preview templates; roles with manage can design templates and generate PDF reports.');

registerWidgetType({
  type: 'pdf-template-widget',
  name: 'PDF Reports',
  icon: '📄',
  category: 'System',
  defaultW: 12,
  defaultH: 24,
  minW: 10,
  minH: 16,
});

// ─── Data model (matches server/reporting/generator.go) ──────────────────────

interface BorderProps {
  left: number; right: number; top: number; bottom: number;
}

interface CellProps {
  text: string;
  font?: string;
  size?: number;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  align?: string;        // "L" | "C" | "R"
  bgColor?: string;
  textColor?: string;
  borders?: BorderProps;
  cellWidth?: number;
  cellHeight?: number;
  wrap?: boolean;
  linkUrl?: string;
  imageData?: string;
  imageFit?: 'contain' | 'stretch';
  imagePadding?: number;
}

interface ChartConfig {
  // Each entry is the full tag path below the org, e.g. "sensors.pump1.flow_rate".
  // May contain {{variable}} tokens which are substituted before generation.
  metrics: string[];
  lookback: string;  // "1h" | "6h" | "24h" | "7d" | "30d"
  title?: string;
  yLabel?: string;
  yMin?: number;
  yMax?: number;
  colors?: string[];
  smooth?: boolean;
  showLegend?: boolean;
  fillArea?: boolean;
}

interface EventsConfig {
  severity?: string;   // "" = all, or "DEBUG"|"INFO"|"WARN"|"ERROR"|"CRITICAL"
  device?: string;
  search?: string;
  lookback: string;    // "1h"|"6h"|"24h"|"7d"|"30d"
  limit: number;       // max rows in PDF output
  columns: string[];   // which columns to include: "timestamp","severity","user","device","message","params"
}

interface PieSliceConfig {
  value: string;  // tag path
  color: string;  // hex color
  label: string;  // legend label
}

interface PieChartConfig {
  slices: PieSliceConfig[];
  showLegend: boolean;
  height?: number;
}

interface TElement {
  id: string;
  type: 'title' | 'table' | 'spacer' | 'footer' | 'image' | 'chart' | 'events' | 'pie-chart';
  // title / table
  rows?: CellProps[][];
  colWidths?: number[];
  rowHeights?: number[];
  bgColor?: string;
  textColor?: string;
  allBorders?: boolean;
  noBorders?: boolean;
  // spacer / image / chart height
  height?: number;
  // footer
  text?: string;
  font?: string;
  size?: number;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  align?: string;
  textColor2?: string;
  footerBorders?: BorderProps;
  // image
  imageData?: string;
  width?: number;
  // chart
  chartConfig?: ChartConfig;
  // events
  eventsConfig?: EventsConfig;
  // pie-chart
  pieChartConfig?: PieChartConfig;
}

interface TDocConfig {
  pageSize: string;
  orientation: string;
  margins: { left: number; top: number; right: number; bottom: number };
  documentTitle: string;
  watermark?: string;
}

interface TDoc {
  config: TDocConfig;
  elements: TElement[];
}

type EditorTab = 'editor' | 'variables' | 'json' | 'preview';

interface WidgetState {
  view: 'list' | 'editor';
  templates: PDFTemplate[];
  loading: boolean;
  error: string;
  // editor
  editing: PDFTemplate | null;
  isNew: boolean;
  activeTab: EditorTab;
  doc: TDoc;
  variables: PDFVariable[];
  selectedEl: number;        // index into doc.elements, -1 = none
  selectedCell: { row: number; col: number } | null;
  templateName: string;
  templateDesc: string;
  dirty: boolean;
  saving: boolean;
  saveError: string;
  previewUrl: string | null;
  previewLoading: boolean;
  previewError: string;
  previewZoom: number;
  editorZoom: number;
  previewValues: Record<string, string>;  // runtime values for custom variables
  deleteConfirm: string | null;
}

// ─── Default document ────────────────────────────────────────────────────────

let _eid = 0;
function eid(): string { return `e${++_eid}`; }

function defaultDoc(): TDoc {
  return {
    config: {
      pageSize: 'A4',
      orientation: 'P',
      documentTitle: '{{report_name}}',
      watermark: '',
      margins: { left: 72, top: 72, right: 72, bottom: 72 },
    },
    elements: [
      {
        id: eid(), type: 'title',
        rows: [[{ text: '{{report_name}}', bold: true, size: 18, align: 'C' }]],
        colWidths: [1], rowHeights: [2],
        bgColor: '#1e3a5f', textColor: '#ffffff', noBorders: true,
      },
      {
        id: eid(), type: 'table',
        rows: [
          [{ text: 'Organisation', bold: true }, { text: '{{org_name}}' }],
          [{ text: 'Date', bold: true }, { text: '{{report_date}}' }],
        ],
        colWidths: [1, 2], rowHeights: [1, 1],
        bgColor: '#ffffff', textColor: '#000000', allBorders: true,
      },
      { id: eid(), type: 'spacer', height: 36 },
    ],
  };
}

const DEFAULT_VARIABLES: PDFVariable[] = [
  { name: 'report_name', label: 'Report Name', type: 'builtin', source: 'report_name' },
  { name: 'org_name', label: 'Organisation', type: 'builtin', source: 'org_name' },
  { name: 'report_date', label: 'Report Date', type: 'builtin', source: 'now', format: '2006-01-02' },
];

// ─── Widget ──────────────────────────────────────────────────────────────────

export class PDFTemplateWidget extends BaseComponent {
  private canManage = false;
  private state: WidgetState = {
    view: 'list',
    templates: [],
    loading: true,
    error: '',
    editing: null,
    isNew: false,
    activeTab: 'editor',
    doc: defaultDoc(),
    variables: structuredClone(DEFAULT_VARIABLES),
    selectedEl: -1,
    selectedCell: null,
    templateName: '',
    templateDesc: '',
    dirty: false,
    saving: false,
    saveError: '',
    previewUrl: null,
    previewLoading: false,
    previewError: '',
    previewZoom: 1.0,
    editorZoom: 1.0,
    previewValues: {},
    deleteConfirm: null,
  };

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.revokePreview();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canManage] = await Promise.all([can('reports.view'), can('reports.manage')]);
    this.canManage = canManage;
    if (!canView && !canManage) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadTemplates();
  }

  private async loadTemplates(): Promise<void> {
    try {
      this.state.templates = await listPDFTemplates();
    } catch {
      this.state.error = 'Failed to load templates';
    }
    this.state.loading = false;
    this.rerender();
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  protected detachEventListeners(): void {}

  // ─── Render ─────────────────────────────────────────────────────────────────

  protected render(): void {
    if (this.state.loading) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Loading…</div>`;
      return;
    }
    if (this.state.error && this.state.view === 'list') {
      this.innerHTML = `<div class="p-8 text-center text-sm" style="color:#f87171">${this.esc(this.state.error)}</div>`;
      return;
    }
    this.innerHTML = this.state.view === 'list' ? this.renderList() : this.renderEditorShell();
    if (this.state.view === 'editor') {
      this.postRenderEditor();
    }
  }

  // ─── List view ───────────────────────────────────────────────────────────────

  private renderList(): string {
    const s = this.state;
    return `
      <div class="flex flex-col h-full overflow-hidden">
        <div class="flex items-center justify-between px-4 py-3 border-b shrink-0"
             style="border-color:var(--border-color)">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium" style="letter-spacing:.04em">PDF REPORT TEMPLATES</span>
            <span class="text-xs px-2 py-0.5 rounded font-mono"
                  style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);
                         color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 25%,transparent)">
              ${s.templates.length}
            </span>
          </div>
          ${this.canManage ? `<button id="btn-new-template" class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded"
                  style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);
                         color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent)">
            <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/>
            </svg>
            New Template
          </button>` : `<span class="text-xs opacity-50">Read only</span>`}
        </div>
        <div class="flex-1 overflow-auto">
          ${s.templates.length === 0 ? `
            <div class="flex flex-col items-center justify-center h-full gap-3 opacity-40">
              <svg class="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                      d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
              </svg>
              <div class="text-sm">No report templates yet</div>
              <div class="text-xs">Click "New Template" to get started</div>
            </div>
          ` : `
            <table class="w-full text-sm border-collapse">
              <thead>
                <tr class="text-left text-xs font-medium uppercase tracking-wide"
                    style="border-bottom:1px solid var(--border-color);opacity:.45">
                  <th class="px-4 py-2.5">Name</th>
                  <th class="px-4 py-2.5">Description</th>
                  <th class="px-4 py-2.5">Updated</th>
                  <th class="px-4 py-2.5 w-32"></th>
                </tr>
              </thead>
              <tbody>
                ${s.templates.map(t => this.renderTemplateRow(t)).join('')}
              </tbody>
            </table>
          `}
        </div>
        ${s.deleteConfirm ? this.renderDeleteConfirm() : ''}
      </div>`;
  }

  private renderTemplateRow(t: PDFTemplate): string {
    const updated = new Date(t.updatedAt).toLocaleDateString();
    return `
      <tr class="border-b transition-opacity" style="border-color:var(--border-color)">
        <td class="px-4 py-3">
          <div class="flex items-center gap-2">
            <div class="w-6 h-6 rounded flex items-center justify-center shrink-0"
                 style="background:color-mix(in srgb,var(--accent-color) 12%,transparent)">
              <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"
                   style="color:var(--accent-color)">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
              </svg>
            </div>
            <span class="font-medium text-sm">${this.esc(t.name)}</span>
          </div>
        </td>
        <td class="px-4 py-3 text-xs opacity-60">${this.esc(t.description)}</td>
        <td class="px-4 py-3 text-xs opacity-50 font-mono">${updated}</td>
        <td class="px-4 py-3">
          <div class="flex items-center gap-1 justify-end">
            <button class="btn-download-template p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="Download PDF" data-id="${this.esc(t.id)}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>
              </svg>
            </button>
            <button class="btn-edit-template p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="Edit template" data-id="${this.esc(t.id)}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
              </svg>
            </button>
            ${this.canManage ? `<button class="btn-delete-template p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="Delete template" data-id="${this.esc(t.id)}" data-name="${this.esc(t.name)}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
              </svg>
            </button>` : ''}
          </div>
        </td>
      </tr>`;
  }

  private renderDeleteConfirm(): string {
    const t = this.state.templates.find(t => t.id === this.state.deleteConfirm);
    return `
      <div class="fixed inset-0 z-50 flex items-center justify-center" style="background:rgba(0,0,0,.6)">
        <div class="rounded-lg border shadow-xl w-80 p-5"
             style="background:var(--header-bg);border-color:var(--border-color)">
          <div class="text-sm font-medium mb-2">Delete Template</div>
          <div class="text-xs opacity-60 mb-4">
            Delete <strong>${this.esc(t?.name ?? '')}</strong>? This cannot be undone.
          </div>
          <div class="flex gap-2 justify-end">
            <button id="btn-delete-cancel" class="px-3 py-1.5 text-xs rounded border"
                    style="border-color:var(--border-color)">Cancel</button>
            <button id="btn-delete-confirm" class="px-3 py-1.5 text-xs font-medium rounded"
                    style="background:rgba(239,68,68,.15);color:#f87171;border:1px solid rgba(239,68,68,.3)">
              Delete
            </button>
          </div>
        </div>
      </div>`;
  }

  // ─── Editor shell ────────────────────────────────────────────────────────────

  private renderEditorShell(): string {
    const s = this.state;
    return `
      <div class="flex flex-col h-full overflow-hidden" id="editor-shell">
        <!-- Header bar -->
        <div class="flex items-center gap-3 px-4 py-2 border-b shrink-0"
             style="border-color:var(--border-color)">
          <button id="btn-back-to-list"
                  class="flex items-center gap-1 text-xs opacity-50 hover:opacity-100 transition-opacity">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/>
            </svg>
            Templates
          </button>
          <span class="opacity-20">/</span>
          <input id="editor-name" type="text" value="${this.esc(s.templateName)}"
                 placeholder="Template name (required)"
                 class="flex-1 bg-transparent text-sm font-medium outline-none"
                 style="color:inherit;min-width:0" ${this.canManage ? '' : 'disabled'}>
          <div class="flex items-center gap-2 ml-auto shrink-0">
            ${s.saveError ? `<span class="text-xs" style="color:#f87171">${this.esc(s.saveError)}</span>` : ''}
            <span id="dirty-indicator" class="text-xs font-semibold px-2 py-0.5 rounded"
                  style="color:#fed7aa;background:rgba(249,115,22,0.16);border:1px solid rgba(249,115,22,0.35)"
                  ${s.dirty ? '' : 'hidden'}>Unsaved</span>
            ${s.editing && !s.isNew ? `
              <button id="btn-download-current"
                      class="flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--accent-color) 35%,transparent);color:var(--accent-color)">
                <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>
                </svg>
                Download
              </button>` : ''}
            ${this.canManage ? `<button id="btn-save-template"
                    class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded transition-colors"
                    style="${s.dirty ? 'background:var(--accent-color);color:var(--accent-text)' : 'background:rgba(148,163,184,0.18);color:rgba(148,163,184,0.65);cursor:not-allowed'}"
                    ${s.saving || !s.dirty ? 'disabled' : ''}>
              ${s.saving ? 'Saving…' : 'Save'}
            </button>` : ''}
          </div>
        </div>

        <!-- Description -->
        <div class="px-4 py-1.5 border-b shrink-0" style="border-color:var(--border-color)">
          <input id="editor-desc" type="text" value="${this.esc(s.templateDesc)}"
                 placeholder="Optional description"
                 class="w-full bg-transparent text-xs outline-none opacity-60" style="color:inherit" ${this.canManage ? '' : 'disabled'}>
        </div>

        <!-- Tab bar -->
        <div class="flex border-b shrink-0" style="border-color:var(--border-color)">
          ${(this.canManage ? (['editor', 'variables', 'json', 'preview'] as EditorTab[]) : (['preview'] as EditorTab[])).map(tab => `
            <button class="tab-btn px-4 py-2 text-xs border-b-2 transition-colors ${tab === s.activeTab
              ? 'font-medium' : 'border-transparent opacity-50 hover:opacity-80'}"
                    data-tab="${tab}"
                    style="${tab === s.activeTab
                      ? 'border-color:var(--accent-color);color:var(--accent-color)'
                      : 'border-color:transparent'}">
              ${tab === 'editor' ? 'Editor' : tab.charAt(0).toUpperCase() + tab.slice(1)}
            </button>
          `).join('')}
        </div>

        <!-- Tab content -->
        <div class="flex-1 overflow-hidden relative" id="tab-content">
          ${s.activeTab === 'editor' ? this.renderWYSIWYG() : ''}
          ${s.activeTab === 'variables' ? this.renderVariablesTab() : ''}
          ${s.activeTab === 'json' ? this.renderJSONTab() : ''}
          ${s.activeTab === 'preview' ? this.renderPreviewTab() : ''}
        </div>
      </div>`;
  }

  // ─── WYSIWYG editor (3-panel) ─────────────────────────────────────────────

  private renderWYSIWYG(): string {
    return `
      <div class="flex h-full overflow-hidden" id="wysiwyg-panels">
        ${this.renderPalette()}
        ${this.renderCanvas()}
        ${this.renderProperties()}
      </div>`;
  }

  // ─── Left dashboard: palette + document settings ─────────────────────────────

  private renderPalette(): string {
    const cfg = this.state.doc.config;
    const m = cfg.margins;
    const panelBg = 'background:color-mix(in srgb,var(--panel-bg,var(--widget-bg,#0d1117)) 100%,transparent)';
    return `
      <div class="flex flex-col overflow-y-auto shrink-0 border-r"
           style="width:210px;${panelBg};border-color:var(--border-color)">

        <!-- Component palette -->
        <div class="px-3 pt-3 pb-2">
          <div class="text-xs font-medium opacity-40 uppercase tracking-widest mb-2">Components</div>
          <div class="grid grid-cols-2 gap-1.5">
            ${[
              ['title', 'T', 'Title'],
              ['table', '⊞', 'Table'],
              ['footer', '-', 'Footer'],
              ['spacer', '↕', 'Spacer'],
              ['image', '🖼', 'Image'],
              ['chart', '📈', 'Chart'],
              ['events', '📋', 'Events'],
              ['pie-chart', '◔', 'Pie Chart'],
            ].map(([type, icon, label]) => `
              <div class="palette-item flex items-center gap-1.5 px-2 py-1.5 rounded cursor-grab text-xs
                          border select-none transition-colors hover:border-accent"
                   draggable="true" data-palette-type="${type}"
                   style="border-color:var(--border-color);
                          background:color-mix(in srgb,var(--accent-color) 4%,transparent)">
                <span class="text-sm leading-none">${icon}</span>
                <span>${label}</span>
              </div>
            `).join('')}
          </div>
        </div>

        <div class="border-t mx-3" style="border-color:var(--border-color)"></div>

        <!-- Document settings -->
        <div class="px-3 py-2 space-y-2 text-xs">
          <div class="font-medium opacity-40 uppercase tracking-widest mt-1">Document Settings</div>

          <div class="flex gap-2">
            <div class="flex-1">
              <div class="opacity-50 mb-0.5">Page Size</div>
              <select id="cfg-pagesize" class="prop-select">
                ${['A4','Letter','Legal'].map(p => `<option value="${p}" ${cfg.pageSize===p?'selected':''}>${p}</option>`).join('')}
              </select>
            </div>
            <div class="flex-1">
              <div class="opacity-50 mb-0.5">Orientation</div>
              <select id="cfg-orientation" class="prop-select">
                <option value="P" ${cfg.orientation==='P'?'selected':''}>Portrait</option>
                <option value="L" ${cfg.orientation==='L'?'selected':''}>Landscape</option>
              </select>
            </div>
          </div>

          <div>
            <div class="opacity-50 mb-0.5">Document Title</div>
            <input id="cfg-doctitle" type="text" value="${this.esc(cfg.documentTitle)}"
                   class="prop-input"
                   placeholder="e.g. {{report_name}}">
          </div>

          <div>
            <div class="opacity-50 mb-0.5">Watermark</div>
            <input id="cfg-watermark" type="text" value="${this.esc(cfg.watermark ?? '')}"
                   class="prop-input"
                   placeholder="Optional watermark text">
          </div>

          <div class="opacity-50 mb-0">Page Margins (pt)</div>
          <div class="grid grid-cols-2 gap-1.5">
            ${([['Left','cfg-ml',m.left],['Right','cfg-mr',m.right],['Top','cfg-mt',m.top],['Bottom','cfg-mb',m.bottom]] as [string,string,number][]).map(([lbl,id,val]) => `
              <div>
                <div class="opacity-50 mb-0.5">${lbl}</div>
                <input id="${id}" type="number" value="${val}" min="0" max="200"
                       class="prop-input">
              </div>
            `).join('')}
          </div>

          <div class="flex gap-1 pt-0.5">
            <button class="cfg-margin-preset prop-btn prop-text-10 flex-1 hover:opacity-80"
                    data-preset="0">0 pt all</button>
            <button class="cfg-margin-preset prop-btn prop-text-10 flex-1 hover:opacity-80"
                    data-preset="36">36 pt all</button>
            <button class="cfg-margin-preset prop-btn prop-text-10 flex-1 hover:opacity-80"
                    data-preset="72">72 pt all</button>
          </div>
        </div>
      </div>`;
  }

  // ─── Center dashboard: A4 canvas ─────────────────────────────────────────────

  private renderCanvas(): string {
    const s = this.state;
    const elements = s.doc.elements;

    // A4 page at ~0.6 zoom: 595pt × 842pt → 357px × 505px
    // We scale content width as well to match
    const pageW = Math.round(420 * s.editorZoom); // px display width of the A4 canvas
    const pageH = Math.round(pageW * 842 / 595);
    const scaleF = pageW / 595;

    const cfg = s.doc.config;
    const mlPx = (cfg.margins.left * scaleF).toFixed(1);
    const mrPx = (cfg.margins.right * scaleF).toFixed(1);
    const mtPx = (cfg.margins.top * scaleF).toFixed(1);
    const contentWPx = pageW - (cfg.margins.left + cfg.margins.right) * scaleF;

    const elementsHtml = elements.map((el, idx) => {
      const isSelected = idx === s.selectedEl;
      const overlay = isSelected ? `
        <div class="el-overlay" data-el="${idx}"
             style="position:absolute;top:0;left:0;right:0;display:flex;align-items:flex-start;justify-content:flex-end;
                    gap:2px;padding:2px 3px;pointer-events:none">
          <button class="el-move-up" data-el="${idx}" title="Move up"
                  style="pointer-events:all;width:18px;height:18px;border-radius:3px;border:none;cursor:pointer;
                         background:rgba(0,0,0,.7);color:#fff;font-size:10px;display:flex;align-items:center;justify-content:center">▲</button>
          <button class="el-move-down" data-el="${idx}" title="Move down"
                  style="pointer-events:all;width:18px;height:18px;border-radius:3px;border:none;cursor:pointer;
                         background:rgba(0,0,0,.7);color:#fff;font-size:10px;display:flex;align-items:center;justify-content:center">▼</button>
          <button class="el-delete" data-el="${idx}" title="Delete element"
                  style="pointer-events:all;width:18px;height:18px;border-radius:3px;border:none;cursor:pointer;
                         background:rgba(180,30,30,.85);color:#fff;font-size:10px;font-weight:bold;
                         display:flex;align-items:center;justify-content:center">✕</button>
        </div>` : '';

      return `
        <div class="canvas-drop-zone" data-drop-index="${idx}"
             style="height:6px;margin:0 -2px;border-radius:2px;transition:all .15s">
        </div>
        <div class="canvas-element ${isSelected ? 'selected' : ''}" data-el="${idx}"
             style="position:relative;cursor:pointer;border-radius:2px;
                    outline:${isSelected ? '2px solid var(--accent-color)' : '2px solid transparent'};
                    outline-offset:1px">
          ${overlay}
          ${this.renderCanvasElement(el, contentWPx, scaleF, idx)}
        </div>`;
    }).join('');

    return `
      <div class="flex-1 overflow-auto flex flex-col items-center py-6"
           style="background:color-mix(in srgb,var(--panel-bg,var(--widget-bg,#0d1117)) 100%,transparent)">
        <div style="width:${pageW}px">
          <div class="flex items-center justify-center gap-2 mb-2">
            <span class="text-xs opacity-30 font-mono">${cfg.pageSize} - ${cfg.orientation === 'P' ? 'Portrait' : 'Landscape'}</span>
            <div class="flex items-center gap-1 ml-2">
              <button id="btn-editor-zoom-out" title="Zoom out"
                      class="text-xs px-2 py-0.5 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)"
                      ${s.editorZoom <= 0.25 ? 'disabled' : ''}>−</button>
              <span class="text-xs w-10 text-center tabular-nums opacity-50">${Math.round(s.editorZoom * 100)}%</span>
              <button id="btn-editor-zoom-in" title="Zoom in"
                      class="text-xs px-2 py-0.5 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)"
                      ${s.editorZoom >= 3 ? 'disabled' : ''}>+</button>
              <button id="btn-editor-zoom-reset" title="Reset zoom"
                      class="text-xs px-2 py-0.5 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)">1:1</button>
            </div>
          </div>
          <div id="pdf-canvas"
               style="width:${pageW}px;min-height:${pageH}px;background:#fff;color:#000;
                      box-shadow:0 4px 24px rgba(0,0,0,.4);position:relative;
                      padding:${mtPx}px ${mrPx}px 0 ${mlPx}px;box-sizing:border-box">
            ${elementsHtml}
            <div class="canvas-drop-zone" data-drop-index="${elements.length}"
                 style="height:6px;margin:0 -2px;border-radius:2px;transition:all .15s">
            </div>
            <div class="canvas-drop-final" data-drop-index="${elements.length}"
                 style="min-height:60px;display:flex;align-items:center;justify-content:center">
              <div class="text-xs opacity-20 select-none">Drop elements here</div>
            </div>
          </div>
        </div>
      </div>`;
  }

  private renderCanvasElement(el: TElement, contentW: number, scale: number, elIdx: number): string {
    switch (el.type) {
      case 'title':
      case 'table':
        return this.renderCanvasGrid(el, contentW, scale, elIdx);
      case 'spacer':
        return this.renderCanvasSpacer(el, scale);
      case 'footer':
        return this.renderCanvasFooter(el, contentW);
      case 'image':
        return this.renderCanvasImage(el, contentW, scale);
      case 'chart':
        return this.renderCanvasChart(el, contentW, scale);
case 'events':
        return this.renderCanvasEvents(el, contentW, scale);
      case 'pie-chart':
        return this.renderCanvasPieChart(el, contentW, scale);
      default:
        return `<div style="padding:4px;font-size:11px;opacity:.4">[unknown element]</div>`;
    }
  }

  private renderCanvasGrid(el: TElement, _contentW: number, scale: number, elIdx: number): string {
    const rows = el.rows ?? [];
    if (rows.length === 0) {
      return `<div style="padding:8px;font-size:10px;opacity:.3;text-align:center;border:1px dashed #ccc">
                Empty ${el.type} - add rows in properties
              </div>`;
    }
    const numCols = Math.max(...rows.map(r => r.length), 1);
    const weights = Array.from({ length: numCols }, (_, i) =>
      (el.colWidths && el.colWidths[i] > 0) ? el.colWidths[i] : 1
    );
    const totalW = weights.reduce((a, b) => a + b, 0);
    const colPct = weights.map(w => (w / totalW * 100).toFixed(2));
    const defRowH = 22; // px
    const s = this.state;

    const borderAll = el.allBorders;
    const noBorders = el.noBorders;

    return `
      <table style="width:100%;border-collapse:collapse;table-layout:fixed">
        ${rows.map((row, ri) => {
          const rowHMulti = (el.rowHeights && el.rowHeights[ri] > 0) ? el.rowHeights[ri] : 1;
          const rowH = Math.round(defRowH * rowHMulti);
          return `<tr>
            ${Array.from({ length: numCols }, (_, ci) => {
              const cell: CellProps = (ci < row.length) ? row[ci] : { text: '' };
              const isSelectedCell = s.selectedEl === elIdx &&
                s.selectedCell?.row === ri && s.selectedCell?.col === ci;

              const cellBg = cell.bgColor ?? el.bgColor ?? '#ffffff';
              const cellTx = cell.textColor ?? el.textColor ?? '#000000';
              const fam = cell.font ?? 'Helvetica, Arial, sans-serif';
              const sz = Math.max(8, Math.round((cell.size ?? (el.type === 'title' ? 14 : 10)) * scale));
              const fw = cell.bold ? 'bold' : 'normal';
              const fs = cell.italic ? 'italic' : 'normal';
              const td = cell.underline ? 'underline' : 'none';
              const align = cell.align === 'C' ? 'center' : cell.align === 'R' ? 'right' : 'left';

              let borderStyle = '';
              if (cell.borders) {
                const b = cell.borders;
                borderStyle = `border-left:${b.left}px solid #999;border-right:${b.right}px solid #999;
                               border-top:${b.top}px solid #999;border-bottom:${b.bottom}px solid #999;`;
              } else if (noBorders) {
                borderStyle = 'border:none;';
              } else if (borderAll) {
                borderStyle = 'border:1px solid #ccc;';
              }

              const selStyle = isSelectedCell
                ? 'outline:2px solid var(--accent-color);outline-offset:-1px;'
                : '';

              const cellContent = cell.imageData
                ? `<img src="${this.esc(cell.imageData)}"
                        style="display:block;width:100%;height:100%;object-fit:${cell.imageFit === 'stretch' ? 'fill' : 'contain'};padding:${Math.max(0, cell.imagePadding ?? 2)}px;box-sizing:border-box;">`
                : this.esc(cell.text);

              return `<td class="canvas-cell" data-el="${elIdx}" data-row="${ri}" data-col="${ci}"
                          style="width:${colPct[ci]}%;height:${rowH}px;background:${cellBg};color:${cellTx};
                                 font-family:${fam};font-size:${sz}px;font-weight:${fw};
                                 font-style:${fs};text-decoration:${td};text-align:${align};
                                 padding:2px 4px;overflow:hidden;cursor:pointer;
                                 ${borderStyle}${selStyle}vertical-align:middle">
                        ${cellContent}
                      </td>`;
            }).join('')}
          </tr>`;
        }).join('')}
      </table>`;
  }

  private renderCanvasSpacer(el: TElement, scale: number): string {
    const h = Math.max(8, Math.round((el.height ?? 36) * scale * 0.352778));
    return `
      <div style="height:${h}px;display:flex;align-items:center;justify-content:center;
                  border:1px dashed #ccc;border-radius:2px">
        <span style="font-size:10px;color:#aaa;user-select:none">Spacer (${el.height ?? 36} pt)</span>
      </div>`;
  }

  private renderCanvasFooter(el: TElement, _contentW: number): string {
    const fam = el.font ?? 'Helvetica, Arial, sans-serif';
    const sz = el.size ?? 10;
    const fw = el.bold ? 'bold' : 'normal';
    const fs = el.italic ? 'italic' : 'normal';
    const td = el.underline ? 'underline' : 'none';
    const align = el.align === 'C' ? 'center' : el.align === 'R' ? 'right' : 'left';
    const tx = el.textColor2 ?? '#000000';

    let borderCss = '';
    if (el.footerBorders) {
      const b = el.footerBorders;
      borderCss = `border-left:${b.left}px solid #999;border-right:${b.right}px solid #999;
                   border-top:${b.top}px solid #999;border-bottom:${b.bottom}px solid #999;`;
    }

    return `
      <div style="width:100%;padding:3px 0;font-family:${fam};font-size:${sz}px;
                  font-weight:${fw};font-style:${fs};text-decoration:${td};
                  text-align:${align};color:${tx};${borderCss}">
        ${this.esc(el.text ?? '')}
      </div>`;
  }

  private renderCanvasImage(el: TElement, contentW: number, scale: number): string {
    const h = Math.max(20, Math.round((el.height ?? 72) * scale * 0.352778));
    const w = el.width ? Math.min(contentW, Math.round(el.width * scale * 0.352778)) : contentW;
    if (el.imageData) {
      return `<img src="${el.imageData}" style="width:${w}px;height:${h}px;object-fit:contain;display:block">`;
    }
    return `
      <div style="width:${w}px;height:${h}px;display:flex;align-items:center;justify-content:center;
                  background:#f0f0f0;border:1px dashed #ccc;border-radius:2px">
        <span style="font-size:11px;color:#aaa">🖼 Image (${el.height ?? 72} pt tall)</span>
      </div>`;
  }

  private renderCanvasChart(el: TElement, contentW: number, scale: number): string {
    const h = Math.max(30, Math.round((el.height ?? 200) * scale * 0.352778));
    const cfg = el.chartConfig;
    const metrics = cfg?.metrics?.filter(m => m.trim()) ?? [];
    const titleText = cfg?.title || 'Time Series Chart';
    const lookback = cfg?.lookback || '24h';
    return `
      <div style="width:${contentW}px;height:${h}px;display:flex;flex-direction:column;
                  align-items:center;justify-content:center;gap:4px;
                  background:#f8fafc;border:2px dashed #94a3b8;border-radius:3px;box-sizing:border-box">
        <svg width="28" height="20" viewBox="0 0 28 20" fill="none" style="opacity:.5">
          <polyline points="2,16 8,10 13,13 20,4 26,7" stroke="#334155" stroke-width="1.5" fill="none" stroke-linejoin="round"/>
          <polyline points="2,16 8,8 13,11 20,2 26,5" stroke="#64748b" stroke-width="1" fill="none" stroke-linejoin="round" stroke-dasharray="2,2"/>
        </svg>
        <div style="font-size:${Math.round(10*scale)}px;font-weight:600;color:#334155">${this.esc(titleText)}</div>
        <div style="font-size:${Math.round(9*scale)}px;color:#94a3b8">
          ${metrics.length > 0
            ? metrics.map(m => this.esc(m)).join(' · ')
            : '<span style="color:#f87171">No metrics set</span>'}
          · ${lookback} · ${el.height ?? 200}pt
        </div>
      </div>`;
  }

  private renderCanvasEvents(el: TElement, contentW: number, scale: number): string {
    const cfg = el.eventsConfig;
    const sev = cfg?.severity || 'All';
    const dev = cfg?.device ? this.esc(cfg.device) : 'All';
    const search = cfg?.search ? `"${this.esc(cfg.search)}"` : '-';
    const lookback = cfg?.lookback || '24h';
    const limit = cfg?.limit ?? 50;
    const cols = cfg?.columns ?? ['timestamp', 'severity', 'device', 'message'];

    // Build a miniature table preview
    const colLabels: Record<string, string> = {
      timestamp: 'Timestamp', severity: 'Sev', user: 'User',
      device: 'Device', message: 'Message', params: 'Params',
    };
    const visibleCols = cols.filter(c => colLabels[c]);
    const thCells = visibleCols.map(c =>
      `<th style="padding:2px 4px;text-align:left;font-size:${Math.round(8 * scale)}px;
                  font-weight:600;color:#475569;border-bottom:1px solid #cbd5e1;white-space:nowrap">
        ${colLabels[c]}
      </th>`
    ).join('');

    // Sample data rows
    const sampleRows = [
      { timestamp: '2026-03-27 09:14:22', severity: 'INFO', user: 'admin', device: 'pump-01', message: 'Valve opened', params: 'psi=42' },
      { timestamp: '2026-03-27 09:15:08', severity: 'WARN', user: 'system', device: 'tank-03', message: 'Level high', params: 'level=89%' },
      { timestamp: '2026-03-27 09:16:44', severity: 'ERROR', user: '-', device: 'sensor-12', message: 'Read timeout', params: '' },
    ];
    const sevColors: Record<string, string> = {
      INFO: '#3b82f6', WARN: '#f59e0b', ERROR: '#ef4444', DEBUG: '#64748b', CRITICAL: '#dc2626',
    };
    const sampleHtml = sampleRows.map(r => {
      const cells = visibleCols.map(c => {
        const val = (r as any)[c] ?? '';
        if (c === 'severity') {
          return `<td style="padding:1px 4px;font-size:${Math.round(7 * scale)}px">
            <span style="background:${sevColors[val] ?? '#64748b'}22;color:${sevColors[val] ?? '#64748b'};
                         padding:0 3px;border-radius:2px;font-weight:600;font-size:${Math.round(6.5 * scale)}px">${val}</span>
          </td>`;
        }
        return `<td style="padding:1px 4px;font-size:${Math.round(7 * scale)}px;color:#64748b;
                           white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:${c === 'message' ? '120px' : '80px'}">${val}</td>`;
      }).join('');
      return `<tr style="border-bottom:1px solid #e2e8f0">${cells}</tr>`;
    }).join('');

    return `
      <div style="width:${contentW}px;display:flex;flex-direction:column;
                  background:#f8fafc;border:2px dashed #94a3b8;border-radius:3px;box-sizing:border-box;
                  overflow:hidden">
        <!-- Header -->
        <div style="display:flex;align-items:center;gap:6px;padding:4px 8px;
                    background:#f1f5f9;border-bottom:1px solid #cbd5e1">
          <span style="font-size:${Math.round(11 * scale)}px">📋</span>
          <span style="font-size:${Math.round(9 * scale)}px;font-weight:600;color:#334155">Events List</span>
          <span style="font-size:${Math.round(8 * scale)}px;color:#94a3b8;margin-left:auto">
            ${sev} · ${dev} · ${lookback} · max ${limit}
          </span>
        </div>
        <!-- Mini table -->
        <div style="padding:3px 6px;overflow:hidden">
          <table style="width:100%;border-collapse:collapse">
            <thead><tr>${thCells}</tr></thead>
            <tbody>${sampleHtml}</tbody>
          </table>
          <div style="text-align:center;font-size:${Math.round(7 * scale)}px;color:#94a3b8;padding:2px 0">
            ${search !== '-' ? `search: ${search} · ` : ''}queried at generation time
          </div>
        </div>
      </div>`;
  }

  private renderCanvasPieChart(el: TElement, contentW: number, scale: number): string {
    const h = Math.max(30, Math.round((el.height ?? 200) * scale * 0.352778));
    const cfg = el.pieChartConfig;
    const slices = cfg?.slices?.filter(s => s.value.trim()) ?? [];
    const showLegend = cfg?.showLegend ?? true;

    const cx = contentW / 2;
    const cy = h / 2;
    const r = Math.min(cx, cy) * 0.7;

    let svgSlices = '';
    if (slices.length === 0) {
      svgSlices = `<circle cx="${cx}" cy="${cy}" r="${r}" fill="#e2e8f0"/>`;
    } else {
      const total = slices.length;
      let startAngle = -Math.PI / 2;
      const angleStep = (2 * Math.PI) / total;
      svgSlices = slices.map((slice, i) => {
        const endAngle = startAngle + angleStep;
        const x1 = cx + r * Math.cos(startAngle);
        const y1 = cy + r * Math.sin(startAngle);
        const x2 = cx + r * Math.cos(endAngle);
        const y2 = cy + r * Math.sin(endAngle);
        const largeArc = angleStep > Math.PI ? 1 : 0;
        const color = slice.color || `hsl(${(i * 360 / total)},60%,55%)`;
        const d = `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${largeArc},1 ${x2},${y2} Z`;
        startAngle = endAngle;
        return `<path d="${d}" fill="${color}" stroke="#fff" stroke-width="1"/>`;
      }).join('');
    }

    const legendHtml = showLegend && slices.length > 0
      ? `<div style="display:flex;flex-wrap:wrap;gap:4px 8px;justify-content:center;margin-top:4px">
          ${slices.map(s => `
            <span style="display:flex;align-items:center;gap:3px;font-size:${Math.round(7 * scale)}px;color:#475569">
              <span style="width:8px;height:8px;border-radius:50%;background:${s.color || '#94a3b8'};flex-shrink:0"></span>
              ${this.esc(s.label || s.value)}
            </span>`).join('')}
         </div>`
      : '';

    return `
      <div style="width:${contentW}px;display:flex;flex-direction:column;align-items:center;justify-content:center;
                  background:#f8fafc;border:2px dashed #94a3b8;border-radius:3px;box-sizing:border-box;padding:4px 0">
        <svg width="${contentW}" height="${h}" viewBox="0 0 ${contentW} ${h}">
          ${svgSlices}
        </svg>
        ${legendHtml}
        ${slices.length === 0 ? `<div style="font-size:${Math.round(9 * scale)}px;color:#94a3b8;margin-top:4px">◔ Pie Chart - no slices configured</div>` : ''}
      </div>`;
  }

  // ─── Right dashboard: properties ──────────────────────────────────────────────

  private renderProperties(): string {
    const s = this.state;
    const el = s.selectedEl >= 0 ? s.doc.elements[s.selectedEl] : null;

    const panelBg = 'background:color-mix(in srgb,var(--panel-bg,var(--widget-bg,#0d1117)) 100%,transparent)';

    let content = '';
    if (!el) {
      content = `<div class="text-xs opacity-30 text-center pt-8 px-3">
                   Select an element on the canvas to edit its properties
                 </div>`;
    } else {
      content = this.renderElementProperties(el);
    }

    return `
      <div class="flex flex-col overflow-y-auto shrink-0 border-l"
           style="width:260px;${panelBg};border-color:var(--border-color)">
        <div class="px-3 pt-3">
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <span class="text-xs font-medium opacity-40 uppercase tracking-widest">Properties</span>
              ${el ? `<span class="text-xs px-1.5 py-0.5 rounded font-medium"
                           style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);
                                  color:var(--accent-color)">${el.type}</span>` : ''}
            </div>
            ${el ? `
              <button class="el-delete" data-el="${s.selectedEl}"
                      class="text-xs px-2 py-1 rounded border"
                      style="color:#f87171;border-color:rgba(239,68,68,.3);background:rgba(239,68,68,.1);
                             font-size:11px;padding:2px 8px;border-radius:4px;border-style:solid;cursor:pointer">
                Delete
              </button>` : ''}
          </div>
          ${content}
        </div>
      </div>`;
  }

  private renderElementProperties(el: TElement): string {
    switch (el.type) {
      case 'title':
      case 'table':
        return this.renderGridProperties(el);
      case 'spacer':
        return this.renderSpacerProperties(el);
      case 'footer':
        return this.renderFooterProperties(el);
      case 'image':
        return this.renderImageProperties(el);
      case 'chart':
        return this.renderChartProperties(el);
      case 'events':
        return this.renderEventsProperties(el);
      case 'pie-chart':
        return this.renderPieChartProperties(el);
      default:
        return '';
    }
  }

  private renderGridProperties(el: TElement): string {
    const s = this.state;
    const elIdx = s.selectedEl;
    const rows = el.rows ?? [];
    const numCols = rows.length > 0 ? Math.max(...rows.map(r => r.length), 1) : 1;
    const numRows = rows.length;

    const hasCell = s.selectedCell !== null;
    const cell: CellProps | null = hasCell && rows[s.selectedCell!.row]
      ? (rows[s.selectedCell!.row][s.selectedCell!.col] ?? null)
      : null;

    return `
      <div class="space-y-4 pb-4 text-xs">

        <!-- Table structure -->
        <div>
          <div class="opacity-50 uppercase tracking-widest mb-1.5">
            ${el.type === 'title' ? 'Title' : 'Table'} Structure
          </div>
          <div class="text-xs opacity-40 mb-1">${numRows} row${numRows !== 1 ? 's' : ''}, ${numCols} col${numCols !== 1 ? 's' : ''}</div>
          <div class="grid grid-cols-2 gap-1">
            <button class="grid-add-row prop-btn" data-el="${elIdx}">+ Add Row</button>
            <button class="grid-remove-row prop-btn" data-el="${elIdx}">− Remove Row</button>
            <button class="grid-add-col prop-btn" data-el="${elIdx}">+ Add Column</button>
            <button class="grid-remove-col prop-btn" data-el="${elIdx}">− Remove Column</button>
          </div>
        </div>

        <!-- Borders -->
        <div>
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Default Borders</div>
          <div class="grid grid-cols-2 gap-1">
            <button class="grid-borders-all prop-btn${el.allBorders ? ' prop-btn-active' : ''}" data-el="${elIdx}">All On</button>
            <button class="grid-borders-none prop-btn${el.noBorders ? ' prop-btn-active' : ''}" data-el="${elIdx}">All Off</button>
          </div>
        </div>

        <!-- Column widths -->
        ${Array.from({ length: numCols }, (_, ci) => {
          const w = el.colWidths?.[ci] ?? 1;
          return `
            <div>
              <div class="opacity-50 mb-0.5">Col ${ci + 1} Width (weight)</div>
              <input type="number" class="grid-colwidth prop-input" data-el="${elIdx}" data-col="${ci}"
                     value="${w}" min="0.1" step="0.1">
            </div>`;
        }).join('')}

        <!-- Row heights -->
        ${Array.from({ length: numRows }, (_, ri) => {
          const h = el.rowHeights?.[ri] ?? 1;
          return `
            <div>
              <div class="opacity-50 mb-0.5">Row ${ri + 1} Height (×)</div>
              <input type="number" class="grid-rowheight prop-input" data-el="${elIdx}" data-row="${ri}"
                     value="${h}" min="0.5" step="0.5">
            </div>`;
        }).join('')}

        <!-- Default BG / Text colours -->
        <div>
          <div class="opacity-50 mb-0.5">Default Background</div>
          ${this.renderColorPicker('grid-bg', elIdx, el.bgColor ?? '#ffffff')}
        </div>
        <div>
          <div class="opacity-50 mb-0.5">Default Text Color</div>
          ${this.renderColorPicker('grid-tx', elIdx, el.textColor ?? '#000000')}
        </div>

        <!-- Cell properties (when a cell is selected) -->
        ${hasCell && cell !== null ? `
          <div class="border-t pt-3" style="border-color:var(--border-color)">
            <div class="opacity-50 uppercase tracking-widest mb-2">
              Cell (Row ${s.selectedCell!.row + 1}, Col ${s.selectedCell!.col + 1})
            </div>

            <div class="mb-2">
              <div class="opacity-50 mb-0.5">Text</div>
              <input type="text" class="cell-text prop-input" data-el="${elIdx}"
                     data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}"
                     value="${this.esc(cell.text ?? '')}">
            </div>

            <div class="border-t pt-3 mt-3" style="border-color:var(--border-color)">
              <div class="opacity-50 uppercase tracking-widest mb-1.5">Cell Image</div>
              <input type="file" class="cell-image-file" data-el="${elIdx}"
                     data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}"
                     accept="image/png,image/jpeg,image/gif"
                     style="font-size:11px;width:100%">
              ${cell.imageData ? `
                <div class="mt-2">
                  <img src="${this.esc(cell.imageData)}" style="width:100%;max-height:80px;object-fit:contain;
                       border:1px solid var(--border-color);border-radius:2px">
                </div>
                <div class="grid grid-cols-2 gap-2 mt-2">
                  <div>
                    <div class="opacity-50 mb-0.5">Fit</div>
                    <select class="cell-image-fit prop-select" data-el="${elIdx}"
                            data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}">
                      <option value="contain" ${(cell.imageFit ?? 'contain') === 'contain' ? 'selected' : ''}>Contain</option>
                      <option value="stretch" ${cell.imageFit === 'stretch' ? 'selected' : ''}>Stretch</option>
                    </select>
                  </div>
                  <div>
                    <div class="opacity-50 mb-0.5">Padding (pt)</div>
                    <input type="number" class="cell-image-padding prop-input" data-el="${elIdx}"
                           data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}"
                           value="${cell.imagePadding ?? 2}" min="0" max="72" step="1">
                  </div>
                </div>
                <button class="cell-image-remove prop-btn mt-2" data-el="${elIdx}"
                        data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}">Remove Image</button>
              ` : `
                <div class="text-xs opacity-30 mt-1">When set, the image is rendered instead of cell text.</div>
              `}
            </div>

            ${this.renderFontControls(cell, 'cell', elIdx, s.selectedCell!.row, s.selectedCell!.col)}
            ${this.renderAlignControls(cell.align, 'cell', elIdx, s.selectedCell!.row, s.selectedCell!.col)}
            ${this.renderBorderControls(cell.borders, 'cell', elIdx, s.selectedCell!.row, s.selectedCell!.col)}

            <div class="mt-2">
              <div class="opacity-50 mb-0.5">Cell Background (override)</div>
              ${this.renderColorPicker('cell-bg', elIdx, cell.bgColor ?? '', s.selectedCell!.row, s.selectedCell!.col)}
            </div>
            <div class="mt-2">
              <div class="opacity-50 mb-0.5">Cell Text Color (override)</div>
              ${this.renderColorPicker('cell-tx', elIdx, cell.textColor ?? '', s.selectedCell!.row, s.selectedCell!.col)}
            </div>

            <label class="flex items-center gap-2 mt-2 cursor-pointer select-none">
              <input type="checkbox" class="cell-wrap" data-el="${elIdx}"
                     data-row="${s.selectedCell!.row}" data-col="${s.selectedCell!.col}"
                     ${cell.wrap ? 'checked' : ''}>
              <span class="text-xs opacity-60">Wrap text</span>
            </label>
          </div>
        ` : `
          <div class="text-xs opacity-30 mt-1">Click a cell to edit its properties.</div>
        `}
      </div>`;
  }

  private renderSpacerProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    return `
      <div class="space-y-3 pb-4 text-xs">
        <div>
          <div class="opacity-50 mb-0.5">Height (pt)</div>
          <input type="number" class="spacer-height prop-input" data-el="${elIdx}"
                 value="${el.height ?? 36}" min="4" max="500">
        </div>
      </div>`;
  }

  private renderFooterProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    return `
      <div class="space-y-3 pb-4 text-xs">
        <div>
          <div class="opacity-50 mb-0.5">Text</div>
          <input type="text" class="footer-text prop-input" data-el="${elIdx}"
                 value="${this.esc(el.text ?? '')}">
        </div>
        ${this.renderFontControls(el, 'footer', elIdx)}
        ${this.renderAlignControls(el.align, 'footer', elIdx)}
        ${this.renderBorderControls(el.footerBorders, 'footer', elIdx)}
        <div>
          <div class="opacity-50 mb-0.5">Text Color</div>
          ${this.renderColorPicker('footer-tx', elIdx, el.textColor2 ?? '#000000')}
        </div>
      </div>`;
  }

  private renderImageProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    return `
      <div class="space-y-3 pb-4 text-xs">
        <div>
          <div class="opacity-50 mb-0.5">Height (pt)</div>
          <input type="number" class="image-height" data-el="${elIdx}"
                 value="${el.height ?? 144}" min="18"
                 class="prop-input">
        </div>
        <div>
          <div class="opacity-50 mb-0.5">Width (pt, 0 = full width)</div>
          <input type="number" class="image-width" data-el="${elIdx}"
                 value="${el.width ?? 0}" min="0"
                 class="prop-input">
        </div>
        <div>
          <div class="opacity-50 mb-0.5">Image File</div>
          <input type="file" class="image-file" data-el="${elIdx}"
                 accept="image/png,image/jpeg,image/gif"
                 style="font-size:11px;width:100%">
        </div>
        ${el.imageData ? `
          <img src="${el.imageData}" style="width:100%;max-height:80px;object-fit:contain;
               border:1px solid var(--border-color);border-radius:2px">
        ` : ''}
      </div>`;
  }

  private renderChartProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    const cfg: ChartConfig = el.chartConfig ?? { metrics: [], lookback: '24h' };
    const colors = cfg.colors ?? [];
    const metrics = cfg.metrics ?? [];
    return `
      <div class="space-y-3 pb-4 text-xs">

        <div>
          <div class="opacity-50 mb-0.5">Height (pt)</div>
          <input type="number" class="chart-height" data-el="${elIdx}"
                 value="${el.height ?? 200}" min="40"
                 class="prop-input">
        </div>

        <!-- Data source -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Data Source</div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Lookback</div>
            <select class="chart-lookback prop-select" data-el="${elIdx}">
              ${['1h','6h','24h','7d','30d'].map(v =>
                `<option value="${v}" ${(cfg.lookback || '24h') === v ? 'selected' : ''}>${v}</option>`
              ).join('')}
            </select>
          </div>

          <div>
            <div class="opacity-50 mb-0.5">Tag paths</div>
            <div class="opacity-40 mb-1.5" style="font-size:10px;line-height:1.4">
              Full path below org, e.g. <code>sensors.pump1.flow</code>.
              Use <code>&#123;&#123;device&#125;&#125;</code> for a custom variable.
            </div>
            <div id="chart-metrics-list" class="space-y-1 mb-1" data-el="${elIdx}">
              ${metrics.map((m, i) => `
                <div class="flex items-center gap-1" data-metric-idx="${i}">
                  <input type="text" class="chart-metric-name prop-input prop-flex-1 prop-font-mono prop-text-10" data-el="${elIdx}" data-idx="${i}"
                         value="${this.esc(m)}" placeholder="device.tag"
                         >
                  <button class="chart-metric-pick prop-btn-icon" data-el="${elIdx}" data-idx="${i}"
                          title="Browse tag tree"
                          >⋯</button>
                  <button class="chart-metric-remove prop-btn-remove" data-el="${elIdx}" data-idx="${i}">✕</button>
                </div>`).join('')}
            </div>
            <button class="chart-metric-add prop-btn" data-el="${elIdx}">+ Add Tag</button>
          </div>
        </div>

        <!-- Appearance -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Appearance</div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Chart Title</div>
            <input type="text" class="chart-title prop-input" data-el="${elIdx}"
                   value="${this.esc(cfg.title ?? '')}"
                   placeholder="Optional">
          </div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Y-Axis Label</div>
            <input type="text" class="chart-ylabel prop-input" data-el="${elIdx}"
                   value="${this.esc(cfg.yLabel ?? '')}"
                   placeholder="Optional">
          </div>

          <div class="grid grid-cols-2 gap-2 mb-2">
            <div>
              <div class="opacity-50 mb-0.5">Y Min</div>
              <input type="number" class="chart-ymin prop-input" data-el="${elIdx}"
                     value="${cfg.yMin ?? ''}" placeholder="auto"
                     >
            </div>
            <div>
              <div class="opacity-50 mb-0.5">Y Max</div>
              <input type="number" class="chart-ymax prop-input" data-el="${elIdx}"
                     value="${cfg.yMax ?? ''}" placeholder="auto"
                     >
            </div>
          </div>

          <div class="flex flex-col gap-1.5 mb-2">
            <label class="flex items-center gap-2 cursor-pointer select-none">
              <input type="checkbox" class="chart-legend" data-el="${elIdx}" ${cfg.showLegend ? 'checked' : ''}>
              <span>Show legend</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer select-none">
              <input type="checkbox" class="chart-smooth" data-el="${elIdx}" ${cfg.smooth ? 'checked' : ''}>
              <span>Smooth lines</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer select-none">
              <input type="checkbox" class="chart-fill" data-el="${elIdx}" ${cfg.fillArea ? 'checked' : ''}>
              <span>Fill area</span>
            </label>
          </div>

          <!-- Series colours -->
          <div>
            <div class="opacity-50 uppercase tracking-widest mb-1">Series Colours</div>
            <div id="chart-colors-list" class="space-y-1" data-el="${elIdx}">
              ${colors.map((c, i) => `
                <div class="flex items-center gap-1.5" data-color-idx="${i}">
                  <input type="color" class="chart-color-picker prop-color-picker" data-el="${elIdx}" data-idx="${i}"
                         value="${c}"
                         >
                  <input type="text" class="chart-color-hex prop-input prop-flex-1" data-el="${elIdx}" data-idx="${i}"
                         value="${c}" maxlength="7"
                         >
                  <button class="chart-color-remove prop-btn-remove" data-el="${elIdx}" data-idx="${i}">✕</button>
                </div>`).join('')}
            </div>
            <button class="chart-color-add prop-btn prop-mt-6" data-el="${elIdx}">+ Add Colour</button>
          </div>
        </div>
      </div>`;
  }

  private renderEventsProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    const cfg: EventsConfig = el.eventsConfig ?? {
      lookback: '24h', limit: 50,
      columns: ['timestamp', 'severity', 'device', 'message'],
    };
    const allCols: [string, string][] = [
      ['timestamp', 'Timestamp'], ['severity', 'Severity'], ['user', 'User'],
      ['device', 'Device'], ['message', 'Message'], ['params', 'Parameters'],
    ];
    return `
      <div class="space-y-3 pb-4 text-xs">
        <!-- Filters -->
        <div>
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Filters</div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Severity</div>
            <select class="evt-severity prop-select" data-el="${elIdx}">
              ${['', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL'].map(s =>
                `<option value="${s}" ${(cfg.severity ?? '') === s ? 'selected' : ''}>${s || 'All'}</option>`
              ).join('')}
            </select>
          </div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Device</div>
            <input type="text" class="evt-device prop-input" data-el="${elIdx}"
                   value="${this.esc(cfg.device ?? '')}" placeholder="All devices"
                   >
          </div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Search</div>
            <input type="text" class="evt-search prop-input" data-el="${elIdx}"
                   value="${this.esc(cfg.search ?? '')}" placeholder="Message contains…"
                   >
          </div>
        </div>

        <!-- Time & limits -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Time Window</div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Lookback</div>
            <select class="evt-lookback prop-select" data-el="${elIdx}">
              ${['1h', '3h', '6h', '12h', '24h', '7d', '30d'].map(v =>
                `<option value="${v}" ${(cfg.lookback || '24h') === v ? 'selected' : ''}>${v}</option>`
              ).join('')}
            </select>
          </div>

          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Max Rows</div>
            <input type="number" class="evt-limit prop-input" data-el="${elIdx}"
                   value="${cfg.limit ?? 50}" min="1" max="500"
                   >
          </div>
        </div>

        <!-- Columns -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Columns</div>
          <div class="space-y-1.5">
            ${allCols.map(([key, label]) => `
              <label class="flex items-center gap-2 cursor-pointer select-none">
                <input type="checkbox" class="evt-col" data-el="${elIdx}" data-col-key="${key}"
                       ${cfg.columns.includes(key) ? 'checked' : ''}>
                <span>${label}</span>
              </label>
            `).join('')}
          </div>
        </div>

        <!-- Table style -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Table Style</div>
          <div class="mb-2">
            <div class="opacity-50 mb-0.5">Font Size (pt)</div>
            <select class="evt-font-size prop-select" data-el="${elIdx}">
              ${[7, 8, 9, 10].map(sz =>
                `<option value="${sz}" ${(el.size ?? 8) === sz ? 'selected' : ''}>${sz}</option>`
              ).join('')}
            </select>
          </div>
          <div class="grid grid-cols-2 gap-2">
            <div>
              <div class="opacity-50 mb-0.5">Header BG</div>
              ${this.renderColorPicker('evt-hdr-bg', elIdx, el.bgColor || '#1e3a5f')}
            </div>
            <div>
              <div class="opacity-50 mb-0.5">Header Text</div>
              ${this.renderColorPicker('evt-hdr-tx', elIdx, el.textColor || '#ffffff')}
            </div>
          </div>
        </div>
      </div>`;
  }

  // ─── Properties helpers ───────────────────────────────────────────────────

  private renderFontControls(
    src: { font?: string; size?: number; bold?: boolean; italic?: boolean; underline?: boolean },
    prefix: string, elIdx: number, row?: number, col?: number,
  ): string {
    const dc = `data-el="${elIdx}"${row !== undefined ? ` data-row="${row}" data-col="${col}"` : ''}`;
    return `
        <div class="mt-2">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Font</div>
          <div class="flex gap-1.5 mb-1.5">
          <select class="${prefix}-font-family prop-select prop-flex-1" ${dc}>
            ${['Helvetica','Times','Courier'].map(f =>
              `<option value="${f}" ${(src.font ?? 'Helvetica') === f ? 'selected' : ''}>${f}</option>`
            ).join('')}
          </select>
          <select class="${prefix}-font-size prop-select prop-w-68" ${dc}>
            ${[6,8,9,10,11,12,14,16,18,20,24,28,32,36,40].map(sz =>
              `<option value="${sz}" ${(src.size ?? 10) === sz ? 'selected' : ''}>${sz}px</option>`
            ).join('')}
          </select>
        </div>
        <div class="flex gap-1">
          ${([
            ['B', `${prefix}-bold`, src.bold ?? false, 'font-bold'],
            ['I', `${prefix}-italic`, src.italic ?? false, 'italic'],
            ['U', `${prefix}-underline`, src.underline ?? false, 'underline'],
          ] as Array<[string, string, boolean, string]>).map(([lbl, cls, active, textClass]) => `
            <button class="${cls} prop-btn prop-btn-toggle ${active ? 'prop-btn-active' : ''} flex-1 font-medium"
                    ${dc}
                    >
              <span class="${textClass}">${lbl}</span>
            </button>
          `).join('')}
        </div>
      </div>`;
  }

  private renderAlignControls(
    align: string | undefined, prefix: string, elIdx: number, row?: number, col?: number,
  ): string {
    const dc = `data-el="${elIdx}"${row !== undefined ? ` data-row="${row}" data-col="${col}"` : ''}`;
    const cur = align ?? 'L';
    return `
      <div class="mt-2">
        <div class="opacity-50 uppercase tracking-widest mb-1.5">Alignment</div>
        <div class="flex gap-1">
          ${[['L','← Left'],['C','↓ Center'],['R','→ Right']].map(([v, lbl]) => `
            <button class="${prefix}-align prop-btn prop-btn-toggle ${cur === v ? 'prop-btn-active' : ''} flex-1 prop-text-10"
                    data-align="${v}" ${dc}
                    >
              ${lbl}
            </button>
          `).join('')}
        </div>
      </div>`;
  }

  private renderBorderControls(
    b: BorderProps | undefined, prefix: string, elIdx: number, row?: number, col?: number,
  ): string {
    const dc = `data-el="${elIdx}"${row !== undefined ? ` data-row="${row}" data-col="${col}"` : ''}`;
    const bv = b ?? { left: 0, right: 0, top: 0, bottom: 0 };
    return `
      <div class="mt-2">
        <div class="opacity-50 uppercase tracking-widest mb-1.5">Borders</div>
        <div class="grid grid-cols-2 gap-x-2 gap-y-1.5">
          ${(['left','right','top','bottom'] as (keyof BorderProps)[]).map(side => `
            <div>
              <div class="opacity-50 mb-0.5">${side.charAt(0).toUpperCase()+side.slice(1)}</div>
              <div class="flex items-center gap-1">
                <button class="${prefix}-border-dec prop-btn-sm" data-side="${side}" ${dc}>−</button>
                <span class="text-center font-mono prop-text-11 prop-w-28">${bv[side]}px</span>
                <button class="${prefix}-border-inc prop-btn-sm" data-side="${side}" ${dc}>+</button>
              </div>
            </div>
          `).join('')}
        </div>
        <div class="flex gap-1 mt-1.5">
          ${[['none','None'],['all','All'],['box','Box'],['bottom','Bottom']].map(([v, lbl]) => `
            <button class="${prefix}-border-qs prop-btn prop-btn-toggle prop-py-0_5 flex-1 prop-text-10"
                    data-qs="${v}" ${dc}
                    >
              ${lbl}
            </button>
          `).join('')}
        </div>
      </div>`;
  }

  private renderColorPicker(
    cls: string, elIdx: number, value: string, row?: number, col?: number,
  ): string {
    const dc = `data-el="${elIdx}"${row !== undefined ? ` data-row="${row}" data-col="${col}"` : ''}`;
    const swatches = ['#ffffff','#000000','#1e3a5f','#2d5a8e','#4a9eff','#e8f4fd',
                      '#f5f5f5','#e0e0e0','#ffd700','#ff6b6b','#4ecdc4','#2d8a2d'];
    const safeVal = value || '#ffffff';
    return `
      <div class="flex flex-col gap-1.5">
        <div class="flex items-center gap-1.5">
          <input type="color" class="${cls}-picker prop-color-picker prop-color-picker-lg" ${dc} value="${safeVal}">
          <input type="text" class="${cls}-hex prop-input prop-flex-1" ${dc} value="${value || ''}"
                 placeholder="${value ? '' : 'inherit'}" maxlength="7"
                 >
          ${value ? `<button class="${cls}-clear prop-btn-clear" ${dc}>✕</button>` : ''}
        </div>
        <div class="flex flex-wrap gap-1">
          ${swatches.map(sw => `
            <button class="${cls}-swatch prop-color-swatch" data-swatch="${sw}" ${dc}
                    title="${sw}"
                    style="background:${sw}">
            </button>
          `).join('')}
        </div>
      </div>`;
  }

  // ─── Variables tab ────────────────────────────────────────────────────────

  private renderVariablesTab(): string {
    const vars = this.state.variables;
    return `
      <div class="flex flex-col h-full overflow-hidden">
        <div class="flex-1 overflow-auto p-4">
          <div class="text-xs opacity-40 mb-3">
            Use <code class="px-1 rounded" style="background:color-mix(in srgb,var(--accent-color) 10%,transparent)">&#123;&#123;name&#125;&#125;</code>
            in any text field to insert a variable value.
          </div>
          <div id="variables-list" class="space-y-2">
            ${vars.map((v, i) => this.renderVariableRow(v, i)).join('')}
          </div>
          ${vars.length === 0 ? `<div class="text-xs opacity-30 text-center py-6">No variables yet.</div>` : ''}
        </div>
        <div class="px-4 py-3 border-t shrink-0" style="border-color:var(--border-color)">
          <button id="btn-add-variable"
                  class="w-full text-xs py-1.5 rounded border transition-colors"
                  style="border-color:color-mix(in srgb,var(--accent-color) 30%,transparent);
                         color:var(--accent-color)">
            + Add Variable
          </button>
        </div>
      </div>`;
  }

  private renderVariableRow(v: PDFVariable, i: number): string {
    return `
      <div class="rounded border p-2.5 space-y-1.5 text-xs"
           style="border-color:var(--border-color)">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <input type="text" class="var-name prop-input prop-w-120 prop-font-mono" data-idx="${i}" value="${this.esc(v.name)}"
                   placeholder="variable_name"
                   >
            <span class="opacity-30">=</span>
            <select class="var-type prop-select" data-idx="${i}">
              <option value="builtin" ${v.type==='builtin'?'selected':''}>Built-in</option>
              <option value="rtdb" ${v.type==='rtdb'?'selected':''}>RTDB Tag</option>
              <option value="sql" ${v.type==='sql'?'selected':''}>SQL Query</option>
              <option value="custom" ${v.type==='custom'?'selected':''}>Custom (parameter)</option>
            </select>
          </div>
          <button class="btn-remove-var" data-idx="${i}"
                  style="opacity:.4;cursor:pointer;background:none;border:none;color:inherit;
                         font-size:14px;padding:0 2px">✕</button>
        </div>
        ${v.type === 'builtin' ? `
          <div class="flex items-center gap-2">
            <span class="opacity-40 w-14 shrink-0">Source</span>
            <select class="var-source prop-select prop-flex-1" data-idx="${i}">
              <option value="now" ${v.source==='now'?'selected':''}>now (current date/time)</option>
              <option value="org_name" ${v.source==='org_name'?'selected':''}>org_name (display name)</option>
              <option value="org_slug" ${v.source==='org_slug'?'selected':''}>org_slug</option>
              <option value="report_name" ${v.source==='report_name'?'selected':''}>report_name</option>
              <option value="page_no" ${v.source==='page_no'?'selected':''}>page_no (current page)</option>
              <option value="page_count" ${v.source==='page_count'?'selected':''}>page_count (total pages)</option>
            </select>
          </div>
          ${v.source === 'now' ? `
            <div class="flex items-center gap-2">
              <span class="opacity-40 w-14 shrink-0">Format</span>
            <input type="text" class="var-format prop-input prop-flex-1" data-idx="${i}" value="${this.esc(v.format ?? '2006-01-02')}"
                     placeholder="e.g. 2006-01-02"
                     >
            </div>
          ` : ''}
        ` : ''}
        ${v.type === 'rtdb' ? `
          <div class="flex items-center gap-2">
            <span class="opacity-40 w-14 shrink-0">Tag path</span>
            <input type="text" class="var-path prop-input prop-flex-1 prop-font-mono" data-idx="${i}" value="${this.esc(v.path ?? '')}"
                   placeholder="e.g. pump1.flow_rate"
                   >
          </div>
        ` : ''}
        ${v.type === 'sql' ? `
          <div>
            <div class="opacity-40 mb-0.5">SQL Query ($1 = org name, returns one value)</div>
            <textarea class="var-query prop-input prop-textarea prop-font-mono prop-min-h-60" data-idx="${i}" rows="3"
                      placeholder="SELECT COUNT(*) FROM events WHERE org_name=$1 AND ..."
                      >${this.esc(v.query ?? '')}</textarea>
          </div>
        ` : ''}
        ${v.type === 'custom' ? `
          <div class="flex items-center gap-2">
            <span class="opacity-40 w-14 shrink-0">Label</span>
            <input type="text" class="var-label prop-input prop-flex-1" data-idx="${i}" value="${this.esc(v.label ?? '')}"
                   placeholder="Human-readable label"
                   >
          </div>
          <div class="flex items-center gap-2">
            <span class="opacity-40 w-14 shrink-0">Input</span>
            <select class="var-input-type prop-select prop-flex-1" data-idx="${i}">
              <option value="text"     ${(!v.inputType||v.inputType==='text')    ?'selected':''}>Text</option>
              <option value="date"     ${v.inputType==='date'                    ?'selected':''}>Date</option>
              <option value="datetime" ${v.inputType==='datetime'                ?'selected':''}>Date &amp; Time</option>
              <option value="number"   ${v.inputType==='number'                  ?'selected':''}>Number</option>
            </select>
          </div>
          <div class="flex items-center gap-2">
            <span class="opacity-40 w-14 shrink-0">Default</span>
            <input type="${v.inputType === 'date' ? 'date' : v.inputType === 'datetime' ? 'datetime-local' : v.inputType === 'number' ? 'number' : 'text'}"
                   class="var-default-value prop-input prop-flex-1" data-idx="${i}" value="${this.esc(v.defaultValue ?? '')}"
                   placeholder="Optional default"
                   >
          </div>
        ` : ''}
      </div>`;
  }

  // ─── JSON tab ─────────────────────────────────────────────────────────────

  private renderJSONTab(): string {
    return `
      <div class="flex flex-col h-full overflow-hidden p-3">
        <div class="text-xs opacity-40 mb-2">
          Raw template JSON - paste either a document ({ config, elements }) or a saved template ({ templateJson, variables }).
        </div>
        <json-editor id="json-editor" style="flex:1;min-height:0"></json-editor>
      </div>`;
  }

  private parseTemplateJSONInput(raw: string): { doc: TDoc; variables?: PDFVariable[]; name?: string; description?: string } {
    const parsed = JSON.parse(raw);
    const wrapper = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, any> : null;
    const candidate = wrapper?.templateJson ?? wrapper?.templateJSON ?? parsed;
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) {
      throw new Error('Expected a report document object');
    }
    if (!candidate.config || !Array.isArray(candidate.elements)) {
      throw new Error('Expected { config, elements } or { templateJson, variables }');
    }

    const cfg = candidate.config ?? {};
    const margins = cfg.margins ?? {};
    const doc: TDoc = {
      ...candidate,
      config: {
        pageSize: cfg.pageSize || 'A4',
        orientation: cfg.orientation || 'P',
        documentTitle: cfg.documentTitle || '{{report_name}}',
        watermark: cfg.watermark || '',
        margins: {
          left: Number(margins.left ?? 72),
          top: Number(margins.top ?? 72),
          right: Number(margins.right ?? 72),
          bottom: Number(margins.bottom ?? 72),
        },
      },
      elements: candidate.elements,
    };

    return {
      doc,
      variables: Array.isArray(wrapper?.variables) ? structuredClone(wrapper.variables) : undefined,
      name: typeof wrapper?.name === 'string' ? wrapper.name : undefined,
      description: typeof wrapper?.description === 'string' ? wrapper.description : undefined,
    };
  }

  private applyTemplateJSONInput(raw: string): void {
    const parsed = this.parseTemplateJSONInput(raw);
    const s = this.state;
    s.doc = parsed.doc;
    if (parsed.variables) {
      s.variables = parsed.variables;
      this.syncPreviewValues();
    }
    if (parsed.name) s.templateName = parsed.name;
    if (parsed.description !== undefined) s.templateDesc = parsed.description;
    s.dirty = true;
  }

  // ─── Preview tab ──────────────────────────────────────────────────────────

  private renderPreviewTab(): string {
    const s = this.state;
    const zoomPct = Math.round(s.previewZoom * 100);
    const customVars = s.variables.filter(v => v.type === 'custom');
    const htmlIT = (t?: string) =>
      t === 'date' ? 'date' : t === 'datetime' ? 'datetime-local' : t === 'number' ? 'number' : 'text';
    return `
      <div class="flex flex-col h-full overflow-hidden">

        <!-- Toolbar -->
        <div class="flex items-center gap-3 px-4 py-2.5 border-b shrink-0"
             style="border-color:var(--border-color)">
          <span class="text-xs opacity-40">
            ${s.isNew ? 'Save the template first to preview it.' : s.dirty ? 'Refresh Preview saves unsaved changes first.' : 'Preview is up to date with the saved template.'}
          </span>
          <div class="flex items-center gap-1 ml-auto">
            ${s.previewUrl ? `
              <button id="btn-zoom-out" title="Zoom out"
                      class="text-xs px-2 py-1 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)"
                      ${s.previewZoom <= 0.25 ? 'disabled' : ''}>−</button>
              <span id="zoom-label" class="text-xs w-12 text-center tabular-nums" style="color:var(--text-color)">${zoomPct}%</span>
              <button id="btn-zoom-in" title="Zoom in"
                      class="text-xs px-2 py-1 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)"
                      ${s.previewZoom >= 3 ? 'disabled' : ''}>+</button>
              <button id="btn-zoom-reset" title="Reset zoom"
                      class="text-xs px-2 py-1 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--text-color)">1:1</button>
              <div class="w-px h-4 mx-1" style="background:var(--border-color)"></div>
            ` : ''}
            ${!s.isNew ? `
              <button id="btn-refresh-preview"
                      class="text-xs px-3 py-1.5 rounded border transition-colors"
                      style="border-color:color-mix(in srgb,var(--accent-color) 30%,transparent);color:var(--accent-color)"
                      ${s.previewLoading ? 'disabled' : ''}>
                ${s.previewLoading ? 'Loading…' : s.dirty ? 'Save & Preview' : 'Refresh Preview'}
              </button>
            ` : ''}
          </div>
        </div>

        <!-- Parameters panel (custom variables only) -->
        ${customVars.length > 0 ? `
        <div class="px-4 py-3 border-b shrink-0 text-xs"
             style="border-color:var(--border-color);background:color-mix(in srgb,var(--widget-bg) 100%,transparent)">
          <div class="opacity-50 uppercase tracking-widest mb-2">Parameters</div>
          <div class="grid gap-x-4 gap-y-2" style="grid-template-columns:repeat(auto-fill,minmax(200px,1fr))">
            ${customVars.map(v => `
              <div>
                <div class="opacity-50 mb-0.5">${this.esc(v.label || v.name)}</div>
                <input class="preview-param w-full" type="${htmlIT(v.inputType)}"
                       data-name="${this.esc(v.name)}"
                       value="${this.esc(s.previewValues[v.name] ?? v.defaultValue ?? '')}"
                       placeholder="${this.esc(v.defaultValue ?? '')}"
                       >
              </div>`).join('')}
          </div>
        </div>
        ` : ''}

        <!-- PDF content -->
        <div class="flex-1 overflow-auto" style="background:color-mix(in srgb,var(--bg-color) 60%,#888)">
          ${s.previewError ? `<div class="p-4 text-xs" style="color:#f87171">${this.esc(s.previewError)}</div>` : ''}
          ${s.previewUrl ? `
            <div style="transform-origin:top center;transform:scale(${s.previewZoom});width:${100/s.previewZoom}%;min-height:${100/s.previewZoom}%;">
              <iframe src="${s.previewUrl}" style="width:100%;height:100vh;min-height:800px;border:none;display:block"></iframe>
            </div>` : ''}
          ${!s.previewUrl && !s.previewError && !s.isNew ?
            `<div class="flex items-center justify-center h-full text-xs opacity-30">
              Click "Refresh Preview" to render the PDF.
            </div>` : ''}
        </div>
      </div>`;
  }

  // ─── Chart SQL query modal ────────────────────────────────────────────────


  // ─── Post-render wiring (json editor value) ────────────────────────────────

  private postRenderEditor(): void {
    if (this.state.activeTab === 'json') {
      const je = this.querySelector('#json-editor') as any;
      if (je) {
        je.setValue(JSON.stringify(this.state.doc, null, 2));
        je.refresh();
      }
    }
  }

  // ─── Event listeners ─────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    const s = this.state;

    if (s.view === 'list') {
      if (this.canManage) this.on('#btn-new-template', 'click', () => this.openNewTemplate());
      this.onAll('.btn-edit-template', 'click', (e) => {
        const id = (e.target as HTMLElement).closest('[data-id]')?.getAttribute('data-id') ?? '';
        const t = s.templates.find(t => t.id === id);
        if (t) this.openTemplate(t);
      });
      this.onAll('.btn-download-template', 'click', async (e) => {
        const id = (e.target as HTMLElement).closest('[data-id]')?.getAttribute('data-id') ?? '';
        const t = s.templates.find(t => t.id === id);
        if (t) await this.downloadPDF(t.id, t.name);
      });
      if (this.canManage) this.onAll('.btn-delete-template', 'click', (e) => {
        const el = (e.target as HTMLElement).closest('[data-id]');
        s.deleteConfirm = el?.getAttribute('data-id') ?? null;
        this.rerender();
      });
      if (this.canManage) {
        this.on('#btn-delete-cancel', 'click', () => { s.deleteConfirm = null; this.rerender(); });
        this.on('#btn-delete-confirm', 'click', async () => {
          if (s.deleteConfirm) await this.deleteTemplate(s.deleteConfirm);
        });
      }
      return;
    }

    // ── Editor view ──

    this.on('#btn-back-to-list', 'click', () => this.closeEditor());
    if (this.canManage) this.on('#btn-save-template', 'click', () => {
      if (!s.dirty || s.saving) return;
      void this.saveTemplate();
    });
    this.on('#btn-download-current', 'click', () => {
      if (s.editing) this.downloadPDF(s.editing.id, s.editing.name);
    });
    this.on('#editor-name', 'change', (e) => {
      s.templateName = (e.target as HTMLInputElement).value.trim();
      s.dirty = true;
      this.syncDirtyControls();
    });
    this.on('#editor-name', 'input', (e) => {
      s.templateName = (e.target as HTMLInputElement).value.trim();
      s.dirty = true;
      this.syncDirtyControls();
    });
    this.on('#editor-desc', 'change', (e) => {
      s.templateDesc = (e.target as HTMLInputElement).value;
      s.dirty = true;
      this.syncDirtyControls();
    });
    this.on('#editor-desc', 'input', (e) => {
      s.templateDesc = (e.target as HTMLInputElement).value;
      s.dirty = true;
      this.syncDirtyControls();
    });

    // Tab switching
    this.onAll('.tab-btn', 'click', (e) => {
      const tab = (e.target as HTMLElement).getAttribute('data-tab') as EditorTab;
      if (tab === 'json' && s.activeTab !== 'json') {
        // Snapshot current doc to JSON editor
        s.activeTab = 'json';
        this.rerender();
        setTimeout(() => {
          const je = this.querySelector('#json-editor') as any;
          if (je) { je.setValue(JSON.stringify(s.doc, null, 2)); je.refresh(); }
        }, 50);
      } else if (this.canManage && s.activeTab === 'json' && tab !== 'json') {
        // Try to parse JSON editor back to doc
        const je = this.querySelector('#json-editor') as any;
        if (je) {
          try {
            this.applyTemplateJSONInput(je.getValue());
          } catch (err: any) {
            void showAlert(`Invalid report template JSON: ${err?.message ?? 'Unable to parse JSON'}`, {
              title: 'Invalid JSON',
              tone: 'danger',
            });
            return;
          }
        }
        s.activeTab = tab;
        s.selectedEl = -1;
        s.selectedCell = null;
        this.rerender();
      } else {
        s.activeTab = tab;
        s.selectedEl = -1;
        s.selectedCell = null;
        this.rerender();
      }
    });

    if (!this.canManage) {
      if (s.activeTab === 'preview') {
        this.onAll('.preview-param', 'input', (e) => {
          const name = (e.target as HTMLInputElement).getAttribute('data-name') ?? '';
          if (name) s.previewValues[name] = (e.target as HTMLInputElement).value;
        });
        this.on('#btn-refresh-preview', 'click', () => this.loadPreview());
        this.on('#btn-zoom-out', 'click', () => {
          s.previewZoom = Math.max(0.25, Math.round((s.previewZoom - 0.1) * 10) / 10);
          this.rerender();
        });
        this.on('#btn-zoom-in', 'click', () => {
          s.previewZoom = Math.min(3, Math.round((s.previewZoom + 0.1) * 10) / 10);
          this.rerender();
        });
        this.on('#btn-zoom-reset', 'click', () => {
          s.previewZoom = 1.0;
          this.rerender();
        });
      }
      return;
    }

    // Editor canvas zoom (always wired when in editor view)
    this.on('#btn-editor-zoom-out', 'click', () => {
      s.editorZoom = Math.max(0.25, Math.round((s.editorZoom - 0.1) * 10) / 10);
      this.rerender();
    });
    this.on('#btn-editor-zoom-in', 'click', () => {
      s.editorZoom = Math.min(3, Math.round((s.editorZoom + 0.1) * 10) / 10);
      this.rerender();
    });
    this.on('#btn-editor-zoom-reset', 'click', () => {
      s.editorZoom = 1.0;
      this.rerender();
    });

    if (s.activeTab !== 'editor') {
      this.attachTabListeners();
      return;
    }

    // ── WYSIWYG editor listeners ──

    // Document config
    this.on('#cfg-pagesize', 'change', (e) => {
      s.doc.config.pageSize = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.on('#cfg-orientation', 'change', (e) => {
      s.doc.config.orientation = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.on('#cfg-doctitle', 'change', (e) => {
      s.doc.config.documentTitle = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.on('#cfg-watermark', 'change', (e) => {
      s.doc.config.watermark = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.on('#cfg-ml', 'change', (e) => { s.doc.config.margins.left = +((e.target as HTMLInputElement).value) || 0; s.dirty = true; this.rerender(); });
    this.on('#cfg-mr', 'change', (e) => { s.doc.config.margins.right = +((e.target as HTMLInputElement).value) || 0; s.dirty = true; this.rerender(); });
    this.on('#cfg-mt', 'change', (e) => { s.doc.config.margins.top = +((e.target as HTMLInputElement).value) || 0; s.dirty = true; this.rerender(); });
    this.on('#cfg-mb', 'change', (e) => { s.doc.config.margins.bottom = +((e.target as HTMLInputElement).value) || 0; s.dirty = true; this.rerender(); });
    this.onAll('.cfg-margin-preset', 'click', (e) => {
      const v = +(((e.target as HTMLElement).closest('[data-preset]'))?.getAttribute('data-preset') ?? 72);
      s.doc.config.margins = { left: v, right: v, top: v, bottom: v }; s.dirty = true; this.rerender();
    });

    // Palette drag - stop mousedown propagation so GridStack doesn't
    // interpret it as a widget-move and drag the entire widget.
    this.onAll('.palette-item', 'mousedown', (e) => { e.stopPropagation(); });
    this.onAll('.palette-item', 'dragstart', (e) => {
      const type = (e.target as HTMLElement).closest('[data-palette-type]')?.getAttribute('data-palette-type') ?? '';
      (e as DragEvent).dataTransfer?.setData('text/palette-type', type);
    });

    // Canvas drop zones
    this.onAll('.canvas-drop-zone, .canvas-drop-final', 'dragover', (e) => {
      (e as DragEvent).preventDefault();
      (e.target as HTMLElement).style.background = 'var(--accent-color)';
      (e.target as HTMLElement).style.opacity = '.4';
    });
    this.onAll('.canvas-drop-zone, .canvas-drop-final', 'dragleave', (e) => {
      (e.target as HTMLElement).style.background = '';
      (e.target as HTMLElement).style.opacity = '';
    });
    this.onAll('.canvas-drop-zone, .canvas-drop-final', 'drop', (e) => {
      (e as DragEvent).preventDefault();
      (e.target as HTMLElement).style.background = '';
      (e.target as HTMLElement).style.opacity = '';
      const type = (e as DragEvent).dataTransfer?.getData('text/palette-type');
      if (!type) return;
      const zone = (e.target as HTMLElement).closest('[data-drop-index]');
      const dropIdx = +(zone?.getAttribute('data-drop-index') ?? s.doc.elements.length);
      this.insertElement(type as TElement['type'], dropIdx);
    });

    // Element selection (click on canvas element background, not cells)
    this.onAll('.canvas-element', 'click', (e) => {
      const el = (e.target as HTMLElement).closest('.canvas-element');
      if (!el) return;
      // If click was on a cell, handled below
      if ((e.target as HTMLElement).closest('.canvas-cell')) return;
      const idx = +(el.getAttribute('data-el') ?? -1);
      if (s.selectedEl === idx) {
        s.selectedEl = -1;
        s.selectedCell = null;
      } else {
        s.selectedEl = idx;
        s.selectedCell = null;
      }
      this.rerender();
    });

    // Cell selection
    this.onAll('.canvas-cell', 'click', (e) => {
      e.stopPropagation();
      const cell = (e.target as HTMLElement).closest('.canvas-cell');
      if (!cell) return;
      const elIdx = +(cell.getAttribute('data-el') ?? -1);
      const row = +(cell.getAttribute('data-row') ?? 0);
      const col = +(cell.getAttribute('data-col') ?? 0);
      s.selectedEl = elIdx;
      s.selectedCell = { row, col };
      this.rerender();
    });

    // Element move up/down and delete (overlay buttons)
    this.onAll('.el-move-up', 'click', (e) => {
      e.stopPropagation();
      const idx = +((e.target as HTMLElement).closest('[data-el]')?.getAttribute('data-el') ?? -1);
      if (idx > 0) {
        [s.doc.elements[idx - 1], s.doc.elements[idx]] = [s.doc.elements[idx], s.doc.elements[idx - 1]];
        s.selectedEl = idx - 1; s.dirty = true; this.rerender();
      }
    });
    this.onAll('.el-move-down', 'click', (e) => {
      e.stopPropagation();
      const idx = +((e.target as HTMLElement).closest('[data-el]')?.getAttribute('data-el') ?? -1);
      if (idx >= 0 && idx < s.doc.elements.length - 1) {
        [s.doc.elements[idx], s.doc.elements[idx + 1]] = [s.doc.elements[idx + 1], s.doc.elements[idx]];
        s.selectedEl = idx + 1; s.dirty = true; this.rerender();
      }
    });
    this.onAll('.el-delete', 'click', (e) => {
      e.stopPropagation();
      const idx = +((e.target as HTMLElement).closest('[data-el]')?.getAttribute('data-el') ?? -1);
      if (idx >= 0) {
        s.doc.elements.splice(idx, 1);
        s.selectedEl = -1; s.selectedCell = null; s.dirty = true; this.rerender();
      }
    });

    // ── Properties panel events ──

    // Grid structure
    this.onAll('.grid-add-row', 'click', (e) => {
      const el = this.getEl(e); if (!el || (el.type !== 'title' && el.type !== 'table')) return;
      const numCols = el.rows && el.rows.length > 0 ? el.rows[0].length : 1;
      el.rows = el.rows ?? [];
      el.rows.push(Array.from({ length: numCols }, () => ({ text: '' })));
      s.dirty = true; this.rerender();
    });
    this.onAll('.grid-remove-row', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      if ((el.rows?.length ?? 0) > 1) { el.rows!.pop(); s.dirty = true; this.rerender(); }
    });
    this.onAll('.grid-add-col', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.rows = (el.rows ?? []).map(r => [...r, { text: '' }]);
      s.dirty = true; this.rerender();
    });
    this.onAll('.grid-remove-col', 'click', (e) => {
      const el = this.getEl(e); if (!el || (el.rows?.[0]?.length ?? 0) <= 1) return;
      el.rows = el.rows!.map(r => r.slice(0, -1));
      s.dirty = true; this.rerender();
    });
    this.onAll('.grid-borders-all', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.allBorders = true; el.noBorders = false; s.dirty = true; this.rerender();
    });
    this.onAll('.grid-borders-none', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.noBorders = true; el.allBorders = false; s.dirty = true; this.rerender();
    });
    this.onAll('.grid-colwidth', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const col = +((e.target as HTMLElement).getAttribute('data-col') ?? 0);
      el.colWidths = el.colWidths ?? [];
      el.colWidths[col] = +((e.target as HTMLInputElement).value) || 1;
      s.dirty = true; this.rerender();
    });
    this.onAll('.grid-rowheight', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const row = +((e.target as HTMLElement).getAttribute('data-row') ?? 0);
      el.rowHeights = el.rowHeights ?? [];
      el.rowHeights[row] = +((e.target as HTMLInputElement).value) || 1;
      s.dirty = true; this.rerender();
    });

    // Grid default colours
    this.attachColorListeners('grid-bg', (el, v) => { el.bgColor = v || undefined; });
    this.attachColorListeners('grid-tx', (el, v) => { el.textColor = v || undefined; });

    // Cell text
    this.onAll('.cell-text', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.text = (e.target as HTMLInputElement).value; s.dirty = true; this.rerender();
    });

    this.onAll('.cell-image-file', 'change', (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = () => {
        const cell = this.getCell(e); if (!cell) return;
        cell.imageData = reader.result as string;
        cell.imageFit = cell.imageFit ?? 'contain';
        cell.imagePadding = cell.imagePadding ?? 2;
        s.dirty = true; this.rerender();
      };
      reader.readAsDataURL(file);
    });
    this.onAll('.cell-image-fit', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.imageFit = (e.target as HTMLSelectElement).value as 'contain' | 'stretch';
      s.dirty = true; this.rerender();
    });
    this.onAll('.cell-image-padding', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.imagePadding = Math.max(0, +((e.target as HTMLInputElement).value) || 0);
      s.dirty = true; this.rerender();
    });
    this.onAll('.cell-image-remove', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      delete cell.imageData;
      delete cell.imageFit;
      delete cell.imagePadding;
      s.dirty = true; this.rerender();
    });

    // Cell font
    this.onAll('.cell-font-family', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.font = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.onAll('.cell-font-size', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.size = +(e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.onAll('.cell-bold', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.bold = !cell.bold; s.dirty = true; this.rerender();
    });
    this.onAll('.cell-italic', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.italic = !cell.italic; s.dirty = true; this.rerender();
    });
    this.onAll('.cell-underline', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.underline = !cell.underline; s.dirty = true; this.rerender();
    });
    this.onAll('.cell-align', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.align = (e.target as HTMLElement).closest('[data-align]')?.getAttribute('data-align') ?? 'L';
      s.dirty = true; this.rerender();
    });

    // Cell borders
    this.onAll('.cell-border-inc', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      const side = (e.target as HTMLElement).closest('[data-side]')?.getAttribute('data-side') as keyof BorderProps;
      if (!side) return;
      cell.borders = cell.borders ?? { left: 0, right: 0, top: 0, bottom: 0 };
      cell.borders[side] = Math.min(5, cell.borders[side] + 1);
      s.dirty = true; this.rerender();
    });
    this.onAll('.cell-border-dec', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      const side = (e.target as HTMLElement).closest('[data-side]')?.getAttribute('data-side') as keyof BorderProps;
      if (!side) return;
      cell.borders = cell.borders ?? { left: 0, right: 0, top: 0, bottom: 0 };
      cell.borders[side] = Math.max(0, cell.borders[side] - 1);
      s.dirty = true; this.rerender();
    });
    this.onAll('.cell-border-qs', 'click', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      const qs = (e.target as HTMLElement).closest('[data-qs]')?.getAttribute('data-qs');
      cell.borders = this.quickSetBorder(qs);
      s.dirty = true; this.rerender();
    });

    // Cell colours
    this.attachColorListeners('cell-bg', (el, v, row, col) => {
      const cell = this.getCellByIndex(el, row!, col!); if (cell) { cell.bgColor = v || undefined; }
    });
    this.attachColorListeners('cell-tx', (el, v, row, col) => {
      const cell = this.getCellByIndex(el, row!, col!); if (cell) { cell.textColor = v || undefined; }
    });

    // Cell wrap
    this.onAll('.cell-wrap', 'change', (e) => {
      const cell = this.getCell(e); if (!cell) return;
      cell.wrap = (e.target as HTMLInputElement).checked; s.dirty = true;
    });

    // Spacer height
    this.onAll('.spacer-height', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.height = +(e.target as HTMLInputElement).value || 36; s.dirty = true; this.rerender();
    });

    // Footer
    this.onAll('.footer-text', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.text = (e.target as HTMLInputElement).value; s.dirty = true; this.rerender();
    });
    this.onAll('.footer-font-family', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.font = (e.target as HTMLSelectElement).value; s.dirty = true;
    });
    this.onAll('.footer-font-size', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.size = +(e.target as HTMLSelectElement).value; s.dirty = true;
    });
    this.onAll('.footer-bold', 'click', (e) => {
      const el = this.getEl(e); if (!el) return; el.bold = !el.bold; s.dirty = true; this.rerender();
    });
    this.onAll('.footer-italic', 'click', (e) => {
      const el = this.getEl(e); if (!el) return; el.italic = !el.italic; s.dirty = true; this.rerender();
    });
    this.onAll('.footer-underline', 'click', (e) => {
      const el = this.getEl(e); if (!el) return; el.underline = !el.underline; s.dirty = true; this.rerender();
    });
    this.onAll('.footer-align', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.align = (e.target as HTMLElement).closest('[data-align]')?.getAttribute('data-align') ?? 'L';
      s.dirty = true; this.rerender();
    });
    this.onAll('.footer-border-inc', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const side = (e.target as HTMLElement).closest('[data-side]')?.getAttribute('data-side') as keyof BorderProps;
      if (!side) return;
      el.footerBorders = el.footerBorders ?? { left: 0, right: 0, top: 0, bottom: 0 };
      el.footerBorders[side] = Math.min(5, el.footerBorders[side] + 1);
      s.dirty = true; this.rerender();
    });
    this.onAll('.footer-border-dec', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const side = (e.target as HTMLElement).closest('[data-side]')?.getAttribute('data-side') as keyof BorderProps;
      if (!side) return;
      el.footerBorders = el.footerBorders ?? { left: 0, right: 0, top: 0, bottom: 0 };
      el.footerBorders[side] = Math.max(0, el.footerBorders[side] - 1);
      s.dirty = true; this.rerender();
    });
    this.onAll('.footer-border-qs', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const qs = (e.target as HTMLElement).closest('[data-qs]')?.getAttribute('data-qs');
      el.footerBorders = this.quickSetBorder(qs); s.dirty = true; this.rerender();
    });
    this.attachColorListeners('footer-tx', (el, v) => { el.textColor2 = v || undefined; });

    // Image
    this.onAll('.image-height', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.height = +(e.target as HTMLInputElement).value || 72; s.dirty = true; this.rerender();
    });
    this.onAll('.image-width', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.width = +(e.target as HTMLInputElement).value || 0; s.dirty = true; this.rerender();
    });
    this.onAll('.image-file', 'change', (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = () => {
        const el = this.getEl(e); if (!el) return;
        el.imageData = reader.result as string; s.dirty = true; this.rerender();
      };
      reader.readAsDataURL(file);
    });

    // ── Chart property listeners ──

    const ensureChart = (el: TElement): ChartConfig => {
      if (!el.chartConfig) el.chartConfig = { metrics: [], lookback: '24h' };
      return el.chartConfig;
    };

    this.onAll('.chart-height', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.height = +(e.target as HTMLInputElement).value || 200; s.dirty = true; this.rerender();
    });
    this.onAll('.chart-title', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).title = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.onAll('.chart-ylabel', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).yLabel = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.onAll('.chart-ymin', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const v = (e.target as HTMLInputElement).value;
      ensureChart(el).yMin = v !== '' ? +v : undefined; s.dirty = true;
    });
    this.onAll('.chart-ymax', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const v = (e.target as HTMLInputElement).value;
      ensureChart(el).yMax = v !== '' ? +v : undefined; s.dirty = true;
    });
    this.onAll('.chart-legend', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).showLegend = (e.target as HTMLInputElement).checked; s.dirty = true;
    });
    this.onAll('.chart-smooth', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).smooth = (e.target as HTMLInputElement).checked; s.dirty = true;
    });
    this.onAll('.chart-fill', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).fillArea = (e.target as HTMLInputElement).checked; s.dirty = true;
    });

    // Series colour controls
    this.onAll('.chart-color-picker', 'input', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensureChart(el);
      if (!cfg.colors) cfg.colors = [];
      cfg.colors[idx] = (e.target as HTMLInputElement).value;
      // sync hex input
      const hex = this.querySelector<HTMLInputElement>(`.chart-color-hex[data-idx="${idx}"]`);
      if (hex) hex.value = cfg.colors[idx];
      s.dirty = true;
    });
    this.onAll('.chart-color-hex', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const val = (e.target as HTMLInputElement).value;
      if (/^#[0-9a-fA-F]{6}$/.test(val)) {
        const cfg = ensureChart(el);
        if (!cfg.colors) cfg.colors = [];
        cfg.colors[idx] = val; s.dirty = true;
      }
    });
    this.onAll('.chart-color-remove', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensureChart(el);
      if (cfg.colors) { cfg.colors.splice(idx, 1); s.dirty = true; this.rerender(); }
    });
    this.onAll('.chart-color-add', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const cfg = ensureChart(el);
      if (!cfg.colors) cfg.colors = [];
      cfg.colors.push('#4a9eff'); s.dirty = true; this.rerender();
    });

    // Lookback
    this.onAll('.chart-lookback', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureChart(el).lookback = (e.target as HTMLSelectElement).value; s.dirty = true;
    });

    // Metric name inputs
    this.onAll('.chart-metric-name', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensureChart(el);
      if (!cfg.metrics) cfg.metrics = [];
      cfg.metrics[idx] = (e.target as HTMLInputElement).value.trim(); s.dirty = true;
    });
    this.onAll('.chart-metric-remove', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensureChart(el);
      if (cfg.metrics) { cfg.metrics.splice(idx, 1); s.dirty = true; this.rerender(); }
    });
    this.onAll('.chart-metric-add', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const cfg = ensureChart(el);
      if (!cfg.metrics) cfg.metrics = [];
      cfg.metrics.push(''); s.dirty = true; this.rerender();
    });
    this.onAll('.chart-metric-pick', 'click', (e) => {
      const target = e.target as HTMLElement;
      const elIdx = +(target.closest('[data-el]')?.getAttribute('data-el') ?? -1);
      const idx = +(target.getAttribute('data-idx') ?? -1);
      if (elIdx < 0 || idx < 0) return;
      getTreeBrowserDialog().open('', 'Select Tag', (selectedPath) => {
        // Strip suffix codes (:U, :S, :A, :D, :N); org prefix is already stripped by tree-browser
        const tagPath = selectedPath.replace(/:[A-Z]$/, '');
        const el = s.doc.elements[elIdx];
        const cfg = ensureChart(el);
        if (!cfg.metrics) cfg.metrics = [];
        cfg.metrics[idx] = tagPath;
        // Also update the visible input without a full rerender
        const input = this.querySelector<HTMLInputElement>(
          `.chart-metric-name[data-el="${elIdx}"][data-idx="${idx}"]`
        );
        if (input) input.value = tagPath;
        s.dirty = true;
      }, true /* includeLeaves */);
    });

    // ── Events property listeners ──

    const ensureEvents = (el: TElement): EventsConfig => {
      if (!el.eventsConfig) el.eventsConfig = { lookback: '24h', limit: 50, columns: ['timestamp', 'severity', 'device', 'message'] };
      return el.eventsConfig;
    };

    this.onAll('.evt-severity', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureEvents(el).severity = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.onAll('.evt-device', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureEvents(el).device = (e.target as HTMLInputElement).value.trim(); s.dirty = true; this.rerender();
    });
    this.onAll('.evt-search', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureEvents(el).search = (e.target as HTMLInputElement).value.trim(); s.dirty = true; this.rerender();
    });
    this.onAll('.evt-lookback', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureEvents(el).lookback = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender();
    });
    this.onAll('.evt-limit', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensureEvents(el).limit = Math.max(1, Math.min(500, +(e.target as HTMLInputElement).value || 50));
      s.dirty = true; this.rerender();
    });
    this.onAll('.evt-col', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const key = (e.target as HTMLElement).getAttribute('data-col-key') ?? '';
      const cfg = ensureEvents(el);
      const checked = (e.target as HTMLInputElement).checked;
      if (checked && !cfg.columns.includes(key)) cfg.columns.push(key);
      if (!checked) cfg.columns = cfg.columns.filter(c => c !== key);
      s.dirty = true; this.rerender();
    });
    this.onAll('.evt-font-size', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.size = +(e.target as HTMLSelectElement).value || 8; s.dirty = true; this.rerender();
    });
    // Events header color pickers
    this.onAll('.evt-hdr-bg-picker, .evt-hdr-bg-hex', 'input', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.bgColor = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.onAll('.evt-hdr-bg-hex', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const v = (e.target as HTMLInputElement).value;
      if (/^#[0-9a-fA-F]{6}$/.test(v)) { el.bgColor = v; s.dirty = true; this.rerender(); }
    });
    this.onAll('.evt-hdr-bg-clear', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.bgColor = ''; s.dirty = true; this.rerender();
    });
    this.onAll('.evt-hdr-bg-swatch', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.bgColor = (e.target as HTMLElement).getAttribute('data-color') ?? ''; s.dirty = true; this.rerender();
    });
    this.onAll('.evt-hdr-tx-picker, .evt-hdr-tx-hex', 'input', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.textColor = (e.target as HTMLInputElement).value; s.dirty = true;
    });
    this.onAll('.evt-hdr-tx-hex', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const v = (e.target as HTMLInputElement).value;
      if (/^#[0-9a-fA-F]{6}$/.test(v)) { el.textColor = v; s.dirty = true; this.rerender(); }
    });
    this.onAll('.evt-hdr-tx-clear', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.textColor = ''; s.dirty = true; this.rerender();
    });
    this.onAll('.evt-hdr-tx-swatch', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.textColor = (e.target as HTMLElement).getAttribute('data-color') ?? ''; s.dirty = true; this.rerender();
    });

    // ── Pie chart property listeners ──

    const ensurePie = (el: TElement): PieChartConfig => {
      if (!el.pieChartConfig) el.pieChartConfig = { slices: [], showLegend: true };
      return el.pieChartConfig;
    };

    this.onAll('.piechart-height', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      el.height = +(e.target as HTMLInputElement).value || 200; s.dirty = true; this.rerender();
    });
    this.onAll('.piechart-legend', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      ensurePie(el).showLegend = (e.target as HTMLInputElement).checked; s.dirty = true; this.rerender();
    });
    this.onAll('.piechart-slice-add', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const cfg = ensurePie(el);
      cfg.slices.push({ value: '', color: '#4a9eff', label: '' });
      s.dirty = true; this.rerender();
    });
    this.onAll('.piechart-slice-remove', 'click', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensurePie(el);
      if (idx >= 0) { cfg.slices.splice(idx, 1); s.dirty = true; this.rerender(); }
    });
    this.onAll('.piechart-slice-value', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensurePie(el);
      if (idx >= 0) { cfg.slices[idx].value = (e.target as HTMLInputElement).value.trim(); s.dirty = true; }
    });
    this.onAll('.piechart-slice-label', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensurePie(el);
      if (idx >= 0) { cfg.slices[idx].label = (e.target as HTMLInputElement).value; s.dirty = true; }
    });
    this.onAll('.piechart-slice-color-picker', 'input', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const cfg = ensurePie(el);
      if (idx >= 0) {
        cfg.slices[idx].color = (e.target as HTMLInputElement).value;
        const hex = this.querySelector<HTMLInputElement>(`.piechart-slice-color-hex[data-idx="${idx}"]`);
        if (hex) hex.value = cfg.slices[idx].color;
        s.dirty = true;
      }
    });
    this.onAll('.piechart-slice-color-hex', 'change', (e) => {
      const el = this.getEl(e); if (!el) return;
      const idx = +((e.target as HTMLElement).getAttribute('data-idx') ?? -1);
      const val = (e.target as HTMLInputElement).value;
      if (idx >= 0 && /^#[0-9a-fA-F]{6}$/.test(val)) {
        ensurePie(el).slices[idx].color = val; s.dirty = true;
      }
    });
    this.onAll('.piechart-slice-pick', 'click', (e) => {
      const target = e.target as HTMLElement;
      const elIdx = +(target.closest('[data-el]')?.getAttribute('data-el') ?? -1);
      const idx = +(target.getAttribute('data-idx') ?? -1);
      if (elIdx < 0 || idx < 0) return;
      getTreeBrowserDialog().open('', 'Select Tag', (selectedPath) => {
        const tagPath = selectedPath.replace(/:[A-Z]$/, '');
        const el = s.doc.elements[elIdx];
        const cfg = ensurePie(el);
        cfg.slices[idx].value = tagPath;
        const input = this.querySelector<HTMLInputElement>(
          `.piechart-slice-value[data-el="${elIdx}"][data-idx="${idx}"]`
        );
        if (input) input.value = tagPath;
        s.dirty = true;
      }, true);
    });
  }

  private attachTabListeners(): void {
    const s = this.state;
    if (s.activeTab === 'variables') {
      this.on('#btn-add-variable', 'click', () => {
        s.variables.push({ name: `var${s.variables.length + 1}`, label: '', type: 'builtin', source: 'now' });
        s.dirty = true; this.rerender();
      });
      this.onAll('.btn-remove-var', 'click', (e) => {
        const idx = +((e.target as HTMLElement).closest('[data-idx]')?.getAttribute('data-idx') ?? -1);
        if (idx >= 0) { s.variables.splice(idx, 1); s.dirty = true; this.rerender(); }
      });
      this.onAll('.var-name', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].name = (e.target as HTMLInputElement).value.trim(); s.dirty = true; }
      });
      this.onAll('.var-type', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) {
          s.variables[idx].type = (e.target as HTMLSelectElement).value as any;
          s.dirty = true; this.rerender();
        }
      });
      this.onAll('.var-source', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].source = (e.target as HTMLSelectElement).value; s.dirty = true; this.rerender(); }
      });
      this.onAll('.var-format', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].format = (e.target as HTMLInputElement).value; s.dirty = true; }
      });
      this.onAll('.var-path', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].path = (e.target as HTMLInputElement).value.trim(); s.dirty = true; }
      });
      this.onAll('.var-query', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].query = (e.target as HTMLTextAreaElement).value; s.dirty = true; }
      });
      this.onAll('.var-label', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) { s.variables[idx].label = (e.target as HTMLInputElement).value; s.dirty = true; }
      });
      this.onAll('.var-input-type', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx >= 0) {
          s.variables[idx].inputType = (e.target as HTMLSelectElement).value;
          s.dirty = true; this.rerender();
        }
      });
      this.onAll('.var-default-value', 'change', (e) => {
        const idx = this.varIdx(e);
        if (idx < 0) return;
        const v = s.variables[idx];
        v.defaultValue = (e.target as HTMLInputElement).value;
        // Only update previewValues if the user hasn't set a custom value yet
        if (!(v.name in s.previewValues) || s.previewValues[v.name] === '') {
          s.previewValues[v.name] = v.defaultValue;
        }
        s.dirty = true;
      });
      // Sync on type/name changes that affect the custom-variables set
      this.onAll('.var-type', 'change', () => this.syncPreviewValues());
      this.onAll('.var-name', 'change', () => this.syncPreviewValues());
      this.onAll('.btn-remove-var', 'click', () => this.syncPreviewValues());
    }
    if (s.activeTab === 'preview') {
      // Live-update previewValues as the user types in the parameters panel
      this.onAll('.preview-param', 'input', (e) => {
        const name = (e.target as HTMLElement).getAttribute('data-name') ?? '';
        if (name) s.previewValues[name] = (e.target as HTMLInputElement).value;
      });
      this.on('#btn-refresh-preview', 'click', () => this.loadPreview());
      this.on('#btn-zoom-out', 'click', () => {
        s.previewZoom = Math.max(0.25, Math.round((s.previewZoom - 0.1) * 10) / 10);
        this.rerender();
      });
      this.on('#btn-zoom-in', 'click', () => {
        s.previewZoom = Math.min(3, Math.round((s.previewZoom + 0.1) * 10) / 10);
        this.rerender();
      });
      this.on('#btn-zoom-reset', 'click', () => {
        s.previewZoom = 1.0;
        this.rerender();
      });
    }
  }

  // ─── Colour listener helper ────────────────────────────────────────────────

  private attachColorListeners(
    cls: string,
    setter: (el: TElement, v: string, row?: number, col?: number) => void,
  ): void {
    const s = this.state;

    const apply = (e: Event, val: string) => {
      const target = e.target as HTMLElement;
      const elIdx = +(target.closest('[data-el]')?.getAttribute('data-el') ?? -1);
      const row = target.closest('[data-row]') ? +(target.closest('[data-row]')!.getAttribute('data-row')!) : undefined;
      const col = row !== undefined ? +(target.closest('[data-col]')!.getAttribute('data-col')!) : undefined;
      const el = s.doc.elements[elIdx];
      if (!el) return;
      setter(el, val, row, col);
      // Update the hex text input sibling
      const hex = this.querySelector(`.${cls}-hex[data-el="${elIdx}"]${row !== undefined ? `[data-row="${row}"][data-col="${col}"]` : ''}`) as HTMLInputElement;
      if (hex) hex.value = val;
      // Update the color picker sibling
      const picker = this.querySelector(`.${cls}-picker[data-el="${elIdx}"]${row !== undefined ? `[data-row="${row}"][data-col="${col}"]` : ''}`) as HTMLInputElement;
      if (picker && val) picker.value = val;
      s.dirty = true;
      // Soft update canvas only
      this.updateCanvasCell(elIdx, row, col);
    };

    this.onAll(`.${cls}-picker`, 'input', (e) => apply(e, (e.target as HTMLInputElement).value));
    this.onAll(`.${cls}-hex`, 'change', (e) => {
      const v = (e.target as HTMLInputElement).value.trim();
      if (/^#[0-9a-fA-F]{3,6}$/.test(v) || v === '') apply(e, v);
    });
    this.onAll(`.${cls}-swatch`, 'click', (e) => {
      const sw = (e.target as HTMLElement).closest('[data-swatch]')?.getAttribute('data-swatch') ?? '';
      apply(e, sw);
    });
    this.onAll(`.${cls}-clear`, 'click', (e) => apply(e, ''));
  }

  // ─── Soft canvas refresh (colour changes only) ─────────────────────────────

  private updateCanvasCell(_elIdx: number, _row?: number, _col?: number): void {
    // Just do a full rerender - cheaper than trying to patch individual cells
    this.rerender();
  }

  // ─── Helper accessors ──────────────────────────────────────────────────────

  private getEl(e: Event): TElement | null {
    const idx = +((e.target as HTMLElement).closest('[data-el]')?.getAttribute('data-el') ?? -1);
    return this.state.doc.elements[idx] ?? null;
  }

  private getCell(e: Event): CellProps | null {
    const target = e.target as HTMLElement;
    const elIdx = +(target.closest('[data-el]')?.getAttribute('data-el') ?? -1);
    const row = +(target.closest('[data-row]')?.getAttribute('data-row') ?? -1);
    const col = +(target.closest('[data-col]')?.getAttribute('data-col') ?? -1);
    return this.getCellByIndex(this.state.doc.elements[elIdx], row, col);
  }

  private getCellByIndex(el: TElement | null | undefined, row: number, col: number): CellProps | null {
    if (!el || !el.rows || row < 0 || col < 0) return null;
    if (row >= el.rows.length) return null;
    if (col >= el.rows[row].length) return null;
    return el.rows[row][col];
  }

  private varIdx(e: Event): number {
    return +((e.target as HTMLElement).closest('[data-idx]')?.getAttribute('data-idx') ?? -1);
  }

  private quickSetBorder(qs: string | null | undefined): BorderProps {
    switch (qs) {
      case 'all': return { left: 1, right: 1, top: 1, bottom: 1 };
      case 'box': return { left: 1, right: 1, top: 1, bottom: 1 };
      case 'bottom': return { left: 0, right: 0, top: 0, bottom: 1 };
      default: return { left: 0, right: 0, top: 0, bottom: 0 };
    }
  }

  // ─── Element insertion ────────────────────────────────────────────────────

  private insertElement(type: TElement['type'], atIndex: number): void {
    const s = this.state;
    let el: TElement;
    switch (type) {
      case 'title':
        el = {
          id: eid(), type: 'title',
          rows: [[{ text: 'Title Text', bold: true, size: 16, align: 'C' }]],
          colWidths: [1], rowHeights: [2],
          bgColor: '#1e3a5f', textColor: '#ffffff', noBorders: true,
        };
        break;
      case 'table':
        el = {
          id: eid(), type: 'table',
          rows: [
            [{ text: 'Column 1', bold: true }, { text: 'Column 2', bold: true }],
            [{ text: 'Data 1' }, { text: 'Data 2' }],
          ],
          colWidths: [1, 1], rowHeights: [1, 1],
          bgColor: '#ffffff', textColor: '#000000', allBorders: true,
        };
        break;
      case 'footer':
        el = {
          id: eid(), type: 'footer',
          text: 'Footer text', align: 'C', size: 10,
          footerBorders: { left: 0, right: 0, top: 1, bottom: 0 },
        };
        break;
      case 'spacer':
        el = { id: eid(), type: 'spacer', height: 36 };
        break;
      case 'image':
        el = { id: eid(), type: 'image', height: 144, width: 0 };
        break;
      case 'chart':
        el = {
          id: eid(), type: 'chart', height: 200,
          chartConfig: { metrics: [], lookback: '24h', showLegend: true, smooth: false, fillArea: false, colors: [] },
        };
        break;
      case 'events':
        el = {
          id: eid(), type: 'events', size: 8,
          bgColor: '#1e3a5f', textColor: '#ffffff',
          eventsConfig: {
            severity: '', device: '', search: '',
            lookback: '24h', limit: 50,
            columns: ['timestamp', 'severity', 'device', 'message'],
          },
        };
        break;
      case 'pie-chart':
        el = {
          id: eid(), type: 'pie-chart', height: 200,
          pieChartConfig: { slices: [], showLegend: true },
        };
        break;
      default:
        return;
    }
    s.doc.elements.splice(atIndex, 0, el);
    s.selectedEl = atIndex;
    s.selectedCell = null;
    s.dirty = true;
    this.rerender();
  }

  // ─── Utility addEventListener wrappers ────────────────────────────────────

  private _listeners: Array<{ el: Element; event: string; fn: EventListener }> = [];

  private on(selector: string, event: string, fn: (e: Event) => void): void {
    const el = this.querySelector(selector);
    if (!el) return;
    el.addEventListener(event, fn);
    this._listeners.push({ el, event, fn });
  }

  private onAll(selector: string, event: string, fn: (e: Event) => void): void {
    this.querySelectorAll(selector).forEach(el => {
      el.addEventListener(event, fn);
      this._listeners.push({ el, event, fn });
    });
  }

  private syncDirtyControls(): void {
    const s = this.state;
    const dirtyIndicator = this.querySelector('#dirty-indicator') as HTMLElement | null;
    if (dirtyIndicator) dirtyIndicator.hidden = !s.dirty;

    const saveButton = this.querySelector('#btn-save-template') as HTMLButtonElement | null;
    if (!saveButton) return;
    saveButton.disabled = s.saving || !s.dirty;
    if (s.dirty) {
      saveButton.style.background = 'var(--accent-color)';
      saveButton.style.color = 'var(--accent-text)';
      saveButton.style.cursor = '';
    } else {
      saveButton.style.background = 'rgba(148,163,184,0.18)';
      saveButton.style.color = 'rgba(148,163,184,0.65)';
      saveButton.style.cursor = 'not-allowed';
    }
  }

  // ─── Template CRUD ────────────────────────────────────────────────────────

  // Rebuild previewValues from the current custom variables, preserving any
  // values the user has already typed.
  private syncPreviewValues(): void {
    const s = this.state;
    const next: Record<string, string> = {};
    for (const v of s.variables) {
      if (v.type === 'custom') {
        next[v.name] = s.previewValues[v.name] ?? v.defaultValue ?? '';
      }
    }
    s.previewValues = next;
  }

  private openNewTemplate(): void {
    if (!this.canManage) return;
    const s = this.state;
    s.view = 'editor';
    s.isNew = true;
    s.editing = null;
    s.doc = defaultDoc();
    s.variables = structuredClone(DEFAULT_VARIABLES);
    s.templateName = '';
    s.templateDesc = '';
    s.activeTab = this.canManage ? 'editor' : 'preview';
    s.selectedEl = -1;
    s.selectedCell = null;
    s.dirty = false;
    s.saveError = '';
    s.previewValues = {};
    this.revokePreview();
    this.rerender();
  }

  private openTemplate(t: PDFTemplate): void {
    const s = this.state;
    s.view = 'editor';
    s.isNew = false;
    s.editing = t;
    try {
      s.doc = JSON.parse(typeof t.templateJson === 'string' ? t.templateJson : JSON.stringify(t.templateJson));
      // ensure all elements have ids
      for (const el of s.doc.elements) { if (!el.id) el.id = eid(); }
    } catch {
      s.doc = defaultDoc();
    }
    s.variables = Array.isArray(t.variables) ? structuredClone(t.variables) : structuredClone(DEFAULT_VARIABLES);
    s.templateName = t.name;
    s.templateDesc = t.description;
    s.activeTab = 'editor';
    s.selectedEl = -1;
    s.selectedCell = null;
    s.dirty = false;
    s.saveError = '';
    s.previewValues = {};
    this.syncPreviewValues();
    this.revokePreview();
    this.rerender();
  }

  private closeEditor(): void {
    const s = this.state;
    s.view = 'list';
    s.editing = null;
    s.dirty = false;
    this.revokePreview();
    this.rerender();
  }

  private async saveTemplate(): Promise<void> {
    if (!this.canManage) return;
    const s = this.state;
    const name = (this.querySelector('#editor-name') as HTMLInputElement)?.value.trim() || s.templateName;
    if (!name) { s.saveError = 'Name is required'; this.rerender(); return; }

    // If on JSON tab, parse editor content first
    if (s.activeTab === 'json') {
      const je = this.querySelector('#json-editor') as any;
      if (je) {
        try {
          this.applyTemplateJSONInput(je.getValue());
        } catch (err: any) {
          s.saveError = `Invalid JSON: ${err?.message ?? 'Unable to parse JSON'}`;
          this.rerender();
          return;
        }
      }
    }

    s.saving = true; s.saveError = ''; this.rerender();
    try {
      const payload = {
        name,
        description: (this.querySelector('#editor-desc') as HTMLInputElement)?.value || s.templateDesc,
        templateJson: s.doc,
        variables: s.variables,
      };
      if (s.isNew) {
        const created = await createPDFTemplate(payload);
        s.editing = created; s.isNew = false;
        s.templates.push(created);
      } else {
        const updated = await updatePDFTemplate(s.editing!.id, payload);
        s.editing = updated;
        const idx = s.templates.findIndex(t => t.id === updated.id);
        if (idx >= 0) s.templates[idx] = updated;
      }
      s.templateName = name;
      s.dirty = false;
    } catch (err: any) {
      s.saveError = err.message ?? 'Save failed';
    }
    s.saving = false;
    this.rerender();
  }

  private async deleteTemplate(id: string): Promise<void> {
    if (!this.canManage) return;
    try {
      await deletePDFTemplate(id);
      this.state.templates = this.state.templates.filter(t => t.id !== id);
    } catch { /* ignore */ }
    this.state.deleteConfirm = null;
    this.rerender();
  }

  private async downloadPDF(id: string, name: string): Promise<void> {
    try {
      const blob = await generatePDF(id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url; a.download = `${name.replace(/[^a-z0-9_-]/gi, '_')}.pdf`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (err: any) {
      await showAlert(`Download failed: ${err.message}`, {
        title: 'Download failed',
        tone: 'danger',
      });
    }
  }

  private async loadPreview(): Promise<void> {
    const s = this.state;
    if (!s.editing) return;
    // Capture current param inputs before the rerender wipes the DOM
    this.capturePreviewParams();
    if (this.canManage && s.dirty) {
      await this.saveTemplate();
      if (this.state.dirty || this.state.saveError) return;
    }
    this.revokePreview();
    s.previewLoading = true; s.previewError = ''; this.rerender();
    try {
      const blob = await previewPDFTemplate(
        s.editing.id,
        Object.keys(s.previewValues).length > 0 ? s.previewValues : undefined,
      );
      s.previewUrl = URL.createObjectURL(blob);
    } catch (err: any) {
      s.previewError = err.message ?? 'Preview failed';
    }
    s.previewLoading = false;
    this.rerender();
  }

  // Read current .preview-param input values into state before rerender.
  private capturePreviewParams(): void {
    this.querySelectorAll<HTMLInputElement>('.preview-param').forEach(input => {
      const name = input.getAttribute('data-name');
      if (name) this.state.previewValues[name] = input.value;
    });
  }

  private revokePreview(): void {
    if (this.state.previewUrl) {
      URL.revokeObjectURL(this.state.previewUrl);
      this.state.previewUrl = null;
    }
  }

  private renderPieChartProperties(el: TElement): string {
    const elIdx = this.state.selectedEl;
    const cfg: PieChartConfig = el.pieChartConfig ?? { slices: [], showLegend: true };
    const slices = cfg.slices ?? [];
    
    return `
      <div class="space-y-3 pb-4 text-xs">
        <div>
          <div class="opacity-50 mb-0.5">Height (pt)</div>
          <input type="number" class="piechart-height prop-input" data-el="${elIdx}"
                 value="${el.height ?? 200}" min="40"
                 >
        </div>

        <!-- Data source -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Slices</div>
          
          <div class="mb-2">
            <div class="opacity-40 mb-1.5" style="font-size:10px;line-height:1.4">
              Each slice requires a tag path, color, and label.
            </div>
            <div id="piechart-slices-list" class="space-y-2 mb-1" data-el="${elIdx}">
              ${slices.map((slice, i) => `
                <div class="flex flex-col gap-1.5 p-2 rounded border" style="border-color:var(--border-color)" data-slice-idx="${i}">
                  <div class="flex items-center gap-1">
                    <input type="text" class="piechart-slice-value prop-input prop-flex-1 prop-font-mono" data-el="${elIdx}" data-idx="${i}"
                           value="${this.esc(slice.value)}" placeholder="device.tag"
                           >
                    <button class="piechart-slice-pick prop-btn-icon" data-el="${elIdx}" data-idx="${i}"
                            title="Browse tag tree"
                            >⋯</button>
                    <button class="piechart-slice-remove prop-btn-remove" data-el="${elIdx}" data-idx="${i}">✕</button>
                  </div>
                  <div class="flex items-center gap-1.5">
                    <input type="color" class="piechart-slice-color-picker prop-color-picker" data-el="${elIdx}" data-idx="${i}"
                           value="${slice.color}"
                           >
                    <input type="text" class="piechart-slice-color-hex prop-input prop-flex-1 prop-font-mono" data-el="${elIdx}" data-idx="${i}"
                           value="${slice.color}" maxlength="7"
                           >
                  </div>
                   <div>
                     <input type="text" class="piechart-slice-label prop-input" data-el="${elIdx}" data-idx="${i}"
                            value="${this.esc(slice.label)}" placeholder="Legend label"
                            >
                   </div>
                </div>`).join('')}
            </div>
            <button class="piechart-slice-add prop-btn" data-el="${elIdx}">+ Add Slice</button>
          </div>
        </div>

        <!-- Appearance -->
        <div class="border-t pt-3" style="border-color:var(--border-color)">
          <div class="opacity-50 uppercase tracking-widest mb-1.5">Appearance</div>
          
          <label class="flex items-center gap-2 cursor-pointer select-none">
            <input type="checkbox" class="piechart-legend" data-el="${elIdx}" ${cfg.showLegend ? 'checked' : ''}>
            <span>Show legend</span>
          </label>
        </div>
      </div>`;
  }

  // ─── Utility ──────────────────────────────────────────────────────────────

  private esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }
}

customElements.define('pdf-template-widget', PDFTemplateWidget);
