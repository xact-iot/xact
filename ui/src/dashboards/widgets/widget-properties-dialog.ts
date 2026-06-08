import { BaseComponent } from '../../components/base-component';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import '../../components/icon-picker';

export interface PropertyField {
  name: string;
  /**
   * 'path'        - text input + Browse button (single node/tag path)
   *                 Set context.includeLeaves = true to allow selecting tag leaves.
   * 'path-list'   - ordered list of paths, each with Browse + Remove; supports Add
   * 'column-list' - ordered list of column definitions (header, tagPath, width, formatter)
   *                 Rendered as a collapsible section with reordering support.
   * 'color'       - native color picker (<input type="color">)
   * 'section'     - collapsible section divider; not submitted, groups following fields
   * 'select'      - dropdown; provide options via context.options: Array<{value,label}>
   */
  type: 'boolean' | 'string' | 'number' | 'path' | 'path-list' | 'column-list' | 'color' | 'icon' | 'section' | 'select';
  label: string;
  description?: string;
  default?: any;
  /** Optional context passed to the field renderer. Used by column-list to know
   *  how many path segments to strip when a tag is picked from the tree browser. */
  context?: Record<string, any>;
}

export class WidgetPropertiesDialog extends BaseComponent {
  private isOpen = false;
  private widgetId = '';
  private dialogTitle = 'Widget Properties';
  private properties: PropertyField[] = [];
  private values: Record<string, any> = {};
  private readOnly = false;

  /** Per column-list prop: true = expanded (default), false = collapsed. */
  private collapseState: Record<string, boolean> = {};

  // Portal element rendered directly on document.body to escape nested
  // stacking contexts created by GridStack transforms or Leaflet panes.
  private portalEl: HTMLDivElement | null = null;

  private getRoot(): Element { return this.portalEl ?? this; }

  open(widgetId: string, schema: PropertyField[], currentValues: Record<string, any>, readOnly = false, title = 'Widget Properties'): void {
    this.widgetId = widgetId;
    this.dialogTitle = title || 'Widget Properties';
    this.properties = schema;
    this.values = { ...currentValues };
    this.readOnly = readOnly;
    this.collapseState = {}; // all sections start expanded
    this.isOpen = true;
    this.rerender();
  }

  close(): void {
    this.isOpen = false;
    this.emit('properties-closed', { widgetId: this.widgetId });
    this.rerender();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.portalEl?.remove();
    this.portalEl = null;
  }

  protected render(): void {
    if (!this.isOpen) {
      this.portalEl?.remove();
      this.portalEl = null;
      return;
    }

    if (!this.portalEl) {
      this.portalEl = document.createElement('div');
      document.body.appendChild(this.portalEl);
    }

    const S = `border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)`;
    const SBtn = `border-color:var(--border-color);background:color-mix(in srgb,var(--border-color) 30%,transparent);color:var(--content-text)`;
    const disabled = this.readOnly ? 'disabled' : '';
    const readOnlyStyle = this.readOnly ? 'opacity:0.65;' : '';
    const formatterOptions = [
      { value: 'text',   label: 'Text (truncated)'  },
      { value: 'number', label: 'Number (≤2 dec)'   },
      { value: 'okfail', label: '✓ OK / ✗ Fail'     },
      { value: 'cross',  label: '✗ Cross if truthy' },
    ];

    // ── Group flat property list into sections ────────────────────────────────
    interface FieldGroup { key: string; label: string | null; desc: string | null; fields: PropertyField[]; }
    const groups: FieldGroup[] = [];
    let curGroup: FieldGroup = { key: '__root__', label: null, desc: null, fields: [] };
    for (const prop of this.properties) {
      if (prop.type === 'section') {
        if (curGroup.fields.length > 0 || curGroup.label !== null) groups.push(curGroup);
        curGroup = { key: prop.name, label: prop.label, desc: prop.description ?? null, fields: [] };
      } else {
        curGroup.fields.push(prop);
      }
    }
    if (curGroup.fields.length > 0 || curGroup.label !== null) groups.push(curGroup);

    // ── Render one field ──────────────────────────────────────────────────────
    const renderField = (prop: PropertyField): string => {
      const value = this.values[prop.name] ?? prop.default;
      const fieldDisabled = this.readOnly || this.isDisabledByDependency(prop);
      const disabledAttr = fieldDisabled ? 'disabled' : '';
      const fieldReadOnlyStyle = fieldDisabled ? 'opacity:0.65;' : readOnlyStyle;

      // ── column-list renders its own labelled collapsible section ─────────────
      if (prop.type === 'column-list') {
        const isOpen = this.collapseState[prop.name] ?? true;
        const cols: Array<{ header: string; tagPath: string; formatter: string; width?: string }> =
          Array.isArray(value) ? value : [];
        const parentDepth: number = prop.context?.parentNodeDepth ?? 0;

        const colRows = cols.map((col, i) => `
          <div class="wpd-col-row mb-2 rounded border"
               style="border-color:var(--border-color);background:color-mix(in srgb,var(--border-color) 8%,transparent);"
               data-prop="${prop.name}" data-index="${i}">

            <!-- Row 1: order controls + header input + remove -->
            <div class="flex items-center gap-1 px-2 pt-2 pb-1">
              <span class="text-xs select-none flex-shrink-0"
                    style="color:var(--footer-text);min-width:1.25rem;text-align:center;">${i + 1}</span>
              <button type="button" class="wpd-move-col-up-btn px-1 py-0.5 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}"
                      title="Move up" ${this.readOnly || i === 0 ? 'disabled' : ''}>↑</button>
              <button type="button" class="wpd-move-col-down-btn px-1 py-0.5 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}"
                      title="Move down" ${this.readOnly || i === cols.length - 1 ? 'disabled' : ''}>↓</button>
              <input type="text" class="wpd-col-header flex-1 px-2 py-1 text-xs rounded border"
                     style="${S};${readOnlyStyle}" data-prop="${prop.name}" data-index="${i}" ${disabled}
                     placeholder="Column heading" value="${this.esc(col.header)}">
              <button type="button" class="wpd-remove-col-btn px-2 py-1 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}"
                      title="Remove column" ${disabled}>✕</button>
            </div>

            <!-- Row 2: relative tag path + browse -->
            <div class="flex items-center gap-1 px-2 pb-1">
              <span class="text-xs opacity-50 flex-shrink-0"
                    style="color:var(--footer-text);min-width:3.5rem">Tag path</span>
              <input type="text" class="wpd-col-tagpath flex-1 px-2 py-1 text-xs rounded border font-mono"
                     style="${S};${readOnlyStyle}" data-prop="${prop.name}" data-index="${i}" ${disabled}
                     data-parent-depth="${parentDepth}"
                     placeholder="e.g. sign.message" value="${this.esc(col.tagPath)}">
              <button type="button" class="wpd-browse-col-btn px-2 py-1 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}"
                      data-parent-depth="${parentDepth}" title="Browse tags" ${disabled}>…</button>
            </div>

            <!-- Row 3: formatter + width -->
            <div class="flex items-center gap-1 px-2 pb-2">
              <span class="text-xs opacity-50 flex-shrink-0"
                    style="color:var(--footer-text);min-width:3.5rem">Format</span>
              <select class="wpd-col-formatter flex-1 px-2 py-1 text-xs rounded border"
                      style="${S};${readOnlyStyle}" data-prop="${prop.name}" data-index="${i}" ${disabled}>
                ${formatterOptions.map(o =>
                  `<option value="${o.value}"${col.formatter === o.value ? ' selected' : ''}>${o.label}</option>`,
                ).join('')}
              </select>
              <span class="text-xs opacity-50 flex-shrink-0 ml-2"
                    style="color:var(--footer-text)">Width</span>
              <input type="text" class="wpd-col-width px-2 py-1 text-xs rounded border font-mono"
                     style="${S};width:5rem;${readOnlyStyle}" data-prop="${prop.name}" data-index="${i}" ${disabled}
                     placeholder="e.g. 8rem" value="${this.esc(col.width ?? '')}">
            </div>
          </div>
        `).join('');

        return `
          <div class="mb-3">
            <!-- Collapsible section header -->
            <button type="button"
                    class="wpd-col-section-hdr w-full flex items-center gap-2 px-2 py-1.5 rounded text-left"
                    style="background:color-mix(in srgb,var(--border-color) 18%,transparent);"
                    data-prop="${prop.name}">
              <span class="text-xs select-none" style="color:var(--accent-color)">${isOpen ? '▼' : '▶'}</span>
              <span class="text-sm font-semibold flex-1">${prop.label}</span>
              <span class="text-xs opacity-50">${cols.length} col${cols.length !== 1 ? 's' : ''}</span>
            </button>
            ${prop.description ? `<div class="text-xs opacity-50 px-2 pb-1">${prop.description}</div>` : ''}
            <div class="wpd-col-section-body mt-1" ${isOpen ? '' : 'style="display:none"'}>
              <div class="wpd-col-list" data-prop="${prop.name}">
                ${colRows}
                <button type="button"
                        class="wpd-add-col-btn mt-1 px-3 py-1 text-xs rounded border w-full"
                        style="border-color:var(--border-color);background:color-mix(in srgb,var(--border-color) 12%,transparent);color:var(--accent-color)"
                        data-prop="${prop.name}" ${disabled}>+ Add column</button>
              </div>
            </div>
          </div>
        `;
      }

      // ── All other field types use the standard label + input wrapper ──────────
      let inputHtml = '';

      switch (prop.type) {
        case 'boolean':
          inputHtml = `
            <input type="checkbox" id="prop-${prop.name}" name="${prop.name}"
                   class="w-4 h-4 rounded" ${value ? 'checked' : ''} ${disabledAttr}>
          `;
          break;

        case 'number':
          const minAttr = prop.context?.min !== undefined ? ` min="${this.esc(String(prop.context.min))}"` : '';
          const maxAttr = prop.context?.max !== undefined ? ` max="${this.esc(String(prop.context.max))}"` : '';
          const stepAttr = prop.context?.step !== undefined ? ` step="${this.esc(String(prop.context.step))}"` : ' step="any"';
          inputHtml = `
            <input type="number" id="prop-${prop.name}" name="${prop.name}"
                   class="w-full px-2 py-1 text-sm rounded border" style="${S};${fieldReadOnlyStyle}" ${disabledAttr}
                   value="${value ?? ''}"${minAttr}${maxAttr}${stepAttr}>
          `;
          break;

        case 'path':
          inputHtml = `
            <div class="flex gap-1">
              <input type="text" id="prop-${prop.name}" name="${prop.name}"
                     class="flex-1 px-2 py-1 text-sm rounded border font-mono" style="${S};${fieldReadOnlyStyle}" ${disabledAttr}
                     value="${this.esc(value ?? '')}" placeholder="e.g. building.floor1">
              <button type="button" class="wpd-browse-btn px-2 py-1 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" ${disabledAttr}>Browse…</button>
            </div>
          `;
          break;

        case 'color':
          inputHtml = `
            <div class="flex items-center gap-2">
              <input type="color" id="prop-${prop.name}" name="${prop.name}"
                     value="${this.esc(value || '#ffffff')}"
                     style="width:2.5rem;height:2rem;padding:2px 3px;border-radius:4px;cursor:pointer;border:1px solid var(--border-color);background:var(--content-bg);${fieldReadOnlyStyle}" ${disabledAttr}>
              <span class="text-xs font-mono opacity-60">${this.esc(value || '#ffffff')}</span>
            </div>
          `;
          break;

        case 'path-list': {
          const items: string[] = Array.isArray(value) ? value : [];
          const rows = items.map((item, i) => `
            <div class="flex gap-1 mb-1 wpd-list-row" data-prop="${prop.name}" data-index="${i}">
              <input type="text" class="wpd-list-input flex-1 px-2 py-1 text-sm rounded border font-mono"
                     style="${S};${readOnlyStyle}" data-prop="${prop.name}" data-index="${i}" ${disabled}
                     value="${this.esc(item)}" placeholder="node.path">
              <button type="button" class="wpd-browse-list-btn px-2 py-1 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}" ${disabled}>Browse…</button>
              <button type="button" class="wpd-remove-list-btn px-2 py-1 text-xs rounded border flex-shrink-0"
                      style="${SBtn}" data-prop="${prop.name}" data-index="${i}" title="Remove" ${disabled}>✕</button>
            </div>
          `).join('');

          inputHtml = `
            <div class="wpd-list-wrap" data-prop="${prop.name}">
              ${rows}
              <button type="button" class="wpd-add-list-btn mt-1 px-3 py-1 text-xs rounded border"
                      style="border-color:var(--border-color);background:color-mix(in srgb,var(--border-color) 20%,transparent);color:var(--accent-color)"
                      data-prop="${prop.name}" ${disabled}>+ Add node</button>
            </div>
          `;
          break;
        }

        case 'icon':
          inputHtml = `
            <icon-picker id="prop-${prop.name}" value="${this.esc(value ?? '')}" ${disabledAttr}></icon-picker>
          `;
          break;

        case 'select': {
          const opts: Array<{ value: string; label: string }> = prop.context?.options ?? [];
          inputHtml = `
            <select id="prop-${prop.name}" name="${prop.name}"
                    class="w-full px-2 py-1 text-sm rounded border" style="${S};${fieldReadOnlyStyle}" ${disabledAttr}>
              ${opts.map(o =>
                `<option value="${this.esc(o.value)}"${String(value ?? '') === String(o.value) ? ' selected' : ''}>${this.esc(o.label)}</option>`,
              ).join('')}
            </select>
          `;
          break;
        }

        case 'string':
        default:
          inputHtml = `
            <input type="text" id="prop-${prop.name}" name="${prop.name}"
                   class="w-full px-2 py-1 text-sm rounded border" style="${S};${fieldReadOnlyStyle}" ${disabledAttr}
                   value="${this.esc(value ?? '')}">
          `;
          break;
      }

      return `
        <div class="mb-4">
          <div class="flex items-center gap-2 mb-1">
            <label for="prop-${prop.name}" class="text-sm font-medium">${prop.label}</label>
          </div>
          ${prop.description ? `<div class="text-xs opacity-60 mb-2">${prop.description}</div>` : ''}
          ${inputHtml}
        </div>
      `;
    }; // end renderField

    // ── Render groups, wrapping named sections in a collapsible container ─────
    const fieldsHtml = groups.map(group => {
      const fieldRows = group.fields.map(renderField).join('');
      if (group.label === null) return fieldRows; // root group - no wrapper

      const stateKey = `section:${group.key}`;
      const isOpen = this.collapseState[stateKey] ?? false; // default collapsed
      const sectionStyle = [
        'background:linear-gradient(90deg,color-mix(in srgb,var(--accent-color) 20%,var(--content-bg)),color-mix(in srgb,var(--border-color) 18%,transparent))',
        'border:1px solid color-mix(in srgb,var(--accent-color) 26%,var(--border-color))',
        'border-left:4px solid var(--accent-color)',
        'box-shadow:0 1px 0 rgba(255,255,255,0.04) inset',
      ].join(';');
      return `
        <div class="mb-3">
          <button type="button"
                  class="wpd-section-hdr w-full flex items-center gap-2 px-3 py-2 rounded text-left"
                  style="${sectionStyle};"
                  data-section="${group.key}">
            <span class="text-xs select-none flex items-center justify-center rounded"
                  style="width:1.25rem;height:1.25rem;color:var(--accent-color);background:color-mix(in srgb,var(--content-bg) 70%,transparent);border:1px solid color-mix(in srgb,var(--accent-color) 22%,transparent);">${isOpen ? '▼' : '▶'}</span>
            <span class="text-sm font-semibold flex-1"
                  style="letter-spacing:0.01em;color:color-mix(in srgb,var(--content-text) 90%,var(--accent-color));">${group.label}</span>
          </button>
          ${group.desc ? `<div class="text-xs opacity-50 px-2 pb-1">${group.desc}</div>` : ''}
          <div class="wpd-section-body pl-2 pt-1" ${isOpen ? '' : 'style="display:none"'}>
            ${fieldRows}
          </div>
        </div>
      `;
    }).join('');

    this.portalEl.innerHTML = `
      <div class="fixed inset-0 flex items-center justify-center"
           style="background:rgba(0,0,0,.5);z-index:22000;">
        <div class="rounded-lg shadow-xl p-6 w-full max-w-md"
             style="background:var(--content-bg);color:var(--content-text);
                    border:1px solid var(--border-color);
                    max-height:90vh;display:flex;flex-direction:column;">
          <div class="flex items-center justify-between mb-4 flex-shrink-0">
            <div>
              <h3 class="text-lg font-semibold">${this.esc(this.dialogTitle)}</h3>
              ${this.readOnly ? `<div class="text-xs opacity-50">Read only</div>` : ''}
            </div>
            <button id="wpd-close" class="text-xl opacity-60 hover:opacity-100">&times;</button>
          </div>
          <form id="wpd-form" style="overflow-y:auto;flex:1;padding-right:0.25rem;">
            ${fieldsHtml}
            <div class="flex gap-2 mt-6">
              ${this.readOnly ? '' : `
              <button type="submit" class="flex-1 px-4 py-2 text-sm font-semibold rounded"
                      style="background:var(--accent-color);color:var(--accent-text)">Apply</button>
              `}
              <button type="button" id="wpd-cancel" class="flex-1 px-4 py-2 text-sm font-semibold rounded"
                      style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">${this.readOnly ? 'Close' : 'Cancel'}</button>
            </div>
          </form>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    const root = this.getRoot();
    root.querySelector('#wpd-close')?.addEventListener('click', this.handleClose);
    root.querySelector('#wpd-cancel')?.addEventListener('click', this.handleClose);
    root.querySelector('#wpd-form')?.addEventListener('submit', this.handleSubmit);
    if (this.readOnly) return;
    root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]').forEach(b => b.addEventListener('change', this.handleDependencyChange));
    root.querySelectorAll('.wpd-browse-btn').forEach(b => b.addEventListener('click', this.handleBrowse));
    root.querySelectorAll('.wpd-browse-list-btn').forEach(b => b.addEventListener('click', this.handleBrowseList));
    root.querySelectorAll('.wpd-remove-list-btn').forEach(b => b.addEventListener('click', this.handleRemoveListItem));
    root.querySelectorAll('.wpd-add-list-btn').forEach(b => b.addEventListener('click', this.handleAddListItem));
    root.querySelectorAll('.wpd-section-hdr').forEach(b => b.addEventListener('click', this.handleToggleSection));
    root.querySelectorAll('.wpd-col-section-hdr').forEach(b => b.addEventListener('click', this.handleToggleColSection));
    root.querySelectorAll('.wpd-browse-col-btn').forEach(b => b.addEventListener('click', this.handleBrowseColTag));
    root.querySelectorAll('.wpd-remove-col-btn').forEach(b => b.addEventListener('click', this.handleRemoveColumn));
    root.querySelectorAll('.wpd-add-col-btn').forEach(b => b.addEventListener('click', this.handleAddColumn));
    root.querySelectorAll('.wpd-move-col-up-btn').forEach(b => b.addEventListener('click', this.handleMoveColumnUp));
    root.querySelectorAll('.wpd-move-col-down-btn').forEach(b => b.addEventListener('click', this.handleMoveColumnDown));
  }

  protected detachEventListeners(): void {
    const root = this.getRoot();
    root.querySelector('#wpd-close')?.removeEventListener('click', this.handleClose);
    root.querySelector('#wpd-cancel')?.removeEventListener('click', this.handleClose);
    root.querySelector('#wpd-form')?.removeEventListener('submit', this.handleSubmit);
    if (this.readOnly) return;
    root.querySelectorAll<HTMLInputElement>('input[type="checkbox"]').forEach(b => b.removeEventListener('change', this.handleDependencyChange));
    root.querySelectorAll('.wpd-browse-btn').forEach(b => b.removeEventListener('click', this.handleBrowse));
    root.querySelectorAll('.wpd-browse-list-btn').forEach(b => b.removeEventListener('click', this.handleBrowseList));
    root.querySelectorAll('.wpd-remove-list-btn').forEach(b => b.removeEventListener('click', this.handleRemoveListItem));
    root.querySelectorAll('.wpd-add-list-btn').forEach(b => b.removeEventListener('click', this.handleAddListItem));
    root.querySelectorAll('.wpd-section-hdr').forEach(b => b.removeEventListener('click', this.handleToggleSection));
    root.querySelectorAll('.wpd-col-section-hdr').forEach(b => b.removeEventListener('click', this.handleToggleColSection));
    root.querySelectorAll('.wpd-browse-col-btn').forEach(b => b.removeEventListener('click', this.handleBrowseColTag));
    root.querySelectorAll('.wpd-remove-col-btn').forEach(b => b.removeEventListener('click', this.handleRemoveColumn));
    root.querySelectorAll('.wpd-add-col-btn').forEach(b => b.removeEventListener('click', this.handleAddColumn));
    root.querySelectorAll('.wpd-move-col-up-btn').forEach(b => b.removeEventListener('click', this.handleMoveColumnUp));
    root.querySelectorAll('.wpd-move-col-down-btn').forEach(b => b.removeEventListener('click', this.handleMoveColumnDown));
  }

  // ── Sync helpers ─────────────────────────────────────────────────────────────

  private syncListValues(): void {
    const root = this.getRoot();
    for (const prop of this.properties) {
      if (prop.type !== 'path-list') continue;
      const inputs = root.querySelectorAll<HTMLInputElement>(`.wpd-list-input[data-prop="${prop.name}"]`);
      this.values[prop.name] = Array.from(inputs).map(i => i.value);
    }
  }

  private syncColumnListValues(): void {
    const root = this.getRoot();
    for (const prop of this.properties) {
      if (prop.type !== 'column-list') continue;
      const rows = root.querySelectorAll<HTMLElement>(`.wpd-col-row[data-prop="${prop.name}"]`);
      const cols: Array<{ header: string; tagPath: string; formatter: string; width: string }> = [];
      rows.forEach(row => {
        cols.push({
          header:    row.querySelector<HTMLInputElement>('.wpd-col-header')?.value    ?? '',
          tagPath:   row.querySelector<HTMLInputElement>('.wpd-col-tagpath')?.value   ?? '',
          formatter: row.querySelector<HTMLSelectElement>('.wpd-col-formatter')?.value ?? 'text',
          width:     row.querySelector<HTMLInputElement>('.wpd-col-width')?.value     ?? '',
        });
      });
      this.values[prop.name] = cols;
    }
  }

  /** Snapshot current values of all standard (non-list) form inputs into this.values. */
  private syncFormValues(): void {
    const form = this.getRoot().querySelector<HTMLFormElement>('#wpd-form');
    if (!form) return;
    for (const prop of this.properties) {
      if (prop.type === 'section' || prop.type === 'path-list' || prop.type === 'column-list') continue;
      if (prop.type === 'boolean') {
        const el = form.querySelector<HTMLInputElement>(`#prop-${prop.name}`);
        if (el) this.values[prop.name] = el.checked;
      } else if (prop.type === 'icon') {
        const el = form.querySelector(`#prop-${prop.name}`) as any;
        if (el) this.values[prop.name] = el.value ?? prop.default;
      } else {
        const el = form.querySelector<HTMLInputElement | HTMLSelectElement>(`#prop-${prop.name}`);
        if (el) this.values[prop.name] = el.value;
      }
    }
  }

  private syncAll(): void { this.syncFormValues(); this.syncListValues(); this.syncColumnListValues(); }

  // ── Handlers ─────────────────────────────────────────────────────────────────

  private handleClose = (): void => { this.close(); };

  private handleDependencyChange = (): void => {
    this.syncFormValues();
    this.applyDependencyState();
  };

  private isDisabledByDependency(prop: PropertyField): boolean {
    const sourceField = String(prop.context?.disabledUnlessField ?? '');
    if (!sourceField) return false;
    const sourceProp = this.properties.find(p => p.name === sourceField);
    return !Boolean(this.values[sourceField] ?? sourceProp?.default);
  }

  private applyDependencyState(): void {
    const root = this.getRoot();
    for (const prop of this.properties) {
      if (!prop.context?.disabledUnlessField) continue;
      const el = root.querySelector<HTMLInputElement | HTMLSelectElement>(`#prop-${prop.name}`);
      if (!el) continue;
      const disabled = this.isDisabledByDependency(prop);
      el.disabled = disabled;
      el.style.opacity = disabled ? '0.65' : '';
    }
  }

  private handleBrowse = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    if (!propName) return;
    const prop = this.properties.find(p => p.name === propName);
    const includeLeaves = prop?.context?.includeLeaves === true;
    const currentValue = (this.getRoot().querySelector(`#prop-${propName}`) as HTMLInputElement | null)?.value.trim() ?? '';
    const rootFromField = String(prop?.context?.rootFromField ?? '');
    const rootValue = rootFromField
      ? (this.getRoot().querySelector(`#prop-${rootFromField}`) as HTMLInputElement | null)?.value.trim()
        || String(this.values[rootFromField] ?? '').trim()
      : '';
    const browseRoot = this.resolveBrowseRoot(rootValue);
    const selectedPath = browseRoot && currentValue
      ? this.joinPath(browseRoot, currentValue)
      : currentValue.replace(/\.\*.*$/, '');
    const expandTo = selectedPath.replace(/\.\*.*$/, '');
    getTreeBrowserDialog().open(browseRoot, includeLeaves ? 'Select Tag' : 'Select Node', (path) => {
      const input = this.getRoot().querySelector(`#prop-${propName}`) as HTMLInputElement | null;
      if (!input) return;
      input.value = prop?.context?.stripBrowseRoot === true
        ? this.stripBrowseRoot(path, browseRoot)
        : path;
    }, includeLeaves, expandTo, selectedPath);
  };

  private resolveBrowseRoot(rootValue: string): string {
    const cleanRoot = String(rootValue ?? '').trim().replace(/\.+$/g, '');
    if (!cleanRoot) return '';
    if (!cleanRoot.includes('*')) return cleanRoot;

    const deviceName = getUiStore().get('deviceName') || '';
    if (deviceName) return cleanRoot.replace(/\*/g, deviceName).replace(/\.+$/g, '');

    const starIdx = cleanRoot.indexOf('*');
    const parent = cleanRoot.slice(0, starIdx).replace(/\.+$/g, '');
    const suffix = cleanRoot.slice(starIdx + 1).replace(/^\.+|\.+$/g, '');
    if (!parent) return '';

    const store = getMirrorStore();
    const child = store.listChildrenNames(store.toAbsolute(parent))[0] ?? '';
    if (!child) return parent;
    return `${parent}.${child}${suffix ? `.${suffix}` : ''}`;
  }

  private joinPath(prefix: string, suffix: string): string {
    const p = prefix.replace(/\.+$/g, '');
    const s = suffix.replace(/^\.+/g, '');
    if (!p) return s;
    if (!s) return p;
    if (s === p || s.startsWith(p + '.')) return s;
    return `${p}.${s}`;
  }

  private stripBrowseRoot(path: string, root: string): string {
    const cleanRoot = root.replace(/\.+$/g, '');
    if (!cleanRoot) return path;
    if (path === cleanRoot) return '';
    const rootDot = cleanRoot + '.';
    return path.startsWith(rootDot) ? path.slice(rootDot.length) : path;
  }

  private handleBrowseList = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    if (!propName) return;
    this.syncListValues();
    const currentList: string[] = Array.isArray(this.values[propName]) ? this.values[propName] : [];
    const expandTo = (currentList[index] ?? '').replace(/\.\*.*$/, '');
    getTreeBrowserDialog().open('', 'Select Node', (path) => {
      this.syncListValues();
      const list: string[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
      list[index] = path;
      this.values[propName] = list;
      this.rerender();
    }, false, expandTo);
  };

  private handleRemoveListItem = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    if (!propName) return;
    this.syncListValues();
    const list: string[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    list.splice(index, 1);
    this.values[propName] = list;
    this.rerender();
  };

  private handleAddListItem = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    if (!propName) return;
    this.syncListValues();
    const list: string[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    list.push('');
    this.values[propName] = list;
    this.rerender();
  };

  private handleToggleSection = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const sectionKey = btn.dataset.section;
    if (!sectionKey) return;
    this.syncAll();
    const stateKey = `section:${sectionKey}`;
    this.collapseState[stateKey] = !(this.collapseState[stateKey] ?? false);
    this.rerender();
  };

  private handleToggleColSection = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    if (!propName) return;
    this.syncAll();
    this.collapseState[propName] = !(this.collapseState[propName] ?? true);
    this.rerender();
  };

  /** Browse for a tag; strip the device-path prefix to give a relative path. */
  private handleBrowseColTag = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    const parentDepth = parseInt(btn.dataset.parentDepth ?? '0', 10);
    if (!propName) return;
    this.syncColumnListValues();
    getTreeBrowserDialog().open('', 'Select Tag', (fullPath) => {
      this.syncColumnListValues();
      const cols: any[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
      // Strip parentNode (parentDepth segments) + device name (1 segment) → relative path.
      let tagPath = fullPath;
      if (parentDepth > 0) {
        const parts = fullPath.split('.');
        if (parts.length > parentDepth + 1) {
          tagPath = parts.slice(parentDepth + 1).join('.');
        }
      }
      if (cols[index]) cols[index] = { ...cols[index], tagPath };
      this.values[propName] = cols;
      this.rerender();
    }, true);
  };

  private handleRemoveColumn = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    if (!propName) return;
    this.syncColumnListValues();
    const cols: any[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    cols.splice(index, 1);
    this.values[propName] = cols;
    this.rerender();
  };

  private handleAddColumn = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    if (!propName) return;
    this.syncColumnListValues();
    const cols: any[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    cols.push({ header: '', tagPath: '', formatter: 'text', width: '' });
    this.values[propName] = cols;
    this.rerender();
  };

  private handleMoveColumnUp = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    if (!propName || index === 0) return;
    this.syncColumnListValues();
    const cols: any[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    [cols[index - 1], cols[index]] = [cols[index], cols[index - 1]];
    this.values[propName] = cols;
    this.rerender();
  };

  private handleMoveColumnDown = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const propName = btn.dataset.prop;
    const index = parseInt(btn.dataset.index ?? '0', 10);
    if (!propName) return;
    this.syncColumnListValues();
    const cols: any[] = Array.isArray(this.values[propName]) ? [...this.values[propName]] : [];
    if (index >= cols.length - 1) return;
    [cols[index], cols[index + 1]] = [cols[index + 1], cols[index]];
    this.values[propName] = cols;
    this.rerender();
  };

  private handleSubmit = (e: Event): void => {
    e.preventDefault();
    if (this.readOnly) {
      this.close();
      return;
    }
    this.syncAll();

    const form = e.target as HTMLFormElement;
    const formData = new FormData(form);
    const newValues: Record<string, any> = {};

    for (const prop of this.properties) {
      if (prop.type === 'section') continue;
      if (prop.type === 'boolean') {
        const checkbox = form.querySelector(`#prop-${prop.name}`) as HTMLInputElement;
        newValues[prop.name] = checkbox?.checked ?? false;
      } else if (prop.type === 'number') {
        const value = formData.get(prop.name);
        newValues[prop.name] = value ? parseFloat(value as string) : prop.default;
      } else if (prop.type === 'path-list') {
        newValues[prop.name] = (this.values[prop.name] as string[] ?? []).filter(Boolean);
      } else if (prop.type === 'column-list') {
        newValues[prop.name] = this.values[prop.name] as any[] ?? [];
      } else if (prop.type === 'icon') {
        newValues[prop.name] = (form.querySelector(`#prop-${prop.name}`) as any)?.value ?? prop.default;
      } else {
        const value = formData.get(prop.name);
        newValues[prop.name] = value === null ? prop.default : value;
      }
    }

    this.emit('properties-updated', { widgetId: this.widgetId, config: newValues });
    this.close();
  };

  private esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }
}

customElements.define('widget-properties-dialog', WidgetPropertiesDialog);
