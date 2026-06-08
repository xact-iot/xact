/**
 * status-table-widget - Two-column live status table.
 *
 * Left column: configurable row labels (optionally bold).
 * Value columns: live tag values formatted as text, number, bar, date/time, or mapped icons.
 *
 * Supports per-row color bands and an optional whole-widget hide condition.
 * Config is managed via an overlay opened by the standard widget gear button.
 */

import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { createEventLogEntry } from '../../api';
import { getCurrentUser } from '../../auth';
import { getIconSVG, isIconSetLoaded, loadIconSet } from '../../utils/icons';
import { formatUnixMillis } from '../../utils/time';
import { resolveMetricTagPath } from './tag-path-resolver';
import '../../components/icon-picker';

// ── Registration ───────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'status-table-widget',
  name: 'Status Table',
  icon: '📋',
  category: 'Metrics',
  defaultW: 6,
  defaultH: 8,
  minW: 3,
  minH: 3,
});

// ── Types ──────────────────────────────────────────────────────────────────────

type Formatter = 'text' | 'number' | 'bar' | 'date/time' | 'icon';
type ColumnKind = 'none' | 'value' | 'command';
type CommandInputKind = 'switch' | 'text' | 'number' | 'enum' | 'slider';
type IconAnimation = 'none' | 'pulse' | 'shake';

interface IconMapEntry {
  value: string;
  icon: string;
  color: string;
  size: number;
  animation: IconAnimation;
}

interface ColumnDef {
  type: ColumnKind;
  tagPath: string;
  formatter: Formatter;
  invert: boolean;
  upperLimit: number;
  colorBandsEnabled: boolean;
  colorBandsThreshold1: number;
  colorBandsThreshold2: number;
  colorBandsColor1: string;
  colorBandsColor2: string;
  colorBandsColor3: string;
  iconMap: IconMapEntry[];
  commandPath: string;
  input: CommandInputKind;
  commandValue: any;
  enumOptions: string;
  min: number;
  max: number;
  timeoutSeconds: number;
}

interface RowDef {
  id: string;
  label: string;
  tagPath: string;
  formatter: Formatter;
  bold: boolean;
  invert: boolean;
  upperLimit: number;
  colorBandsEnabled: boolean;
  colorBandsThreshold1: number;
  colorBandsThreshold2: number;
  colorBandsColor1: string;
  colorBandsColor2: string;
  colorBandsColor3: string;
  col2: ColumnDef;
  col3: ColumnDef;
}

interface Config {
  headerText: string;
  tagPrefix: string;
  hideTagPath: string;
  hideTagValue: string;
  colHeader1: string;
  colHeader2: string;
  colHeader3: string;
  rows: RowDef[];
}

// ── Defaults ───────────────────────────────────────────────────────────────────

const DEFAULT_ROW: Omit<RowDef, 'id'> = {
  label: 'Label',
  tagPath: '',
  formatter: 'text',
  bold: false,
  invert: false,
  upperLimit: 100,
  colorBandsEnabled: false,
  colorBandsThreshold1: 50,
  colorBandsThreshold2: 80,
  colorBandsColor1: '#22c55e',
  colorBandsColor2: '#f59e0b',
  colorBandsColor3: '#ef4444',
  col2: {} as ColumnDef,
  col3: {} as ColumnDef,
};

const DEFAULT_COLUMN: ColumnDef = {
  type: 'value',
  tagPath: '',
  formatter: 'text',
  invert: false,
  upperLimit: 100,
  colorBandsEnabled: false,
  colorBandsThreshold1: 50,
  colorBandsThreshold2: 80,
  colorBandsColor1: '#22c55e',
  colorBandsColor2: '#f59e0b',
  colorBandsColor3: '#ef4444',
  iconMap: [],
  commandPath: '',
  input: 'text',
  commandValue: '',
  enumOptions: '',
  min: 0,
  max: 100,
  timeoutSeconds: 10,
};

const DEFAULT_CONFIG: Config = {
  headerText: 'Status',
  tagPrefix: '',
  hideTagPath: '',
  hideTagValue: '',
  colHeader1: '',
  colHeader2: '',
  colHeader3: '',
  rows: [],
};

// ── Styles (injected once) ─────────────────────────────────────────────────────

function ensureStyles(): void {
  if (document.getElementById('status-table-widget-styles')) return;
  const s = document.createElement('style');
  s.id = 'status-table-widget-styles';
  s.textContent = `
    .stw-bar-track {
      width: 100%; height: 8px; border-radius: 4px;
      overflow: hidden;
    }
    .stw-bar-fill {
      height: 100%; border-radius: 4px;
      transition: width 0.35s ease;
    }
    .stw-command-input {
      max-width: 100%;
      font: inherit;
      border: 1px solid color-mix(in srgb,var(--accent-color) 28%,var(--border-color));
      border-radius: 4px;
      background: color-mix(in srgb,var(--content-bg) 78%,var(--accent-color) 8%);
      color: var(--content-text);
      padding: 4px 7px;
      box-sizing: border-box;
      outline: none;
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.03);
      text-align: right;
    }
    select.stw-command-input {
      text-align-last: right;
    }
    .stw-command-input:focus {
      border-color: color-mix(in srgb,var(--accent-color) 65%,var(--border-color));
      box-shadow: 0 0 0 2px color-mix(in srgb,var(--accent-color) 18%,transparent);
    }
    .stw-col-header {
      padding: 6px 10px;
      font-size: 11px;
      line-height: 1.2;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: color-mix(in srgb,var(--content-text) 72%,var(--accent-color));
      font-weight: 700;
    }
    .stw-icon-value {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      vertical-align: middle;
    }
    .stw-icon-pulse {
      animation: stw-icon-pulse 1.2s ease-in-out infinite;
    }
    .stw-icon-shake {
      animation: stw-icon-shake 0.55s linear infinite;
    }
    @keyframes stw-icon-pulse {
      0%, 100% { transform: scale(1); opacity: 0.72; }
      50% { transform: scale(1.14); opacity: 1; }
    }
    @keyframes stw-icon-shake {
      0%, 100% { transform: translateX(0); }
      20% { transform: translateX(-1.5px); }
      40% { transform: translateX(1.5px); }
      60% { transform: translateX(-1px); }
      80% { transform: translateX(1px); }
    }
    .stw-command-button {
      border: 1px solid color-mix(in srgb,var(--accent-color) 35%,transparent);
      border-radius: 4px;
      background: color-mix(in srgb,var(--accent-color) 13%,transparent);
      color: var(--accent-color);
      padding: 3px 8px;
      cursor: pointer;
      font-size: 12px;
      flex: 0 0 auto;
    }
    .stw-command-control {
      display: flex;
      align-items: center;
      justify-content: flex-end;
      gap: 6px;
      width: 100%;
      min-width: 0;
    }
    .stw-command-value {
      display: inline-flex;
      align-items: center;
      justify-content: flex-end;
      min-width: 0;
    }
    .stw-switch {
      position: relative;
      display: inline-flex;
      width: 38px;
      height: 20px;
      vertical-align: middle;
    }
    .stw-switch input {
      position: absolute;
      opacity: 0;
      width: 0;
      height: 0;
    }
    .stw-switch-track {
      position: absolute;
      inset: 0;
      border-radius: 999px;
      border: 1px solid color-mix(in srgb,var(--border-color) 75%,transparent);
      background: color-mix(in srgb,var(--content-bg) 82%,var(--border-color));
      cursor: pointer;
      transition: background 0.16s ease, border-color 0.16s ease, box-shadow 0.16s ease;
    }
    .stw-switch-track::before {
      content: '';
      position: absolute;
      width: 14px;
      height: 14px;
      left: 2px;
      top: 2px;
      border-radius: 999px;
      background: color-mix(in srgb,var(--content-text) 78%,var(--content-bg));
      box-shadow: 0 1px 3px rgba(0,0,0,0.45);
      transition: transform 0.16s ease, background 0.16s ease;
    }
    .stw-switch input:checked + .stw-switch-track {
      border-color: color-mix(in srgb,var(--accent-color) 70%,var(--border-color));
      background: color-mix(in srgb,var(--accent-color) 38%,var(--content-bg));
    }
    .stw-switch input:checked + .stw-switch-track::before {
      transform: translateX(18px);
      background: var(--accent-color);
    }
    .stw-switch input:focus-visible + .stw-switch-track {
      box-shadow: 0 0 0 2px color-mix(in srgb,var(--accent-color) 22%,transparent);
    }
    .stw-command-dialog {
      position: fixed;
      inset: 0;
      z-index: 22000;
      display: flex;
      align-items: center;
      justify-content: center;
      background: rgba(0,0,0,0.55);
    }
    .stw-row-editor > summary {
      list-style: none;
    }
    .stw-row-editor > summary::-webkit-details-marker {
      display: none;
    }
    .stw-row-toggle {
      width: 1rem;
      height: 1rem;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      color: var(--accent-color);
      opacity: 0.85;
      flex: 0 0 auto;
    }
    .stw-row-toggle::before {
      content: '▸';
      font-size: 0.8rem;
      line-height: 1;
    }
    .stw-row-editor[open] .stw-row-toggle::before {
      content: '▾';
    }
  `;
  document.head.appendChild(s);
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function esc(s: string): string {
  return String(s ?? '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function genId(): string {
  return Math.random().toString(36).slice(2, 9);
}

function isTruthy(v: any): boolean {
  if (v === null || v === undefined) return false;
  if (typeof v === 'boolean') return v;
  if (typeof v === 'number') return v !== 0;
  const s = String(v).toLowerCase().trim();
  return s !== '' && s !== '0' && s !== 'false' && s !== 'no';
}

function resolveColumnColor(col: ColumnDef, value: any): string {
  if (!col.colorBandsEnabled) return 'var(--accent-color)';
  const n = parseFloat(String(value));
  if (isNaN(n)) return 'var(--accent-color)';
  if (n < col.colorBandsThreshold1) return col.colorBandsColor1;
  if (n < col.colorBandsThreshold2) return col.colorBandsColor2;
  return col.colorBandsColor3;
}

function normalizeFormatter(formatter: any): Formatter {
  return formatter === 'number' || formatter === 'bar' || formatter === 'icon' || formatter === 'date/time' ? formatter : 'text';
}

function normalizeCommandInput(input: any): CommandInputKind {
  return input === 'switch' || input === 'number' || input === 'enum' || input === 'slider' ? input : 'text';
}

function guessFormatter(value: any): Formatter {
  if (value === null || value === undefined || value === '') return 'text';
  if (typeof value === 'number') return Number.isFinite(value) ? 'number' : 'text';
  if (typeof value === 'bigint') return 'number';
  const s = String(value).trim();
  if (!s) return 'text';
  return !Number.isNaN(Number(s)) ? 'number' : 'text';
}

function tagNameToLabel(tagName: string): string {
  return String(tagName ?? '')
    .replace(/_/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function columnFromLegacy(row: Partial<RowDef>): ColumnDef {
  return {
    ...DEFAULT_COLUMN,
    tagPath: row.tagPath ?? '',
    formatter: normalizeFormatter(row.formatter),
    invert: row.invert ?? false,
    upperLimit: row.upperLimit ?? 100,
    colorBandsEnabled: row.colorBandsEnabled ?? false,
    colorBandsThreshold1: row.colorBandsThreshold1 ?? 50,
    colorBandsThreshold2: row.colorBandsThreshold2 ?? 80,
    colorBandsColor1: row.colorBandsColor1 ?? '#22c55e',
    colorBandsColor2: row.colorBandsColor2 ?? '#f59e0b',
    colorBandsColor3: row.colorBandsColor3 ?? '#ef4444',
  };
}

function normalizeColumn(c: Partial<ColumnDef> | undefined, fallback?: ColumnDef): ColumnDef {
  const next = { ...DEFAULT_COLUMN, ...(fallback ?? {}), ...(c ?? {}) };
  next.formatter = normalizeFormatter(next.formatter);
  next.input = normalizeCommandInput(next.input);
  next.iconMap = Array.isArray(next.iconMap)
    ? next.iconMap.map(entry => ({
      value: String(entry.value ?? ''),
      icon: String(entry.icon ?? ''),
      color: String(entry.color ?? '#60a5fa'),
      size: Number(entry.size) || 20,
      animation: (entry.animation === 'pulse' || entry.animation === 'shake') ? entry.animation : 'none',
    }))
    : [];
  return next;
}

function rowFromConfig(r: Partial<RowDef>): RowDef {
  const legacy = columnFromLegacy(r);
  return {
    ...DEFAULT_ROW,
    ...r,
    id: r.id || genId(),
    col2: normalizeColumn((r as any).col2, legacy),
    col3: normalizeColumn((r as any).col3, { ...DEFAULT_COLUMN, type: 'none' }),
  };
}

// Shared style tokens - matching widget-properties-dialog aesthetics
const fldStyle = `width:100%;padding:0.25rem 0.5rem;font-size:0.875rem;border-radius:0.25rem;border:1px solid var(--border-color);background:var(--content-bg);color:var(--content-text);box-sizing:border-box;`;
const lblStyle = `display:block;font-size:0.75rem;font-weight:500;margin-bottom:0.25rem;opacity:0.75;`;
const sectionTitleStyle = `font-size:0.86rem;font-weight:700;color:color-mix(in srgb,var(--content-text) 88%,var(--accent-color));letter-spacing:0.01em;`;
const subsectionTitleStyle = `font-size:0.78rem;font-weight:650;color:color-mix(in srgb,var(--content-text) 72%,var(--accent-color));letter-spacing:0.02em;`;

// ── Widget ─────────────────────────────────────────────────────────────────────

export class StatusTableWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG, rows: [] };
  private values: Map<string, any> = new Map();
  private subscriptionActive = false;
  private _rerendering = false;
  private _cfgOverlay: HTMLElement | null = null;
  private _wizardOverlay: HTMLElement | null = null;
  private _storeUnsubs: Array<() => void> = [];
  private _deviceNameUnsub: (() => void) | null = null;
  private _lastDeviceName = '';
  private commandValues: Map<string, any> = new Map();
  private _openRowIds: Set<string> = new Set();
  private _cfgSnapshot = '';
  private _pendingIconLoads: Set<string> = new Set();

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config>): void {
    this.config = {
      ...DEFAULT_CONFIG,
      ...c,
      colHeader1: c.colHeader1 ?? '',
      colHeader2: c.colHeader2 ?? '',
      colHeader3: c.colHeader3 ?? '',
      rows: Array.isArray(c.rows) ? c.rows.map(r => rowFromConfig(r)) : [],
    };
    this.values.clear();
    this.rerender();
  }

  getConfig(): Config {
    return this.config;
  }

  /** Called by dashboard-container when the gear icon is clicked. */
  openConfig(): void {
    if (this._cfgOverlay) return;
    this._cfgSnapshot = JSON.stringify(this.config);
    this._cfgOverlay = document.createElement('div');
    document.body.appendChild(this._cfgOverlay);
    this.renderConfigOverlay();
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    ensureStyles();
    const { colHeader1, colHeader2, colHeader3, rows } = this.config;
    const hasColumn3 = this.hasColumn3();

    const hasColHeaders = colHeader1 || colHeader2 || (hasColumn3 && colHeader3);
    const hasBorder = `border-bottom:1px solid var(--border-color);`;
    const col3HeaderAlign = this.columnHeaderAlign('col3');

    const headerRow = hasColHeaders ? `
      <tr style="${hasBorder}">
        <th class="stw-col-header" style="text-align:left;">${esc(colHeader1)}</th>
        <th class="stw-col-header" style="text-align:right;">${esc(colHeader2)}</th>
        ${hasColumn3 ? `<th class="stw-col-header" style="text-align:${col3HeaderAlign};">${esc(colHeader3)}</th>` : ''}
      </tr>` : '';

    const tableRows = rows.map(row => this.renderRow(row, hasColumn3)).join('');

    this.innerHTML = `
      <div style="display:flex;flex-direction:column;height:100%;">
        <div style="flex:1 1 auto;overflow-y:auto;min-height:0;">
          <table style="width:100%;border-collapse:collapse;table-layout:auto;">
            <colgroup><col style="white-space:nowrap;"><col style="width:${hasColumn3 ? '50%' : '100%'};">${hasColumn3 ? '<col style="width:50%;">' : ''}</colgroup>
            <thead>${headerRow}</thead>
            <tbody id="stw-tbody">
              ${rows.length === 0
                ? `<tr><td colspan="${hasColumn3 ? 3 : 2}" style="padding:16px;text-align:center;opacity:0.3;font-size:12px;">No rows configured</td></tr>`
                : tableRows}
            </tbody>
          </table>
        </div>
      </div>
    `;

    this.updateCardTitle();
    this.applyHideCondition();
  }

  private hasColumn3(): boolean {
    return this.config.rows.some(row => row.col3?.type !== 'none');
  }

  private renderRow(row: RowDef, hasColumn3: boolean): string {
    const labelSz = row.bold ? 'var(--widget-label-bold-font-size)' : 'var(--widget-label-font-size)';
    const labelWt = row.bold ? 'var(--widget-label-bold-font-weight)' : 'var(--widget-label-font-weight)';
    return `
      <tr class="stw-row" data-row-id="${esc(row.id)}"
          style="border-bottom:1px solid color-mix(in srgb,var(--border-color) 50%,transparent);">
        <td style="padding:6px 10px;font-size:${labelSz};font-weight:${labelWt};overflow:hidden;text-overflow:ellipsis;white-space:nowrap;"
            title="${esc(row.label)}">${esc(row.label)}</td>
        <td style="padding:6px 10px;font-size:${labelSz};font-weight:${labelWt};text-align:${this.columnAlign(row.col2, 'col2')};">${this.renderColumn(row, row.col2, 'col2')}</td>
        ${hasColumn3 ? `<td style="padding:6px 10px;font-size:${labelSz};font-weight:${labelWt};text-align:${this.columnAlign(row.col3, 'col3')};">${this.renderColumn(row, row.col3, 'col3')}</td>` : ''}
      </tr>`;
  }

  private columnAlign(col: ColumnDef, key: 'col2' | 'col3'): 'left' | 'right' {
    return key === 'col3' && col?.type === 'value' && col.formatter === 'text' ? 'left' : 'right';
  }

  private columnHeaderAlign(key: 'col2' | 'col3'): 'left' | 'right' {
    return this.config.rows.some(row => this.columnAlign(key === 'col3' ? row.col3 : row.col2, key) === 'left')
      ? 'left'
      : 'right';
  }

  private renderColumn(row: RowDef, col: ColumnDef, key: 'col2' | 'col3'): string {
    if (!col || col.type === 'none') return '';
    if (col.type === 'command') return this.renderCommandInput(row, col, key);
    const fullPath = this.resolveTagPath(col.tagPath);
    const value = this.values.has(fullPath)
      ? this.values.get(fullPath)
      : getMirrorStore().resolveTagReference(fullPath);
    const { html, units } = this.formatCell(row, col, value);
    return units
      ? `<span style="display:inline-flex;align-items:baseline;gap:4px;max-width:100%;"><span style="min-width:0;">${html}</span><span style="font-size:0.8em;opacity:0.55;white-space:nowrap;">${esc(units)}</span></span>`
      : html;
  }

  private renderCommandInput(row: RowDef, col: ColumnDef, key: 'col2' | 'col3'): string {
    const id = `${row.id}:${key}`;
    const stored = this.commandValues.has(id) ? this.commandValues.get(id) : col.commandValue;
    const attrs = `data-command-id="${esc(id)}" data-row-id="${esc(row.id)}" data-col="${key}"`;
    const inputKind = normalizeCommandInput(col.input);
    let input = '';
    switch (inputKind) {
      case 'switch':
        input = `<label class="stw-switch" title="${isTruthy(stored) ? 'On' : 'Off'}">
          <input class="stw-command-field" ${attrs} type="checkbox" ${isTruthy(stored) ? 'checked' : ''}>
          <span class="stw-switch-track"></span>
        </label>`;
        break;
      case 'number':
        input = `<input class="stw-command-field stw-command-input" ${attrs} type="number" min="${col.min}" max="${col.max}" value="${esc(String(stored ?? ''))}" style="width:7rem;">`;
        break;
      case 'slider':
        input = `<input class="stw-command-field" ${attrs} type="range" min="${col.min}" max="${col.max}" value="${esc(String(stored ?? col.min))}" style="width:7rem;">`;
        break;
      case 'enum': {
        const opts = col.enumOptions.split(',').map(s => s.trim()).filter(Boolean);
        input = `<select class="stw-command-field stw-command-input" ${attrs} style="width:9rem;">${opts.map(o => `<option value="${esc(o)}" ${String(stored) === o ? 'selected' : ''}>${esc(o)}</option>`).join('')}</select>`;
        break;
      }
      case 'text':
      default:
        input = `<input class="stw-command-field stw-command-input" ${attrs} type="text" value="${esc(String(stored ?? ''))}" style="width:9rem;">`;
        break;
    }
    return `<span class="stw-command-control"><span class="stw-command-value">${input}</span><button class="stw-command-button" data-execute-row="${esc(row.id)}" data-execute-col="${key}">Execute</button></span>`;
  }

  private resolveTagPath(relPath: string): string {
    return resolveMetricTagPath(this.config.tagPrefix, relPath);
  }

  private resolveTagPathForPrefix(relPath: string, prefix: string): string {
    if (!relPath) return '';
    const store = getMirrorStore();
    const pathOnly = this.stripStatusSuffix(relPath);
    if (!prefix) return store.toAbsolute(pathOnly);
    if (pathOnly === prefix || pathOnly.startsWith(prefix + '.')) {
      return store.toAbsolute(pathOnly);
    }
    return store.toAbsolute(`${prefix}.${pathOnly}`);
  }

  private resolveBrowsePrefix(prefix: string): string {
    const cleanPrefix = String(prefix ?? '').trim().replace(/\.+$/g, '');
    if (!cleanPrefix) return '';

    const deviceName = getUiStore().get('deviceName') || '';
    if (!cleanPrefix.includes('*')) return cleanPrefix.replace(/\.+$/g, '');
    if (deviceName) return cleanPrefix.replace(/\*/g, deviceName).replace(/\.+$/g, '');

    const starIdx = cleanPrefix.indexOf('*');
    const parent = cleanPrefix.slice(0, starIdx).replace(/\.+$/g, '');
    const suffix = cleanPrefix.slice(starIdx + 1).replace(/^\.+|\.+$/g, '');
    if (!parent) return '';

    const store = getMirrorStore();
    const absParent = store.toAbsolute(parent);
    const child = store.listChildrenNames(absParent)[0] ?? '';
    if (!child) return parent;
    return `${parent}.${child}${suffix ? `.${suffix}` : ''}`;
  }

  private stripStatusSuffix(path: string): string {
    return getMirrorStore().baseTagPath(path);
  }

  private shouldSubscribeTag(path: string): boolean {
    const store = getMirrorStore();
    const basePath = store.baseTagPath(path);
    return !!basePath && store.getNodeType(basePath) !== 'unknown';
  }

  private formatCell(row: RowDef, col: ColumnDef, value: any): { html: string; units: string } {
    const numSz = row.bold ? '17px' : '15px';

    switch (col.formatter) {
      case 'icon': {
        return { html: this.renderIconCell(col, value), units: '' };
      }

      case 'number': {
        if (value === undefined || value === null) return { html: '<span style="opacity:0.25;">-</span>', units: '' };
        const n = parseFloat(String(value));
        if (isNaN(n)) return { html: `<span style="opacity:0.6;font-size:12px;">${esc(String(value))}</span>`, units: '' };
        const fullPath = this.resolveTagPath(col.tagPath);
        const units = fullPath ? (getMirrorStore().getNodeShared(getMirrorStore().baseTagPath(fullPath))?.units ?? '') : '';
        const formatted = Number.isInteger(n) ? String(n) : n.toFixed(2);
        const color = resolveColumnColor(col, n);
        return {
          html: `<span style="font-family:'IBM Plex Mono',ui-monospace,monospace;font-size:${numSz};color:${color};">${esc(formatted)}</span>`,
          units,
        };
      }

      case 'bar': {
        if (value === undefined || value === null) return { html: '<span style="opacity:0.25;">-</span>', units: '' };
        const n = parseFloat(String(value));
        if (isNaN(n)) return { html: '<span style="opacity:0.25;">-</span>', units: '' };
        const pct = Math.max(0, Math.min(100, (n / (col.upperLimit || 100)) * 100));
        const color = resolveColumnColor(col, n);
        const fullPath = this.resolveTagPath(col.tagPath);
        const units = fullPath ? (getMirrorStore().getNodeShared(getMirrorStore().baseTagPath(fullPath))?.units ?? '') : '';
        return {
          html: `
            <div style="display:flex;align-items:center;gap:6px;">
              <div class="stw-bar-track" style="flex:1;background:color-mix(in srgb, ${color} 18%, transparent);">
                <div class="stw-bar-fill" style="width:${pct.toFixed(1)}%;background:${color};"></div>
              </div>
              <span style="font-family:'IBM Plex Mono',ui-monospace,monospace;font-size:${numSz};color:${color};">${Number.isInteger(n) ? n : n.toFixed(1)}</span>
            </div>`,
          units,
        };
      }

      case 'date/time': {
        if (value === undefined || value === null || value === '') return { html: '<span style="opacity:0.25;">-</span>', units: '' };
        const formatted = formatUnixMillis(value, getUiStore().get('serverTimezone'));
        if (!formatted) return { html: `<span style="opacity:0.6;font-size:12px;">${esc(String(value))}</span>`, units: '' };
        return {
          html: `<span style="font-family:'IBM Plex Mono',ui-monospace,monospace;font-size:${numSz};" title="${esc(String(value))}">${esc(formatted)}</span>`,
          units: '',
        };
      }

      case 'text':
      default: {
        if (value === undefined || value === null) return { html: '<span style="opacity:0.25;">-</span>', units: '' };
        const s = String(value);
        const color = col.colorBandsEnabled ? resolveColumnColor(col, parseFloat(s)) : 'inherit';
        return {
          html: `<span style="display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:100%;color:${color};" title="${esc(s)}">${esc(s)}</span>`,
          units: '',
        };
      }
    }
  }

  private renderIconCell(col: ColumnDef, value: any): string {
    if (value === undefined || value === null) return '<span style="opacity:0.25;">-</span>';
    const valueKey = String(value);
    const entry = col.iconMap.find(item => item.value === valueKey);
    if (!entry || !entry.icon) return '<span style="opacity:0.25;">-</span>';
    const size = Math.max(10, Math.min(96, Number(entry.size) || 20));
    const animationClass = entry.animation === 'pulse'
      ? ' stw-icon-pulse'
      : entry.animation === 'shake'
        ? ' stw-icon-shake'
        : '';
    const svg = this.iconSVG(entry.icon, entry.color || 'currentColor', size);
    return `<span class="stw-icon-value${animationClass}" title="${esc(valueKey)}" style="width:${size}px;height:${size}px;color:${esc(entry.color || 'currentColor')};">${svg || esc(valueKey)}</span>`;
  }

  private iconSVG(icon: string, color: string, size: number): string {
    const prefix = icon.includes(':') ? icon.split(':')[0] : '';
    if (prefix && !isIconSetLoaded(prefix) && !this._pendingIconLoads.has(prefix)) {
      this._pendingIconLoads.add(prefix);
      loadIconSet(prefix).then(() => {
        this._pendingIconLoads.delete(prefix);
        this.rerender();
      });
    }
    return getIconSVG(icon, color, size);
  }

  protected attachEventListeners(): void {
    this.subscriptionActive = true;
    this.subscribeAll();
    this.attachCommandListeners();
    // Re-subscribe when device changes
    const ui = getUiStore();
    this._lastDeviceName = ui.get('deviceName') || '';
    this._deviceNameUnsub = ui.subscribe('deviceName', (deviceName) => {
      const nextDeviceName = deviceName || '';
      if (nextDeviceName === this._lastDeviceName) return;
      this._lastDeviceName = nextDeviceName;
      // Only rerender if tagPrefix uses the wildcard - device-specific configs
      // (e.g. those set by mountDivWidget with * already replaced) don't need
      // to react to global deviceName changes and doing so causes a rerender
      // storm when many div widgets are mounted on the map.
      if (!this.config.tagPrefix.includes('*')) return;
      this.values.clear();
      this.rerender();
    });
  }

  private attachCommandListeners(): void {
    this.querySelectorAll<HTMLInputElement | HTMLSelectElement>('.stw-command-field').forEach(input => {
      input.addEventListener('change', () => {
        const id = input.dataset.commandId;
        if (!id) return;
        if (input instanceof HTMLInputElement && input.type === 'checkbox') {
          this.commandValues.set(id, input.checked);
        } else if (input instanceof HTMLInputElement && (input.type === 'number' || input.type === 'range')) {
          this.commandValues.set(id, Number(input.value));
        } else {
          this.commandValues.set(id, input.value);
        }
      });
      input.addEventListener('input', () => {
        const id = input.dataset.commandId;
        if (!id) return;
        if (input instanceof HTMLInputElement && input.type === 'range') {
          this.commandValues.set(id, Number(input.value));
        }
      });
    });
    this.querySelectorAll<HTMLButtonElement>('.stw-command-button').forEach(btn => {
      btn.addEventListener('click', () => {
        const row = this.config.rows.find(r => r.id === btn.dataset.executeRow);
        const key = btn.dataset.executeCol === 'col3' ? 'col3' : 'col2';
        const col = key === 'col3' ? row?.col3 : row?.col2;
        if (row && col) this.executeCommand(row, col, key);
      });
    });
  }

  protected detachEventListeners(): void {
    this.subscriptionActive = false;
    this._storeUnsubs.forEach(unsub => unsub());
    this._storeUnsubs = [];
    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;
  }

  private subscribeAll(): void {
    const store = getMirrorStore();
    const paths = new Set<string>();

    for (const row of this.config.rows) {
      for (const col of [row.col2, row.col3]) {
        if (col?.type !== 'value') continue;
        const fullPath = this.resolveTagPath(col.tagPath);
        if (fullPath && this.shouldSubscribeTag(fullPath)) paths.add(fullPath);
      }
    }
    const hideFullPath = this.resolveTagPath(this.config.hideTagPath);
    if (hideFullPath && this.shouldSubscribeTag(hideFullPath)) paths.add(hideFullPath);

    for (const path of paths) {
      this._storeUnsubs.push(store.subscribeTagReference(path, (value: any) => {
        if (!this.subscriptionActive) return;
        this.values.set(path, value);

        if (path === hideFullPath) {
          this.applyHideCondition();
          return;
        }
        this.updateAffectedRows(path);
      }));
    }
  }

  private updateAffectedRows(fullPath: string): void {
    const hasColumn3 = this.hasColumn3();
    for (const row of this.config.rows) {
      const tr = this.querySelector<HTMLElement>(`.stw-row[data-row-id="${row.id}"]`);
      if (!tr) continue;
      const tds = tr.querySelectorAll('td');
      if (row.col2?.type === 'value' && this.resolveTagPath(row.col2.tagPath) === fullPath && tds[1]) {
        tds[1].innerHTML = this.renderColumn(row, row.col2, 'col2');
      }
      if (hasColumn3 && row.col3?.type === 'value' && this.resolveTagPath(row.col3.tagPath) === fullPath && tds[2]) {
        tds[2].innerHTML = this.renderColumn(row, row.col3, 'col3');
      }
    }
  }

  private applyHideCondition(): void {
    const { hideTagPath, hideTagValue } = this.config;
    if (!hideTagPath || !hideTagValue) return;
    const fullPath = this.resolveTagPath(hideTagPath);
    const v = this.values.get(fullPath);
    const hide = String(v ?? '').trim() === hideTagValue.trim();
    const card = this.closest('widget-card') as HTMLElement | null;
    const target = card ?? (this as HTMLElement);
    target.style.display = hide ? 'none' : '';
  }

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? 'Status Table');
    }
  }

  private rerender(): void {
    if (this._rerendering) return;
    this._rerendering = true;
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this._rerendering = false;
  }

  disconnectedCallback(): void {
    super.disconnectedCallback?.();
    this.closeConfig();
    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;
  }

  private currentDeviceName(): string {
    const deviceName = getUiStore().get('deviceName') || '';
    const prefix = this.config.tagPrefix.replace('*', deviceName).replace(/\.$/, '');
    if (!prefix) return deviceName;
    return prefix.split('.').filter(Boolean).pop() || deviceName;
  }

  private commandSubject(): string {
    const org = getCurrentUser()?.tenant_id || 'default';
    const tagPrefix = this.commandSubjectTagPrefix(org);
    if (!tagPrefix) throw new Error('No tag prefix selected for command');
    return `xact.command.${org}.${tagPrefix}`;
  }

  private commandSubjectTagPrefix(org: string): string {
    const deviceName = getUiStore().get('deviceName') || '';
    let prefix = this.config.tagPrefix.replace('*', deviceName).trim();
    prefix = prefix.replace(/^[./]+|[./]+$/g, '');
    if (prefix === org) return '';
    if (prefix.startsWith(`${org}.`)) return prefix.slice(org.length + 1);
    return prefix;
  }

  private commandPath(row: RowDef, col: ColumnDef): string {
    return (col.commandPath || col.tagPath || row.tagPath || row.label).trim();
  }

  private fullCommandTagPath(row: RowDef, col: ColumnDef): string {
    const path = this.commandPath(row, col);
    return path ? this.resolveTagPath(path) : '';
  }

  private displayCommandTagPath(fullPath: string): string {
    return getMirrorStore().toRelative(fullPath);
  }

  private currentCommandValue(row: RowDef, col: ColumnDef, key: 'col2' | 'col3'): any {
    const id = `${row.id}:${key}`;
    if (this.commandValues.has(id)) return this.commandValues.get(id);

    const field = this.querySelector<HTMLInputElement | HTMLSelectElement>(`.stw-command-field[data-command-id="${CSS.escape(id)}"]`);
    if (field instanceof HTMLInputElement && field.type === 'checkbox') return field.checked;
    if (field instanceof HTMLInputElement && (field.type === 'number' || field.type === 'range')) return Number(field.value);
    if (field) return field.value;

    return normalizeCommandInput(col.input) === 'switch' ? false : col.commandValue;
  }

  private collectCommandPayload(row: RowDef, col: ColumnDef, key: 'col2' | 'col3'): Record<string, any> {
    const payload: Record<string, any> = { id: crypto.randomUUID() };
    if (!col || col.type !== 'command') return payload;
    const path = this.commandPath(row, col);
    if (!path) return payload;
    payload[path] = this.currentCommandValue(row, col, key);
    return payload;
  }

  private async executeCommand(row: RowDef, col: ColumnDef, key: 'col2' | 'col3'): Promise<void> {
    let dialog: HTMLElement | null = null;
    const timeoutSeconds = Math.max(1, Number(col.timeoutSeconds) || 10);
    const payload = this.collectCommandPayload(row, col, key);
    const sentPath = this.commandPath(row, col);
    const fullTagPath = this.fullCommandTagPath(row, col);
    const displayTagPath = this.displayCommandTagPath(fullTagPath);
    const sentValue = sentPath ? payload[sentPath] : undefined;
    const sentValueText = typeof sentValue === 'string' ? sentValue : JSON.stringify(sentValue);
    let subject = '';
    const show = (state: string, bodyHtml: string) => {
      if (!dialog) {
        dialog = document.createElement('div');
        dialog.className = 'stw-command-dialog';
        document.body.appendChild(dialog);
      }
      dialog.innerHTML = `
        <div style="width:min(520px,92vw);border:1px solid var(--border-color);border-radius:6px;background:var(--content-bg);color:var(--content-text);box-shadow:0 20px 50px rgba(0,0,0,.55);">
          <div style="padding:12px 14px;border-bottom:1px solid var(--border-color);font-weight:600;color:var(--accent-color);">${esc(state)}</div>
          <div style="padding:14px;display:flex;flex-direction:column;gap:10px;">
            ${bodyHtml}
            <div style="display:flex;justify-content:flex-end;"><button class="stw-command-close" style="padding:5px 12px;border:1px solid var(--border-color);border-radius:4px;background:transparent;color:var(--content-text);cursor:pointer;">Close</button></div>
          </div>
        </div>`;
      dialog.querySelector('.stw-command-close')?.addEventListener('click', () => {
        dialog?.remove();
        dialog = null;
      });
    };
    const detailBody = (detail: string) => `
      <div style="font-size:12px;opacity:.7;">${esc(subject)}</div>
      <pre style="margin:0;max-height:220px;overflow:auto;background:rgba(0,0,0,.18);padding:10px;border-radius:4px;font-size:12px;">${esc(JSON.stringify(payload, null, 2))}</pre>
      <div>${esc(detail)}</div>`;
    const commandResultBody = (statusLabel = 'Value', statusText = sentValueText) => `
      <div style="display:grid;grid-template-columns:auto 1fr;gap:8px 12px;align-items:baseline;font-size:13px;">
        <div style="opacity:.65;">Tag path</div>
        <div style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all;">${esc(displayTagPath)}</div>
        <div style="opacity:.65;">${esc(statusLabel)}</div>
        <div style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all;">${esc(statusText)}</div>
      </div>`;
    const driverErrorMessage = (err: any): string => {
      const raw = String(err?.message || '').trim();
      if (!raw || /timeout|timed out|no responders|no response/i.test(raw)) return 'Driver did not respond';
      return raw;
    };
    const confirm = () => new Promise<boolean>(resolve => {
      if (!dialog) {
        dialog = document.createElement('div');
        dialog.className = 'stw-command-dialog';
        document.body.appendChild(dialog);
      }
      dialog.innerHTML = `
        <div style="width:min(520px,92vw);border:1px solid var(--border-color);border-radius:6px;background:var(--content-bg);color:var(--content-text);box-shadow:0 20px 50px rgba(0,0,0,.55);">
          <div style="padding:12px 14px;border-bottom:1px solid var(--border-color);font-weight:600;color:var(--accent-color);">Confirm command</div>
          <div style="padding:14px;display:flex;flex-direction:column;gap:14px;">
            ${commandResultBody()}
            <div style="display:flex;justify-content:flex-end;gap:8px;">
              <button class="stw-command-cancel" style="padding:5px 12px;border:1px solid var(--border-color);border-radius:4px;background:transparent;color:var(--content-text);cursor:pointer;">Cancel</button>
              <button class="stw-command-confirm" style="padding:5px 12px;border:1px solid color-mix(in srgb,var(--accent-color) 50%,transparent);border-radius:4px;background:color-mix(in srgb,var(--accent-color) 18%,transparent);color:var(--accent-color);cursor:pointer;">Confirm</button>
            </div>
          </div>
        </div>`;
      dialog.querySelector('.stw-command-cancel')?.addEventListener('click', () => {
        dialog?.remove();
        dialog = null;
        resolve(false);
      });
      dialog.querySelector('.stw-command-confirm')?.addEventListener('click', () => {
        resolve(true);
      });
    });

    if (!await confirm()) return;

    try {
      subject = this.commandSubject();
      show('Command pending', detailBody(`Waiting for response, timeout ${timeoutSeconds}s`));
      const result = await getMirrorStore().request(subject, payload, timeoutSeconds * 1000);
      const success = !!result?.success;
      const message = result?.message || (success ? 'The command succeeded' : 'The command failed');
      show(
        success ? 'Command succeeded' : 'Command failed',
        success ? commandResultBody() : commandResultBody('Status', message),
      );
      await createEventLogEntry({
        severity: success ? 'INFO' : 'ERROR',
        device: this.currentDeviceName(),
        message: `Command ${success ? 'succeeded' : 'failed'}: ${row.label} (${fullTagPath})`,
        params: { value: sentValue },
      });
    } catch (err: any) {
      const message = driverErrorMessage(err);
      show('Command failed', commandResultBody('Status', message));
      await createEventLogEntry({
        severity: 'ERROR',
        device: this.currentDeviceName(),
        message: `Command failed: ${row.label} (${fullTagPath})`,
        params: { value: sentValue },
      }).catch(() => {});
    }
  }

  // ── Config overlay ─────────────────────────────────────────────────────────

  closeConfig(): void {
    this.closeRowWizard();
    this._cfgOverlay?.remove();
    this._cfgOverlay = null;
    this.emit('widget-config-close', {});
  }

  // ── Config overlay rendering ───────────────────────────────────────────────

  private renderConfigOverlay(): void {
    if (!this._cfgOverlay) return;
    this.captureOpenRows();
    const cfg = this.config;

    const rowsHtml = cfg.rows.map((r, i) => this.renderRowEditor(r, i, cfg.rows.length)).join('');

    this._cfgOverlay.innerHTML = `
      <div id="stw-backdrop" style="position:fixed;inset:0;background:rgba(0,0,0,0.65);z-index:20000;display:flex;align-items:flex-start;justify-content:center;padding:2rem 1rem;overflow-y:auto;">
        <div style="width:min(760px,96vw);border:1px solid var(--accent-color);border-radius:6px;background:var(--content-bg);color:var(--content-text);box-shadow:0 24px 60px rgba(0,0,0,0.6);font-size:0.875rem;">

          <!-- Header -->
          <div style="display:flex;align-items:center;justify-content:space-between;padding:0.75rem 1rem;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 25%,var(--border-color));">
            <span style="font-size:0.875rem;font-weight:600;color:var(--accent-color);">Configure Status Table</span>
            <button id="stw-cfg-close" style="background:none;border:none;font-size:1.25rem;cursor:pointer;color:var(--content-text);opacity:0.6;line-height:1;">&times;</button>
          </div>

          <div style="padding:1rem;display:flex;flex-direction:column;gap:1rem;">

            <!-- General -->
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:0.75rem;">
              <div>
                <label style="${lblStyle}">Widget header</label>
                <input id="stw-header" type="text" value="${esc(cfg.headerText)}" style="${fldStyle}">
              </div>
              ${!(cfg as any).arrayElementPath ? `<div>
                <label style="${lblStyle}">Tag prefix (use * for dashboard device name)</label>
                <div style="display:flex;gap:0.25rem;">
                  <input id="stw-prefix" type="text" value="${esc(cfg.tagPrefix)}" placeholder="e.g. NASA" style="${fldStyle}flex:1;min-width:0;">
                  <button id="stw-browse-prefix" style="padding:0.25rem 0.5rem;font-size:0.875rem;background:color-mix(in srgb,var(--border-color) 40%,transparent);border:1px solid var(--border-color);border-radius:0.25rem;cursor:pointer;color:var(--content-text);">…</button>
                </div>
              </div>` : '<div></div>'}
              <div>
                <label style="${lblStyle}">Column 1 header</label>
                <input id="stw-col-1" type="text" value="${esc(cfg.colHeader1)}" placeholder="optional" style="${fldStyle}">
              </div>
              <div>
                <label style="${lblStyle}">Column 2 header</label>
                <input id="stw-col-2" type="text" value="${esc(cfg.colHeader2)}" placeholder="optional" style="${fldStyle}">
              </div>
              <div>
                <label style="${lblStyle}">Column 3 header</label>
                <input id="stw-col-3" type="text" value="${esc(cfg.colHeader3)}" placeholder="optional" style="${fldStyle}">
              </div>
              <div style="grid-column:1 / -1;">
                <label style="${lblStyle}">Hide widget when</label>
                <div style="display:grid;grid-template-columns:minmax(0,1fr) auto minmax(8rem,0.42fr);gap:0.5rem;align-items:center;">
                  <div style="display:flex;gap:0.25rem;min-width:0;">
                    <input id="stw-hide-path" type="text" value="${esc(cfg.hideTagPath)}" placeholder="relative tag path" style="${fldStyle}flex:1;min-width:0;">
                    <button class="stw-browse" data-target="stw-hide-path" style="padding:0.25rem 0.5rem;font-size:0.875rem;background:color-mix(in srgb,var(--border-color) 40%,transparent);border:1px solid var(--border-color);border-radius:0.25rem;cursor:pointer;color:var(--content-text);">…</button>
                  </div>
                  <span style="font-size:0.75rem;opacity:0.65;white-space:nowrap;">equals</span>
                  <input id="stw-hide-val" type="text" value="${esc(cfg.hideTagValue)}" placeholder="e.g. false" style="${fldStyle}">
                </div>
              </div>
            </div>

            <!-- Row list -->
            <div>
              <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.5rem;padding-bottom:0.375rem;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 18%,var(--border-color));">
                <span style="font-size:0.75rem;font-weight:600;color:var(--accent-color);letter-spacing:0.05em;text-transform:uppercase;">Rows</span>
                <div style="display:flex;align-items:center;gap:0.375rem;">
                  <button id="stw-row-wizard" type="button" style="font-size:0.8rem;padding:0.2rem 0.625rem;background:color-mix(in srgb,var(--border-color) 24%,transparent);color:var(--content-text);border:1px solid var(--border-color);border-radius:0.25rem;cursor:pointer;">Wizard…</button>
                  <button id="stw-add-row" type="button" style="font-size:0.8rem;padding:0.2rem 0.625rem;background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:0.25rem;cursor:pointer;">+ Add row</button>
                </div>
              </div>
              <div id="stw-row-list" style="display:flex;flex-direction:column;gap:0.375rem;">
                ${rowsHtml || '<div style="text-align:center;padding:1rem;opacity:0.35;font-size:0.8rem;">No rows yet - click + Add row</div>'}
              </div>
            </div>

            <!-- Actions -->
            <div style="display:flex;justify-content:flex-end;gap:0.5rem;padding-top:0.25rem;border-top:1px solid var(--border-color);">
              <button id="stw-cfg-cancel" style="padding:0.375rem 1rem;font-size:0.875rem;border:1px solid var(--border-color);border-radius:0.25rem;background:transparent;color:var(--content-text);cursor:pointer;">Cancel</button>
              <button id="stw-cfg-save" style="padding:0.375rem 1.25rem;font-size:0.875rem;font-weight:600;border:none;border-radius:0.25rem;background:var(--accent-color);color:var(--accent-text);cursor:pointer;">Save</button>
            </div>

          </div>
        </div>
      </div>
    `;

    this.attachConfigListeners();
  }

  private renderRowEditor(r: RowDef, idx: number, total: number): string {
    const openAttr = this._openRowIds.has(r.id) ? ' open' : '';
    return `
      <details class="stw-row-editor" data-id="${esc(r.id)}"${openAttr} style="border:1px solid var(--border-color);border-radius:0.25rem;background:var(--surface-tint);">
        <summary style="display:flex;align-items:center;gap:0.5rem;padding:0.5rem 0.625rem;cursor:pointer;">
          <span class="stw-row-toggle" aria-hidden="true"></span>
          <span style="flex:1;font-weight:600;">${esc(r.label || 'Row')}</span>
          <button class="stw-move-up" data-id="${esc(r.id)}" ${idx === 0 ? 'disabled' : ''} style="font-size:0.625rem;padding:1px 5px;">▲</button>
          <button class="stw-move-down" data-id="${esc(r.id)}" ${idx === total - 1 ? 'disabled' : ''} style="font-size:0.625rem;padding:1px 5px;">▼</button>
          <button class="stw-del-row" data-id="${esc(r.id)}" style="padding:0.2rem 0.45rem;color:#f87171;background:transparent;border:1px solid rgba(239,68,68,0.3);border-radius:0.25rem;cursor:pointer;">✕</button>
        </summary>
        <div style="padding:0.625rem;border-top:1px solid color-mix(in srgb,var(--border-color) 45%,transparent);display:flex;flex-direction:column;gap:0.7rem;">
          <div style="border:1px solid color-mix(in srgb,var(--border-color) 55%,transparent);border-radius:4px;padding:0.55rem;background:color-mix(in srgb,var(--content-bg) 28%,transparent);">
            <div style="display:grid;grid-template-columns:1fr auto;gap:0.5rem;align-items:center;">
              <input class="stw-r-label" type="text" value="${esc(r.label)}" placeholder="Row label" style="${fldStyle}">
              <label style="display:flex;align-items:center;gap:0.35rem;font-size:0.8rem;"><input class="stw-r-bold" type="checkbox" ${r.bold ? 'checked' : ''}> Bold</label>
            </div>
          </div>
          ${this.renderColumnEditor(r, 'col2', r.col2, 'Column 2')}
          ${this.renderColumnEditor(r, 'col3', r.col3, 'Column 3')}
        </div>
      </details>
    `;
  }

  private renderColumnEditor(row: RowDef, key: 'col2' | 'col3', c: ColumnDef, title: string): string {
    const showValue = c.type === 'value';
    const showCommand = c.type === 'command';
    const showBands = showValue && (c.formatter === 'text' || c.formatter === 'number' || c.formatter === 'bar');
    const showIconMap = showValue && c.formatter === 'icon';
    return `
      <div class="stw-col-editor" data-col="${key}" style="border:1px solid color-mix(in srgb,var(--border-color) 65%,transparent);border-radius:4px;padding:0.55rem;background:color-mix(in srgb,var(--content-bg) 22%,transparent);">
        <div style="${sectionTitleStyle};margin-bottom:0.45rem;">${title}</div>
        <div style="display:grid;grid-template-columns:auto 1fr 1fr;gap:0.5rem;align-items:end;">
          <div><label style="${lblStyle}">Type</label><select class="stw-c-type" style="${fldStyle}width:auto;"><option value="none" ${c.type === 'none' ? 'selected' : ''}>unused</option><option value="value" ${c.type === 'value' ? 'selected' : ''}>value</option><option value="command" ${c.type === 'command' ? 'selected' : ''}>command</option></select></div>
          ${showValue ? `<div><label style="${lblStyle}">Tag path</label><div style="display:flex;gap:0.2rem;"><input class="stw-c-tag" type="text" value="${esc(c.tagPath)}" style="${fldStyle}"><button class="stw-browse-row" data-id="${esc(row.id)}" data-col="${key}" style="padding:0.2rem 0.375rem;">…</button></div></div>
          <div><label style="${lblStyle}">Formatter</label><select class="stw-c-fmt" style="${fldStyle}">${(['text','number','bar','date/time','icon'] as Formatter[]).map(f => `<option value="${f}" ${c.formatter === f ? 'selected' : ''}>${f}</option>`).join('')}</select></div>` : ''}
          ${showCommand ? `<div><label style="${lblStyle}">Tag path</label><div style="display:flex;gap:0.2rem;"><input class="stw-c-command-path" type="text" value="${esc(c.commandPath || c.tagPath)}" style="${fldStyle}"><button class="stw-browse-row" data-id="${esc(row.id)}" data-col="${key}" data-target="command" style="padding:0.2rem 0.375rem;">…</button></div></div>
          <div><label style="${lblStyle}">Input field</label><select class="stw-c-input" style="${fldStyle}">${(['switch','text','number','enum','slider'] as CommandInputKind[]).map(f => `<option value="${f}" ${c.input === f ? 'selected' : ''}>${f}</option>`).join('')}</select></div>` : ''}
        </div>
        ${showValue ? `<div style="display:flex;gap:0.75rem;align-items:center;flex-wrap:wrap;margin-top:0.5rem;">
          ${c.formatter === 'bar' ? `<label style="font-size:0.8rem;">Upper <input class="stw-c-upper" type="number" value="${c.upperLimit}" style="${fldStyle}width:5rem;"></label>` : ''}
          ${showBands ? `<label style="display:flex;gap:0.3rem;align-items:center;font-size:0.8rem;"><input class="stw-c-bands-on" type="checkbox" ${c.colorBandsEnabled ? 'checked' : ''}> Color bands</label>
          <input type="color" class="stw-c-c1" value="${c.colorBandsColor1}"><input class="stw-c-t1" type="number" value="${c.colorBandsThreshold1}" style="${fldStyle}width:4rem;">
          <input type="color" class="stw-c-c2" value="${c.colorBandsColor2}"><input class="stw-c-t2" type="number" value="${c.colorBandsThreshold2}" style="${fldStyle}width:4rem;">
          <input type="color" class="stw-c-c3" value="${c.colorBandsColor3}">` : ''}
        </div>` : ''}
        ${showIconMap ? this.renderIconMapEditor(c) : ''}
        ${showCommand ? `<div style="display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:0.5rem;margin-top:0.5rem;">
          <div><label style="${lblStyle}">Default value</label><input class="stw-c-command-value" type="text" value="${esc(String(c.commandValue ?? ''))}" style="${fldStyle}"></div>
          <div><label style="${lblStyle}">Enum options</label><input class="stw-c-enum" type="text" value="${esc(c.enumOptions)}" placeholder="A,B,C" style="${fldStyle}"></div>
          <div><label style="${lblStyle}">Min</label><input class="stw-c-min" type="number" value="${c.min}" style="${fldStyle}"></div>
          <div><label style="${lblStyle}">Max</label><input class="stw-c-max" type="number" value="${c.max}" style="${fldStyle}"></div>
          <div><label style="${lblStyle}">Timeout</label><input class="stw-c-timeout" type="number" min="1" value="${c.timeoutSeconds}" style="${fldStyle}"></div>
        </div>` : ''}
      </div>`;
  }

  private renderIconMapEditor(c: ColumnDef): string {
    const entries = c.iconMap.length ? c.iconMap : [];
    return `
      <div style="margin-top:0.55rem;border-top:1px solid color-mix(in srgb,var(--border-color) 40%,transparent);padding-top:0.5rem;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.4rem;">
          <span style="${subsectionTitleStyle}">Value icons</span>
          <button class="stw-icon-map-add" type="button" style="font-size:0.75rem;padding:0.15rem 0.45rem;border:1px solid var(--border-color);border-radius:3px;background:color-mix(in srgb,var(--border-color) 20%,transparent);color:var(--content-text);cursor:pointer;">+ Add</button>
        </div>
        <div style="display:flex;flex-direction:column;gap:0.45rem;">
          ${entries.length ? entries.map((entry, idx) => `
            <div class="stw-icon-map-row" data-index="${idx}" style="display:grid;grid-template-columns:minmax(70px,0.85fr) minmax(160px,1.4fr) auto minmax(64px,0.45fr) minmax(92px,0.65fr) auto;gap:0.4rem;align-items:end;">
              <div><label style="${lblStyle}">Value</label><input class="stw-icon-value-input" type="text" value="${esc(entry.value)}" style="${fldStyle}"></div>
              <div><label style="${lblStyle}">Icon</label><icon-picker class="stw-icon-picker" value="${esc(entry.icon)}"></icon-picker></div>
              <div><label style="${lblStyle}">Color</label><input class="stw-icon-color" type="color" value="${esc(entry.color)}" style="width:34px;height:28px;padding:1px 2px;border:1px solid var(--border-color);border-radius:3px;background:transparent;"></div>
              <div><label style="${lblStyle}">Size</label><input class="stw-icon-size" type="number" min="10" max="96" value="${entry.size}" style="${fldStyle}"></div>
              <div><label style="${lblStyle}">Animation</label><select class="stw-icon-animation" style="${fldStyle}">${(['none','pulse','shake'] as IconAnimation[]).map(a => `<option value="${a}" ${entry.animation === a ? 'selected' : ''}>${a}</option>`).join('')}</select></div>
              <button class="stw-icon-map-del" type="button" data-index="${idx}" style="padding:0.25rem 0.45rem;color:#f87171;background:transparent;border:1px solid rgba(239,68,68,0.3);border-radius:0.25rem;cursor:pointer;">✕</button>
            </div>`).join('') : '<div style="font-size:0.75rem;opacity:0.45;">No icon mappings configured.</div>'}
        </div>
      </div>`;
  }

  private captureOpenRows(): void {
    const el = this._cfgOverlay;
    if (!el) return;
    el.querySelectorAll<HTMLDetailsElement>('.stw-row-editor').forEach(rowEl => {
      const id = rowEl.dataset.id;
      if (!id) return;
      if (rowEl.open) this._openRowIds.add(id);
      else this._openRowIds.delete(id);
    });
  }

  private attachConfigListeners(): void {
    const el = this._cfgOverlay;
    if (!el) return;

    el.querySelector('#stw-cfg-close')?.addEventListener('click', () => this.closeConfig());
    el.querySelector('#stw-cfg-cancel')?.addEventListener('click', () => this.closeConfig());

    el.querySelector('#stw-backdrop')?.addEventListener('click', (e) => {
      if ((e.target as Element).id === 'stw-backdrop') this.closeConfig();
    });

    el.querySelectorAll<HTMLDetailsElement>('.stw-row-editor').forEach(rowEl => {
      rowEl.addEventListener('toggle', () => {
        const id = rowEl.dataset.id;
        if (!id) return;
        if (rowEl.open) this._openRowIds.add(id);
        else this._openRowIds.delete(id);
      });
    });

    el.querySelectorAll<HTMLButtonElement>('.stw-row-editor > summary button').forEach(btn => {
      btn.addEventListener('click', (e) => e.stopPropagation());
    });

    el.querySelector('#stw-add-row')?.addEventListener('click', () => {
      this.collectFromOverlay();
      this.config.rows.push(rowFromConfig({ ...DEFAULT_ROW, id: genId(), col2: { ...DEFAULT_COLUMN }, col3: { ...DEFAULT_COLUMN, type: 'none' } }));
      this.renderConfigOverlay();
    });

    el.querySelector('#stw-row-wizard')?.addEventListener('click', () => this.openRowWizard());

    el.querySelectorAll('.stw-del-row').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = (btn as HTMLElement).dataset.id!;
        this.collectFromOverlay();
        this.config.rows = this.config.rows.filter(r => r.id !== id);
        this.renderConfigOverlay();
      });
    });

    el.querySelectorAll('.stw-move-up').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = (btn as HTMLElement).dataset.id!;
        this.collectFromOverlay();
        const idx = this.config.rows.findIndex(r => r.id === id);
        if (idx > 0) {
          [this.config.rows[idx - 1], this.config.rows[idx]] = [this.config.rows[idx], this.config.rows[idx - 1]];
          this.renderConfigOverlay();
        }
      });
    });

    el.querySelectorAll('.stw-move-down').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = (btn as HTMLElement).dataset.id!;
        this.collectFromOverlay();
        const idx = this.config.rows.findIndex(r => r.id === id);
        if (idx < this.config.rows.length - 1) {
          [this.config.rows[idx], this.config.rows[idx + 1]] = [this.config.rows[idx + 1], this.config.rows[idx]];
          this.renderConfigOverlay();
        }
      });
    });

    // Column type/formatter/input changes → re-render row editor to show/hide conditional fields
    el.querySelectorAll('.stw-c-type,.stw-c-fmt,.stw-c-input').forEach(sel => {
      sel.addEventListener('change', () => {
        this.collectFromOverlay();
        this.renderConfigOverlay();
      });
    });

    // Color bands toggle → re-render to enable/disable band controls
    el.querySelectorAll('.stw-c-bands-on').forEach(cb => {
      cb.addEventListener('change', () => {
        this.collectFromOverlay();
        this.renderConfigOverlay();
      });
    });

    el.querySelectorAll<HTMLElement>('.stw-icon-map-add').forEach(btn => {
      btn.addEventListener('click', () => {
        const rowEl = btn.closest<HTMLElement>('.stw-row-editor');
        const colEl = btn.closest<HTMLElement>('.stw-col-editor');
        const row = this.config.rows.find(r => r.id === rowEl?.dataset.id);
        const key = colEl?.dataset.col === 'col3' ? 'col3' : 'col2';
        this.collectFromOverlay();
        const targetRow = this.config.rows.find(r => r.id === row?.id);
        if (!targetRow) return;
        targetRow[key].iconMap.push({ value: '', icon: 'mdi:circle', color: '#60a5fa', size: 20, animation: 'none' });
        this.renderConfigOverlay();
      });
    });

    el.querySelectorAll<HTMLElement>('.stw-icon-map-del').forEach(btn => {
      btn.addEventListener('click', () => {
        const rowEl = btn.closest<HTMLElement>('.stw-row-editor');
        const colEl = btn.closest<HTMLElement>('.stw-col-editor');
        const rowId = rowEl?.dataset.id;
        const key = colEl?.dataset.col === 'col3' ? 'col3' : 'col2';
        const idx = Number(btn.dataset.index);
        this.collectFromOverlay();
        const targetRow = this.config.rows.find(r => r.id === rowId);
        if (!targetRow || !Number.isInteger(idx)) return;
        targetRow[key].iconMap.splice(idx, 1);
        this.renderConfigOverlay();
      });
    });

    // Browse button for tag prefix - pick a node
    el.querySelector<HTMLElement>('#stw-browse-prefix')?.addEventListener('click', () => {
      const currentPrefix = el.querySelector<HTMLInputElement>('#stw-prefix')?.value.trim() ?? '';
      const expandTo = this.resolveBrowsePrefix(currentPrefix);
      getTreeBrowserDialog().open('', 'Select Node', (path) => {
        const input = el.querySelector<HTMLInputElement>('#stw-prefix');
        if (input) input.value = path;
      }, /* includeLeaves= */ false, expandTo);
    });

    // Browse buttons for hide-tag path
    el.querySelectorAll<HTMLElement>('.stw-browse').forEach(btn => {
      btn.addEventListener('click', () => {
        const targetId = btn.dataset.target!;
        const currentValue = el.querySelector<HTMLInputElement>(`#${targetId}`)?.value.trim() ?? '';
        const prefixInput = el.querySelector<HTMLInputElement>('#stw-prefix');
        const prefix = prefixInput ? prefixInput.value.trim() : this.config.tagPrefix;
        const resolvedPrefix = this.resolveBrowsePrefix(prefix);
        const selectedPath = this.resolveTagPathForPrefix(currentValue, resolvedPrefix);
        getTreeBrowserDialog().open('', 'Select Tag', (path) => {
          const input = el.querySelector<HTMLInputElement>(`#${targetId}`);
          if (input) input.value = path;
        }, true, selectedPath, selectedPath);
      });
    });

    // Browse buttons per row - open tree at the resolved prefix and strip it
    el.querySelectorAll<HTMLElement>('.stw-browse-row').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = btn.dataset.id!;
        const colKey = (btn.dataset.col === 'col3' ? 'col3' : 'col2') as 'col2' | 'col3';
        // When inside an array widget, #stw-prefix is hidden; fall back to the
        // config's tagPrefix (which the array widget sets to the element path).
        const prefixInput = el.querySelector<HTMLInputElement>('#stw-prefix');
        const prefix = prefixInput ? prefixInput.value.trim() : this.config.tagPrefix;
        const resolved = this.resolveBrowsePrefix(prefix);
        const rowEl = el.querySelector<HTMLElement>(`.stw-row-editor[data-id="${id}"]`);
        const colEl = rowEl?.querySelector<HTMLElement>(`.stw-col-editor[data-col="${colKey}"]`);
        const input = btn.dataset.target === 'command'
          ? colEl?.querySelector<HTMLInputElement>('.stw-c-command-path')
          : colEl?.querySelector<HTMLInputElement>('.stw-c-tag');
        const currentTagPath = input?.value.trim() ?? '';
        const selectedPath = this.resolveTagPathForPrefix(currentTagPath, resolved);
        getTreeBrowserDialog().open(resolved, 'Select Tag', (path) => {
          if (!input) return;
          if (resolved) {
            const resolvedDot = resolved + '.';
            if (path.startsWith(resolvedDot)) {
              input.value = path.slice(resolvedDot.length);
              return;
            }
          }
          input.value = path;
        }, true, selectedPath, selectedPath);
      });
    });

    el.querySelector('#stw-cfg-save')?.addEventListener('click', () => {
      this.collectFromOverlay();
      const changed = JSON.stringify(this.config) !== this._cfgSnapshot;
      this.closeConfig();
      this.emit('widget-config-save', { config: this.config, forceDirty: changed });
      this.rerender();
    });
  }

  private openRowWizard(): void {
    const el = this._cfgOverlay;
    if (!el) return;
    this.collectFromOverlay();
    const prefixInput = el.querySelector<HTMLInputElement>('#stw-prefix');
    const prefix = prefixInput ? prefixInput.value.trim() : this.config.tagPrefix;
    const resolvedPrefix = this.resolveBrowsePrefix(prefix);
    this.renderRowWizardPicker(resolvedPrefix);
  }

  private renderRowWizardPicker(resolvedPrefix: string): void {
    this.closeRowWizard();
    const store = getMirrorStore();
    const absPrefix = store.toAbsolute(resolvedPrefix);
    const childNames = store.listChildrenNames(absPrefix).sort((a, b) => a.localeCompare(b));
    const topLevelTags = childNames.filter(name => store.getNodeType(`${absPrefix}.${name}`) !== 'node');
    const groups = childNames.filter(name => store.getNodeType(`${absPrefix}.${name}`) === 'node');
    const options = [
      ...(topLevelTags.length ? [{ label: 'Top Level Tags', path: resolvedPrefix, count: topLevelTags.length }] : []),
      ...groups.map(name => ({
        label: tagNameToLabel(name),
        path: resolvedPrefix ? `${resolvedPrefix}.${name}` : name,
        count: store.listChildrenNames(`${absPrefix}.${name}`).filter(child => store.getNodeType(`${absPrefix}.${name}.${child}`) !== 'node').length,
      })),
    ];

    this._wizardOverlay = document.createElement('div');
    this._wizardOverlay.innerHTML = `
      <div class="stw-wizard-backdrop" style="position:fixed;inset:0;background:rgba(0,0,0,0.55);z-index:24000;display:flex;align-items:center;justify-content:center;padding:1rem;">
        <div style="width:min(420px,94vw);max-height:80vh;display:flex;flex-direction:column;border:1px solid var(--border-color);border-radius:6px;background:var(--content-bg);color:var(--content-text);box-shadow:0 18px 48px rgba(0,0,0,0.55);">
          <div style="display:flex;align-items:center;justify-content:space-between;padding:0.75rem 0.9rem;border-bottom:1px solid var(--border-color);">
            <span style="font-size:0.9rem;font-weight:650;color:var(--accent-color);">Select Tag Group</span>
            <button class="stw-wizard-close" type="button" style="background:none;border:none;color:var(--content-text);font-size:1.25rem;line-height:1;opacity:0.6;cursor:pointer;">&times;</button>
          </div>
          <div style="padding:0.65rem;overflow:auto;">
            ${options.length ? options.map(opt => `
              <button class="stw-wizard-option" type="button" data-path="${esc(opt.path)}" style="width:100%;display:flex;align-items:center;justify-content:space-between;gap:0.75rem;text-align:left;margin-bottom:0.35rem;padding:0.55rem 0.65rem;border:1px solid var(--border-color);border-radius:4px;background:color-mix(in srgb,var(--border-color) 12%,transparent);color:var(--content-text);cursor:pointer;">
                <span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${esc(opt.label)}</span>
                <span style="font-size:0.72rem;opacity:0.55;white-space:nowrap;">${opt.count} tag${opt.count === 1 ? '' : 's'}</span>
              </button>
            `).join('') : '<div style="padding:1rem;text-align:center;font-size:0.8rem;opacity:0.45;">No tags or tag groups found.</div>'}
          </div>
        </div>
      </div>
    `;
    document.body.appendChild(this._wizardOverlay);
    this._wizardOverlay.querySelector('.stw-wizard-close')?.addEventListener('click', () => this.closeRowWizard());
    this._wizardOverlay.querySelector('.stw-wizard-backdrop')?.addEventListener('click', (e) => {
      if ((e.target as HTMLElement).classList.contains('stw-wizard-backdrop')) this.closeRowWizard();
    });
    this._wizardOverlay.querySelectorAll<HTMLElement>('.stw-wizard-option').forEach(btn => {
      btn.addEventListener('click', () => {
        const path = btn.dataset.path ?? resolvedPrefix;
        this.appendRowsFromGroup(path, resolvedPrefix);
        this.closeRowWizard();
        this.renderConfigOverlay();
      });
    });
  }

  private closeRowWizard(): void {
    this._wizardOverlay?.remove();
    this._wizardOverlay = null;
  }

  private appendRowsFromGroup(groupPath: string, resolvedPrefix: string): void {
    const store = getMirrorStore();
    const absGroupPath = store.toAbsolute(groupPath);
    const tagNames = store.listChildrenNames(absGroupPath)
      .filter(name => store.getNodeType(`${absGroupPath}.${name}`) !== 'node');
    const relGroupPath = this.relativePathForPrefix(groupPath, resolvedPrefix);
    const newRows = tagNames.map(tagName => {
      const tagPath = relGroupPath ? `${relGroupPath}.${tagName}` : tagName;
      const fullTagPath = `${absGroupPath}.${tagName}`;
      const formatter = guessFormatter(store.getNodeValue(fullTagPath));
      return rowFromConfig({
        ...DEFAULT_ROW,
        id: genId(),
        label: tagNameToLabel(tagName),
        tagPath,
        formatter,
        col2: { ...DEFAULT_COLUMN, type: 'value', tagPath, formatter },
        col3: { ...DEFAULT_COLUMN, type: 'none' },
      });
    });
    this.config.rows.push(...newRows);
    newRows.forEach(row => this._openRowIds.add(row.id));
  }

  private relativePathForPrefix(path: string, prefix: string): string {
    const store = getMirrorStore();
    const cleanPath = store.toRelative(store.toAbsolute(path)).replace(/\.+$/g, '');
    const cleanPrefix = prefix ? store.toRelative(store.toAbsolute(prefix)).replace(/\.+$/g, '') : '';
    if (!cleanPrefix) return cleanPath;
    if (cleanPath === cleanPrefix) return '';
    const prefixDot = cleanPrefix + '.';
    return cleanPath.startsWith(prefixDot) ? cleanPath.slice(prefixDot.length) : cleanPath;
  }

  /** Read current form values back into this.config (without saving). */
  private collectFromOverlay(): void {
    const el = this._cfgOverlay;
    if (!el) return;

    this.config.headerText = (el.querySelector<HTMLInputElement>('#stw-header')?.value ?? '').trim();
    // Only update tagPrefix from the form when the input is visible (i.e. not
    // inside an array widget where the field is hidden). Otherwise we'd clobber
    // the element path that the array widget injected via setConfig().
    const prefixEl = el.querySelector<HTMLInputElement>('#stw-prefix');
    if (prefixEl) this.config.tagPrefix = prefixEl.value.trim();
    this.config.colHeader1 = el.querySelector<HTMLInputElement>('#stw-col-1')?.value ?? '';
    this.config.colHeader2 = el.querySelector<HTMLInputElement>('#stw-col-2')?.value ?? '';
    this.config.colHeader3 = el.querySelector<HTMLInputElement>('#stw-col-3')?.value ?? '';
    this.config.hideTagPath = el.querySelector<HTMLInputElement>('#stw-hide-path')?.value ?? '';
    this.config.hideTagValue = el.querySelector<HTMLInputElement>('#stw-hide-val')?.value ?? '';

    el.querySelectorAll<HTMLElement>('.stw-row-editor').forEach(rowEl => {
      const id = rowEl.dataset.id!;
      const row = this.config.rows.find(r => r.id === id);
      if (!row) return;

      row.label = rowEl.querySelector<HTMLInputElement>('.stw-r-label')?.value ?? row.label;
      row.bold = rowEl.querySelector<HTMLInputElement>('.stw-r-bold')?.checked ?? false;
      rowEl.querySelectorAll<HTMLElement>('.stw-col-editor').forEach(colEl => {
        const key = (colEl.dataset.col === 'col3' ? 'col3' : 'col2') as 'col2' | 'col3';
        const col = normalizeColumn(row[key]);
        col.type = (colEl.querySelector<HTMLSelectElement>('.stw-c-type')?.value ?? col.type) as ColumnKind;
        col.tagPath = colEl.querySelector<HTMLInputElement>('.stw-c-tag')?.value ?? col.tagPath;
        col.formatter = (colEl.querySelector<HTMLSelectElement>('.stw-c-fmt')?.value ?? col.formatter) as Formatter;
        col.invert = colEl.querySelector<HTMLInputElement>('.stw-c-invert')?.checked ?? col.invert;
        col.upperLimit = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-upper')?.value ?? String(col.upperLimit)) || 100;
        col.colorBandsEnabled = colEl.querySelector<HTMLInputElement>('.stw-c-bands-on')?.checked ?? col.colorBandsEnabled;
        col.colorBandsThreshold1 = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-t1')?.value ?? String(col.colorBandsThreshold1)) || 50;
        col.colorBandsThreshold2 = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-t2')?.value ?? String(col.colorBandsThreshold2)) || 80;
        col.colorBandsColor1 = colEl.querySelector<HTMLInputElement>('.stw-c-c1')?.value ?? col.colorBandsColor1;
        col.colorBandsColor2 = colEl.querySelector<HTMLInputElement>('.stw-c-c2')?.value ?? col.colorBandsColor2;
        col.colorBandsColor3 = colEl.querySelector<HTMLInputElement>('.stw-c-c3')?.value ?? col.colorBandsColor3;
        col.iconMap = Array.from(colEl.querySelectorAll<HTMLElement>('.stw-icon-map-row')).map(mapEl => {
          const picker = mapEl.querySelector<any>('.stw-icon-picker');
          const animation = mapEl.querySelector<HTMLSelectElement>('.stw-icon-animation')?.value;
          return {
            value: mapEl.querySelector<HTMLInputElement>('.stw-icon-value-input')?.value ?? '',
            icon: picker?.value ?? picker?.getAttribute('value') ?? '',
            color: mapEl.querySelector<HTMLInputElement>('.stw-icon-color')?.value ?? '#60a5fa',
            size: parseFloat(mapEl.querySelector<HTMLInputElement>('.stw-icon-size')?.value ?? '20') || 20,
            animation: (animation === 'pulse' || animation === 'shake') ? animation : 'none',
          };
        });
        col.commandPath = colEl.querySelector<HTMLInputElement>('.stw-c-command-path')?.value ?? col.commandPath;
        col.input = normalizeCommandInput(colEl.querySelector<HTMLSelectElement>('.stw-c-input')?.value);
        col.commandValue = colEl.querySelector<HTMLInputElement>('.stw-c-command-value')?.value ?? col.commandValue;
        col.enumOptions = colEl.querySelector<HTMLInputElement>('.stw-c-enum')?.value ?? col.enumOptions;
        col.min = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-min')?.value ?? String(col.min)) || 0;
        col.max = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-max')?.value ?? String(col.max)) || 100;
        col.timeoutSeconds = parseFloat(colEl.querySelector<HTMLInputElement>('.stw-c-timeout')?.value ?? String(col.timeoutSeconds)) || 10;
        row[key] = col;
      });
      row.tagPath = row.col2.tagPath;
      row.formatter = row.col2.formatter;
      row.invert = row.col2.invert;
      row.upperLimit = row.col2.upperLimit;
      row.colorBandsEnabled = row.col2.colorBandsEnabled;
      row.colorBandsThreshold1 = row.col2.colorBandsThreshold1;
      row.colorBandsThreshold2 = row.col2.colorBandsThreshold2;
      row.colorBandsColor1 = row.col2.colorBandsColor1;
      row.colorBandsColor2 = row.col2.colorBandsColor2;
      row.colorBandsColor3 = row.col2.colorBandsColor3;
    });
  }
}

customElements.define('status-table-widget', StatusTableWidget);
