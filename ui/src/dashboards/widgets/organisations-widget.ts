import { BaseComponent } from '../../components/base-component';
import { showConfirm } from '../../components/app-dialog';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { getCurrentUser } from '../../auth';
import {
  listOrganisations, createOrganisation, updateOrganisation, deleteOrganisation,
  listAPIKeys, createAPIKey, deleteAPIKey,
} from '../../api';
import type { Organisation, OrgArea, APIKey } from '../../api';

registerPermissions('organisations', 'Organisation Manager', [
  { name: 'view', description: 'View organisations and their settings' },
  { name: 'change', description: 'Create, edit, and delete organisations' },
], 'Controls access to the Organisation Manager widget - roles with view can inspect organisations; roles with change can create, edit, and delete organisations.');

registerWidgetType({
  type: 'organisations-widget',
  name: 'Organisation Manager',
  icon: '🏢',
  category: 'System',
  defaultW: 12,
  defaultH: 28,
  minW: 10,
  minH: 18,
});

// ---------------------------------------------------------------------------
// Leaflet loader - singleton promise so CSS+JS are loaded only once globally
// ---------------------------------------------------------------------------
let leafletReady: Promise<void> | null = null;

function loadLeaflet(): Promise<void> {
  if (leafletReady) return leafletReady;
  leafletReady = new Promise<void>((resolve, reject) => {
    if ((window as any).L) { resolve(); return; }

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css';
    document.head.appendChild(link);

    const script = document.createElement('script');
    script.src = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.js';
    script.onload = () => resolve();
    script.onerror = () => { leafletReady = null; reject(new Error('Failed to load Leaflet')); };
    document.head.appendChild(script);
  });
  return leafletReady;
}

// ---------------------------------------------------------------------------
// State types
// ---------------------------------------------------------------------------
interface FormState {
  /** Immutable slug of the org being edited; null when creating */
  editingName: string | null;
  /** Slug - only editable at create time */
  name: string;
  /** Human-friendly label, always editable */
  displayName: string;
  active: boolean;
  logo: string;
  favicon: string;
  area: OrgArea | null;
  saving: boolean;
  error: string;
}

function blankForm(): FormState {
  return {
    editingName: null, name: '', displayName: '', active: true, logo: '', favicon: '', area: null,
    saving: false, error: '',
  };
}

// ---------------------------------------------------------------------------
// Widget
// ---------------------------------------------------------------------------
export class OrganisationsWidget extends BaseComponent {
  private orgs: Organisation[] = [];
  private currentOrgName = getCurrentUser()?.tenant_id ?? '';
  private loading = true;
  private loadError = '';
  private canChange = false;
  private form: FormState = blankForm();
  private panelOpen = false;

  // Leaflet handles
  private map: any = null;
  private mapRect: any = null;
  private mapMarkers: any[] = [];

  // API key state
  private apiKeys: APIKey[] = [];
  private apiKeysLoading = false;
  private keySection = false;  // expanded?
  private newlyGeneratedKey: APIKey | null = null;

  // -------------------------------------------------------------------------
  // Lifecycle
  // -------------------------------------------------------------------------
  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canChange] = await Promise.all([
      can('organisations.view'),
      can('organisations.change'),
    ]);
    this.canChange = canChange;
    if (!canView && !canChange) {
      this.innerHTML = `<div class="p-8 text-center org-manager-subtle-text text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      this.orgs = await listOrganisations();
      this.loadError = '';
    } catch (err: any) {
      this.loadError = err?.message ?? 'Failed to load organisations';
    }
    this.loading = false;
    this.rerender();
  }

  private rerender(): void {
    this.destroyMap();
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    if (this.panelOpen) {
      this.initMap().catch(err => console.error('Map init failed:', err));
    }
  }

  protected detachEventListeners(): void { /* innerHTML replacement handles cleanup */ }

  // -------------------------------------------------------------------------
  // Map lifecycle
  // -------------------------------------------------------------------------
  private destroyMap(): void {
    if (this.map) {
      this.map.remove();
      this.map = null;
      this.mapRect = null;
      this.mapMarkers = [];
    }
  }

  private async initMap(): Promise<void> {
    await loadLeaflet();
    const L = (window as any).L;
    if (!L) return;

    const container = this.querySelector<HTMLElement>('#org-map');
    if (!container) return;

    // Prevent map pointer events from bubbling up to GridStack's drag handler.
    // Without this, any mousedown/pointerdown on the map would drag the whole widget.
    ['mousedown', 'pointerdown', 'touchstart'].forEach(evt => {
      container.addEventListener(evt, e => e.stopPropagation(), { passive: false });
    });

    this.map = L.map(container, { zoomControl: true, attributionControl: false });

    // OpenStreetMap standard tiles
    L.tileLayer(
      'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
      {
        attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
        maxZoom: 19,
      },
    ).addTo(this.map);

    // Draw rectangle only if an area is already set
    const area = this.form.area;
    if (area) {
      this.initEditableRect(L, [[area.south, area.west], [area.north, area.east]]);
      this.map.fitBounds(this.mapRect.getBounds(), { padding: [28, 28] });
    } else {
      // No area - show a default view without any rectangle
      this.map.setView([54.5, -2.0], 5);
    }
  }

  private initEditableRect(L: any, bounds: [[number, number], [number, number]]): void {
    const sw: [number, number] = [Math.min(bounds[0][0], bounds[1][0]), Math.min(bounds[0][1], bounds[1][1])];
    const ne: [number, number] = [Math.max(bounds[0][0], bounds[1][0]), Math.max(bounds[0][1], bounds[1][1])];
    const [south, west] = sw;
    const [north, east] = ne;

    this.mapRect = L.rectangle([[south, west], [north, east]], {
      color: '#f59e0b',
      weight: 2,
      fillColor: '#f59e0b',
      fillOpacity: 0.08,
      dashArray: '4 3',
    }).addTo(this.map);

    if (!this.canChange) {
      this.form.area = { north, south, east, west };
      this.updateCoordDisplay();
      return;
    }

    // Corner drag handles: NW, NE, SE, SW
    const cornerIcon = L.divIcon({
      className: '',
      html: '<div style="width:12px;height:12px;background:#f59e0b;border:2px solid #1a1a1a;border-radius:2px;cursor:move;transform:translate(-6px,-6px);box-shadow:0 1px 6px rgba(245,158,11,0.6);"></div>',
      iconSize: [0, 0],
      iconAnchor: [0, 0],
    });

    const corners: [number, number][] = [
      [north, west], // 0 NW
      [north, east], // 1 NE
      [south, east], // 2 SE
      [south, west], // 3 SW
    ];

    this.mapMarkers = corners.map((pos, i) => {
      const m = L.marker(pos, { draggable: true, icon: cornerIcon, zIndexOffset: 1000 });
      m.addTo(this.map);
      m.on('drag', () => this.onCornerDrag(i));
      m.on('dragend', () => this.snapAllMarkers());
      return m;
    });

    // Center move handle - drags the whole rectangle without resizing
    const centerIcon = L.divIcon({
      className: '',
      html: '<div style="width:16px;height:16px;background:#f59e0b;border:2px solid #1a1a1a;border-radius:50%;cursor:move;transform:translate(-8px,-8px);box-shadow:0 1px 8px rgba(245,158,11,0.7);display:flex;align-items:center;justify-content:center;"><div style="width:4px;height:4px;background:#1a1a1a;border-radius:50%;pointer-events:none;"></div></div>',
      iconSize: [0, 0],
      iconAnchor: [0, 0],
    });
    const centerMarker = L.marker(
      [(north + south) / 2, (east + west) / 2],
      { draggable: true, icon: centerIcon, zIndexOffset: 999 },
    );
    centerMarker.addTo(this.map);

    // Half-dimensions captured at dragstart so the rect size stays constant during a move
    let halfLat = (north - south) / 2;
    let halfLng = (east - west)  / 2;

    centerMarker.on('dragstart', () => {
      if (this.mapRect) {
        const b = this.mapRect.getBounds();
        halfLat = (b.getNorth() - b.getSouth()) / 2;
        halfLng = (b.getEast()  - b.getWest())  / 2;
      }
    });
    centerMarker.on('drag', () => {
      const pos = centerMarker.getLatLng();
      const n = pos.lat + halfLat, s = pos.lat - halfLat;
      const e = pos.lng + halfLng, w = pos.lng - halfLng;
      this.mapRect.setBounds([[s, w], [n, e]]);
      this.mapMarkers[0].setLatLng([n, w]);
      this.mapMarkers[1].setLatLng([n, e]);
      this.mapMarkers[2].setLatLng([s, e]);
      this.mapMarkers[3].setLatLng([s, w]);
      this.form.area = { north: n, south: s, east: e, west: w };
      this.updateCoordDisplay();
    });
    centerMarker.on('dragend', () => this.snapAllMarkers());
    this.mapMarkers.push(centerMarker); // index 4

    // Sync form state
    this.form.area = { north, south, east, west };
    this.updateCoordDisplay();
  }

  /** Update rect + non-dragged corners while a corner is being dragged. */
  private onCornerDrag(cornerIndex: number): void {
    const opposite = (cornerIndex + 2) % 4;
    const dragged = this.mapMarkers[cornerIndex].getLatLng();
    const fixed   = this.mapMarkers[opposite].getLatLng();

    const north = Math.max(dragged.lat, fixed.lat);
    const south = Math.min(dragged.lat, fixed.lat);
    const east  = Math.max(dragged.lng, fixed.lng);
    const west  = Math.min(dragged.lng, fixed.lng);

    this.mapRect.setBounds([[south, west], [north, east]]);

    // Positions for all four corners
    const all: [number, number][] = [
      [north, west], [north, east], [south, east], [south, west],
    ];
    // Update every marker except the one being dragged
    for (let i = 0; i < 4; i++) {
      if (i !== cornerIndex) this.mapMarkers[i].setLatLng(all[i]);
    }

    this.form.area = { north, south, east, west };
    this.updateCoordDisplay();
  }

  /** After any drag ends, snap all markers to their exact positions. */
  private snapAllMarkers(): void {
    if (!this.mapRect) return;
    const b = this.mapRect.getBounds();
    const n = b.getNorth(), s = b.getSouth(), e = b.getEast(), w = b.getWest();
    const corners: [number, number][] = [[n, w], [n, e], [s, e], [s, w]];
    for (let i = 0; i < 4; i++) this.mapMarkers[i]?.setLatLng(corners[i]);
    // Snap center marker (index 4)
    this.mapMarkers[4]?.setLatLng([(n + s) / 2, (e + w) / 2]);
  }

  /** Write current bounds into the coord display without a full rerender. */
  private updateCoordDisplay(): void {
    const el = this.querySelector('#coord-display');
    if (!el || !this.form.area) return;
    const { north, south, east, west } = this.form.area;
    el.textContent = `N ${north.toFixed(4)}°  S ${south.toFixed(4)}°  E ${east.toFixed(4)}°  W ${west.toFixed(4)}°`;
  }

  // -------------------------------------------------------------------------
  // Render
  // -------------------------------------------------------------------------
  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-8 text-center org-manager-subtle-text text-sm">Loading organisations…</div>`;
      return;
    }
    if (this.loadError) {
      this.innerHTML = `<div class="p-8 text-center text-red-400 text-sm">${this.esc(this.loadError)}</div>`;
      return;
    }

    this.innerHTML = `
      <div class="flex flex-col h-full text-sm">

        <!-- ── Header bar ──────────────────────────────────────────────── -->
        <div class="flex items-center justify-between px-4 py-3 border-b shrink-0"
             style="border-color:var(--border-color);">
          <div class="flex items-center gap-2">
            <span class="font-medium">Organisations</span>
            <span class="text-xs px-2 py-0.5 rounded-full font-mono"
                  style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);
                         color:var(--accent-color);">${this.orgs.length}</span>
          </div>
          ${this.canChange ? `
          <button id="new-org-btn"
                  class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);
                         color:var(--accent-color);
                         border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);">
            + New Organisation
          </button>` : `
          <span class="text-xs px-2 py-1 rounded border org-manager-muted-text"
                style="border-color:var(--border-color);">Read only</span>`}
        </div>

        <!-- ── Body: list + panel ──────────────────────────────────────── -->
        <div class="flex flex-1 overflow-hidden">

          <!-- Org list -->
          <div class="flex flex-col shrink-0 overflow-y-auto border-r"
               style="width:220px; border-color:var(--border-color);">
            ${this.orgs.length === 0
              ? `<p class="px-4 py-6 text-center org-manager-subtle-text text-xs">No organisations</p>`
              : this.orgs.map(o => this.renderOrgRow(o)).join('')}
          </div>

          <!-- Edit / create panel -->
          <div class="flex flex-col flex-1 overflow-hidden">
            ${this.panelOpen ? this.renderPanel() : this.renderEmptyState()}
          </div>

        </div>
      </div>
    `;
  }

  private renderOrgRow(o: Organisation): string {
    const isSelected = this.panelOpen && this.form.editingName === o.name;
    const label = o.displayName || o.name;
    return `
      <button class="org-row w-full flex items-center gap-3 px-4 py-3 text-left border-b transition-colors"
              data-name="${this.esc(o.name)}"
              style="border-color:var(--border-color);
                     ${isSelected
                       ? 'background:color-mix(in srgb,var(--accent-color) 10%,transparent);'
                       : ''}">
        <span class="flex-1 min-w-0">
          <span class="block text-xs truncate">${this.esc(label)}</span>
          ${o.displayName
            ? `<span class="block font-mono text-xs org-manager-muted-text truncate">${this.esc(o.name)}</span>`
            : ''}
        </span>
        <span class="shrink-0 w-1.5 h-1.5 rounded-full"
              style="background:${o.active ? '#4ade80' : 'var(--inactive-dot)'};"
              title="${o.active ? 'Active' : 'Inactive'}"></span>
      </button>
    `;
  }

  private renderEmptyState(): string {
    return `
      <div class="flex flex-col items-center justify-center h-full gap-3 org-manager-subtle-text">
        <svg class="w-10 h-10" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1"
                d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4"/>
        </svg>
        <p class="text-xs">Select an organisation${this.canChange ? ' or create a new one' : ''}</p>
      </div>
    `;
  }

  private renderPanel(): string {
    const isCreate = this.form.editingName === null;
    const title    = isCreate ? 'New Organisation' : (this.esc(this.form.displayName) || this.esc(this.form.editingName!));
    const area     = this.form.area;
    const coordText = area
      ? `N ${area.north.toFixed(4)}°  S ${area.south.toFixed(4)}°  E ${area.east.toFixed(4)}°  W ${area.west.toFixed(4)}°`
      : 'No area set - drag the handles to define bounds';

    return `
      <div class="flex flex-col h-full">

        <!-- Dashboard header -->
        <div class="flex items-center justify-between px-4 py-2.5 border-b shrink-0"
             style="border-color:var(--border-color); background:color-mix(in srgb,var(--accent-color) 5%,transparent);">
          <span class="font-medium text-xs uppercase tracking-wider org-manager-secondary-text">${title}</span>
          <button id="panel-close" class="org-manager-icon-button p-1 rounded transition-colors">
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <!-- Form fields row -->
        <div class="flex flex-wrap items-end gap-4 px-4 py-3 shrink-0 border-b"
             style="border-color:var(--border-color);">

          <!-- Slug / Name (editable only at create time) -->
          <div class="flex-1 min-w-0">
            <label class="block text-xs org-manager-secondary-text mb-1">Name (slug)${isCreate ? ' *' : ''}</label>
            ${isCreate
              ? `<input id="org-name" type="text" value="${this.esc(this.form.name)}"
                        placeholder="e.g. acme-corp"
                        class="w-full px-2.5 py-1.5 text-xs rounded border outline-none font-mono"
                        style="background:var(--input-bg);border-color:var(--border-color);color:inherit;"
                        ${this.canChange ? '' : 'disabled'}>`
              : `<div class="px-2.5 py-1.5 text-xs font-mono org-manager-muted-text rounded border"
                      style="border-color:var(--border-color);" title="Immutable - used as RTDB node key">${this.esc(this.form.editingName!)}</div>`}
          </div>

          <!-- Display name (always editable) -->
          <div class="flex-1 min-w-0">
            <label class="block text-xs org-manager-secondary-text mb-1">Display name</label>
            <input id="org-display-name" type="text" value="${this.esc(this.form.displayName)}"
                   placeholder="e.g. Acme Corporation"
                   class="w-full px-2.5 py-1.5 text-xs rounded border outline-none"
                   style="background:var(--input-bg);border-color:var(--border-color);color:inherit;"
                   ${this.canChange ? '' : 'disabled'}>
          </div>

          <!-- Active toggle -->
          <div class="shrink-0">
            <label class="block text-xs org-manager-secondary-text mb-1">Status</label>
            <button id="active-toggle"
                    class="flex items-center gap-2 px-3 py-1.5 text-xs rounded border transition-colors"
                    style="${this.form.active
                      ? 'border-color:rgba(74,222,128,0.4);color:#4ade80;background:rgba(34,197,94,0.08);'
                      : 'border-color:var(--subtle-divider);color:var(--org-manager-muted-text);background:transparent;'}"
                    ${this.canChange ? '' : 'disabled'}>
              <span class="inline-block w-1.5 h-1.5 rounded-full"
                    style="background:${this.form.active ? '#4ade80' : 'var(--inactive-dot)'};"></span>
              ${this.form.active ? 'Active' : 'Inactive'}
            </button>
          </div>

          <!-- Branding uploads -->
          <div class="flex items-end gap-2 shrink-0">
            <div>
              <label class="block text-xs org-manager-secondary-text mb-1">Logo</label>
              <label class="flex items-center gap-2 px-2.5 py-1.5 text-xs rounded border cursor-pointer"
                     style="border-color:var(--border-color);background:var(--input-bg);">
                ${this.form.logo
                  ? `<img src="${this.esc(this.form.logo)}" alt="" style="width:18px;height:18px;object-fit:contain;">`
                  : '<span class="org-manager-muted-text">Upload</span>'}
                <input id="org-logo" type="file" accept="image/*" class="hidden" ${this.canChange ? '' : 'disabled'}>
              </label>
            </div>
            ${this.form.logo && this.canChange ? `
            <button id="clear-logo-btn"
                    class="px-2.5 py-1.5 text-xs rounded border transition-colors"
                    style="border-color:var(--border-color);">
              Clear
            </button>` : ''}
          </div>

          <div class="flex items-end gap-2 shrink-0">
            <div>
              <label class="block text-xs org-manager-secondary-text mb-1">Favicon</label>
              <label class="flex items-center gap-2 px-2.5 py-1.5 text-xs rounded border cursor-pointer"
                     style="border-color:var(--border-color);background:var(--input-bg);">
                ${this.form.favicon
                  ? `<img src="${this.esc(this.form.favicon)}" alt="" style="width:18px;height:18px;object-fit:contain;">`
                  : '<span class="org-manager-muted-text">Upload</span>'}
                <input id="org-favicon" type="file" accept="image/*" class="hidden" ${this.canChange ? '' : 'disabled'}>
              </label>
            </div>
            ${this.form.favicon && this.canChange ? `
            <button id="clear-favicon-btn"
                    class="px-2.5 py-1.5 text-xs rounded border transition-colors"
                    style="border-color:var(--border-color);">
              Clear
            </button>` : ''}
          </div>

          <!-- Area toggle -->
          <div class="shrink-0">
            <label class="block text-xs org-manager-secondary-text mb-1">Area</label>
            <button id="area-toggle-btn"
                    class="px-3 py-1.5 text-xs rounded border transition-colors org-manager-secondary-button"
                    style="border-color:var(--border-color);"
                    title="${this.canChange ? (area ? 'Remove geographic bounds' : 'Draw a geographic area on the map') : 'Read only'}"
                    ${this.canChange ? '' : 'disabled'}>
              ${area ? 'Clear bounds' : 'Set bounds'}
            </button>
          </div>

          <!-- Delete (edit mode, non-default only) -->
          ${this.canChange && !isCreate && this.form.editingName !== 'default' ? `
          <div class="shrink-0">
            <label class="block text-xs org-manager-secondary-text mb-1">&nbsp;</label>
            <button id="delete-org-btn"
                    class="px-3 py-1.5 text-xs rounded border transition-colors"
                    style="border-color:rgba(239,68,68,0.35);color:#f87171;background:rgba(239,68,68,0.06);"
                    title="Delete this organisation">
              Delete
            </button>
          </div>` : ''}
        </div>

        <!-- Map - fills remaining space -->
        <div id="org-map" class="flex-1" style="min-height:180px;"></div>

        <!-- Coord readout -->
        <div class="px-4 py-2 shrink-0 border-t font-mono text-xs org-manager-muted-text tabular-nums"
             style="border-color:var(--border-color);" id="coord-display">
          ${coordText}
        </div>

        <!-- API keys section (edit mode only) -->
        ${this.form.editingName !== null ? `
        <div id="api-key-section" class="px-4 shrink-0" style="border-top:1px solid var(--subtle-divider);">
          ${this.apiKeySectionHtml()}
        </div>` : ''}

        
        <!-- Error banner -->
        ${this.form.error ? `
        <div class="mx-4 mb-2 px-3 py-2 rounded text-xs text-red-400"
             style="background:rgba(239,68,68,0.08);">${this.esc(this.form.error)}</div>
        ` : ''}

        <!-- Action buttons -->
        <div class="flex items-center justify-end gap-2 px-4 py-3 border-t shrink-0"
             style="border-color:var(--border-color);">
          <button id="panel-cancel"
                  class="px-4 py-1.5 text-xs rounded border transition-colors"
                  style="border-color:var(--border-color);">
            ${this.canChange ? 'Cancel' : 'Close'}
          </button>
          ${this.canChange ? `
          <button id="panel-save"
                  class="px-4 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background:var(--accent-color);color:var(--accent-text);"
                  ${this.form.saving ? 'disabled' : ''}>
            ${this.form.saving ? 'Saving…' : 'Save'}
          </button>` : ''}
        </div>

      </div>
    `;
  }

  // -------------------------------------------------------------------------
  // Event listeners
  // -------------------------------------------------------------------------
  protected attachEventListeners(): void {
    if (this.canChange) {
      this.querySelector('#new-org-btn')?.addEventListener('click', () => this.openCreate());
    }

    // Org row clicks (select for editing)
    this.querySelectorAll<HTMLElement>('.org-row').forEach(btn => {
      btn.addEventListener('click', () => this.openEdit(btn.dataset.name!));
    });

    if (!this.panelOpen) return;

    this.querySelector('#panel-close')?.addEventListener('click', () => {
      this.panelOpen = false;
      this.form = blankForm();
      this.rerender();
    });
    this.querySelector('#panel-cancel')?.addEventListener('click', () => {
      this.panelOpen = false;
      this.form = blankForm();
      this.rerender();
    });
    if (this.canChange) {
      this.querySelector('#active-toggle')?.addEventListener('click', () => {
        this.form.active = !this.form.active;
        // Update toggle appearance without full rerender (map would reset)
        this.updateActiveToggle();
      });
      this.querySelector('#area-toggle-btn')?.addEventListener('click', () => {
        const btn = this.querySelector<HTMLButtonElement>('#area-toggle-btn');
        if (this.form.area) {
          // Clear bounds - remove rectangle and markers
          this.form.area = null;
          if (this.mapRect) { this.mapRect.remove(); this.mapRect = null; }
          this.mapMarkers.forEach(m => m.remove());
          this.mapMarkers = [];
          const cd = this.querySelector('#coord-display');
          if (cd) cd.textContent = 'No area set - drag the handles to define bounds';
          if (btn) { btn.textContent = 'Set bounds'; btn.title = 'Draw a geographic area on the map'; }
        } else {
          // Set bounds - draw a rectangle around the current map centre
          const L = (window as any).L;
          if (!L || !this.map) return;
          const c = this.map.getCenter();
          const delta = 0.5;
          this.initEditableRect(L, [
            [c.lat - delta, c.lng - delta],
            [c.lat + delta, c.lng + delta],
          ]);
          if (btn) { btn.textContent = 'Clear bounds'; btn.title = 'Remove geographic bounds'; }
        }
      });
      this.querySelector('#panel-save')?.addEventListener('click', () => this.handleSave());
      this.querySelector('#org-logo')?.addEventListener('change', (e: Event) => this.handleBrandFile(e, 'logo'));
      this.querySelector('#org-favicon')?.addEventListener('change', (e: Event) => this.handleBrandFile(e, 'favicon'));
      this.querySelector('#clear-logo-btn')?.addEventListener('click', () => {
        this.form.logo = '';
        this.rerender();
      });
      this.querySelector('#clear-favicon-btn')?.addEventListener('click', () => {
        this.form.favicon = '';
        this.rerender();
      });

      // Delete button in panel
      this.querySelector('#delete-org-btn')?.addEventListener('click', async () => {
        const name = this.form.editingName;
        if (!name) return;
        const confirmed = await showConfirm(
          `Delete organisation "${name}"? This cannot be undone.`,
          { title: 'Delete Organisation', confirmLabel: 'Delete', tone: 'danger' }
        );
        if (confirmed) this.handleDelete(name);
      });
    }

    
    // API key listeners (initial render)
    this.attachAPIKeyListeners();
  }

  /** Patches just the active toggle button in-place to avoid map teardown. */
  private updateActiveToggle(): void {
    const btn = this.querySelector<HTMLButtonElement>('#active-toggle');
    if (!btn) return;
    const active = this.form.active;
    btn.style.cssText = active
      ? 'border-color:rgba(74,222,128,0.4);color:#4ade80;background:rgba(34,197,94,0.08);'
      : 'border-color:var(--subtle-divider);color:var(--org-manager-muted-text);background:transparent;';
    const dot = btn.querySelector('span');
    if (dot) dot.style.background = active ? '#4ade80' : 'var(--inactive-dot)';
    btn.childNodes.forEach(n => {
      if (n.nodeType === Node.TEXT_NODE) n.textContent = active ? ' Active' : ' Inactive';
    });
  }

  // -------------------------------------------------------------------------
  // Actions
  // -------------------------------------------------------------------------
  private openCreate(): void {
    if (!this.canChange) return;
    this.form = blankForm();
    this.panelOpen = true;
    this.rerender();
  }

  private openEdit(name: string): void {
    const org = this.orgs.find(o => o.name === name);
    if (!org) return;
    this.form = {
      editingName: org.name,
      name: org.name,
      displayName: org.displayName,
      active: org.active,
      logo: org.logo ?? '',
      favicon: org.favicon ?? '',
      area: org.area ?? null,
      saving: false, error: '',
    };
    this.panelOpen = true;
    this.apiKeys = [];
    this.newlyGeneratedKey = null;
    this.keySection = false;
    this.rerender();
    if (this.isCurrentOrg(org.name)) {
      this.loadAPIKeys();
    }
  }

  private async loadAPIKeys(): Promise<void> {
    this.apiKeysLoading = true;
    try {
      this.apiKeys = await listAPIKeys();
    } catch { this.apiKeys = []; }
    this.apiKeysLoading = false;
    this.renderAPIKeySection();
  }

  /** Patch just the API key section DOM without a full rerender (avoids map teardown). */
  private renderAPIKeySection(): void {
    const el = this.querySelector<HTMLElement>('#api-key-section');
    if (!el) return;
    el.innerHTML = this.apiKeySectionHtml();
    this.attachAPIKeyListeners();
  }

  private async handleSave(): Promise<void> {
    if (!this.canChange) return;
    const isCreate = this.form.editingName === null;

    // Read slug (create only) and display name from inputs
    const nameInput = this.querySelector<HTMLInputElement>('#org-name');
    if (nameInput) {
      this.form.name = nameInput.value.trim();
    }
    const displayNameInput = this.querySelector<HTMLInputElement>('#org-display-name');
    if (displayNameInput) {
      this.form.displayName = displayNameInput.value.trim();
    }

    if (!this.form.name) {
      this.form.error = 'Name is required.';
      this.rerender();
      return;
    }

    this.form.saving = true;
    this.form.error = '';

    // Capture current map bounds before rerender wipes the map
    if (this.mapRect) {
      const b = this.mapRect.getBounds();
      this.form.area = {
        north: b.getNorth(),
        south: b.getSouth(),
        east:  b.getEast(),
        west:  b.getWest(),
      };
    }

    try {
      if (isCreate) {
        await createOrganisation({
          name: this.form.name,
          displayName: this.form.displayName,
          active: this.form.active,
          logo: this.form.logo,
          favicon: this.form.favicon,
          area: this.form.area ?? undefined,
        });
      } else {
        await updateOrganisation(this.form.editingName!, {
          displayName: this.form.displayName,
          active: this.form.active,
          logo: this.form.logo,
          favicon: this.form.favicon,
          area: this.form.area ?? undefined,
        });
      }
      this.panelOpen = false;
      this.form = blankForm();
      await this.loadData();   // refreshes orgs list + rerenders
      this.emit('organisations-changed');
    } catch (err: any) {
      this.form.error = err?.message ?? 'Failed to save.';
      this.form.saving = false;
      this.rerender();
    }
  }

  private handleBrandFile(e: Event, field: 'logo' | 'favicon'): void {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    if (!file.type.startsWith('image/')) {
      this.form.error = 'Branding uploads must be image files.';
      this.rerender();
      return;
    }
    if (file.size > 256 * 1024) {
      this.form.error = 'Branding images must be 256 KB or smaller.';
      this.rerender();
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      this.form[field] = String(reader.result ?? '');
      this.form.error = '';
      this.rerender();
    };
    reader.onerror = () => {
      this.form.error = 'Failed to read branding image.';
      this.rerender();
    };
    reader.readAsDataURL(file);
  }

  private async handleDelete(name: string): Promise<void> {
    if (!this.canChange) return;
    try {
      await deleteOrganisation(name);
      this.panelOpen = false;
      this.form = blankForm();
      await this.loadData();
      this.emit('organisations-changed');
    } catch (err: any) {
      this.form.error = err?.message ?? 'Failed to delete.';
      this.rerender();
    }
  }

  // -------------------------------------------------------------------------
  // API key actions
  // -------------------------------------------------------------------------
  private async handleCreateKey(): Promise<void> {
    if (!this.canChange) return;
    const input = this.querySelector<HTMLInputElement>('#key-name-input');
    const name = (input?.value ?? '').trim();
    if (!this.form.editingName) return;
    if (!name) {
      if (input) {
        input.style.borderColor = '#f87171';
        input.placeholder = 'Enter a name for the key';
        input.focus();
      }
      return;
    }
    try {
      this.newlyGeneratedKey = await createAPIKey(name);
      this.apiKeys = await listAPIKeys();
      this.keySection = true;
    } catch (err: any) {
      this.form.error = err?.message ?? 'Failed to create API key';
    }
    this.renderAPIKeySection();
  }

  private async handleDeleteKey(id: number): Promise<void> {
    if (!this.canChange) return;
    if (!this.form.editingName) return;
    try {
      await deleteAPIKey(id);
      if (this.newlyGeneratedKey?.id === id) {
        this.newlyGeneratedKey = null;
      }
      this.apiKeys = await listAPIKeys();
    } catch (err: any) {
      this.form.error = err?.message ?? 'Failed to delete API key';
    }
    this.renderAPIKeySection();
  }

  // -------------------------------------------------------------------------
  // API key HTML
  // -------------------------------------------------------------------------
  private apiKeySectionHtml(): string {
    const isCreate = this.form.editingName === null;
    if (isCreate) return '';
    if (!this.isCurrentOrg(this.form.editingName ?? '')) return '';

    const keys = this.apiKeys;
    const loading = this.apiKeysLoading;

    const keyRows = keys.length === 0 && !loading
      ? `<div style="font-family:inherit;font-size:0.7rem;color:var(--org-manager-subtle-text);padding:6px 0;text-align:center;">No API keys</div>`
      : keys.map(k => `
        <div style="
          display:flex;align-items:center;gap:8px;padding:6px 0;
          border-bottom:1px solid var(--subtle-divider);
        ">
          <span style="font-family:inherit;font-size:0.75rem;white-space:nowrap;">${this.esc(k.name)}</span>
          <code style="
            flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;
            font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,'Liberation Mono','Courier New',monospace;
            font-size:0.65rem;letter-spacing:0.04em;color:var(--org-manager-muted-text);
          ">${this.esc(k.key)}</code>
          <span style="font-family:inherit;font-size:0.7rem;color:var(--org-manager-subtle-text);white-space:nowrap;">
            ${new Date(k.createdAt).toLocaleDateString()}
          </span>
          ${this.canChange ? `<button class="key-delete-btn" data-id="${k.id}" style="
            font-family:inherit;font-size:0.75rem;padding:3px 10px;border-radius:3px;cursor:pointer;
            border:1px solid rgba(239,68,68,0.3);color:#f87171;background:transparent;
            white-space:nowrap;
          ">Delete</button>` : ''}
        </div>`).join('');

    const canAdd = keys.length < 5;
    const addForm = !this.canChange
      ? ''
      : canAdd ? `
      <div style="display:flex;align-items:center;gap:6px;margin-top:8px;">
        <input id="key-name-input" type="text" placeholder="Key name…"
               style="font-family:inherit;font-size:0.75rem;flex:1;padding:4px 8px;border-radius:3px;
                      background:var(--input-bg);
                      border:1px solid var(--subtle-divider);color:inherit;outline:none;"
               maxlength="64">
        <button id="key-create-btn" style="
          font-family:inherit;font-size:0.75rem;padding:4px 12px;border-radius:3px;cursor:pointer;
          background:color-mix(in srgb,var(--accent-color) 15%,transparent);
          color:var(--accent-color);
          border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);
          white-space:nowrap;
        ">+ Generate</button>
      </div>` : `<div style="font-family:inherit;font-size:0.7rem;color:var(--org-manager-subtle-text);margin-top:6px;">Maximum 5 keys reached</div>`;

    const generatedKey = this.newlyGeneratedKey ? `
      <div style="
        margin:8px 0;padding:8px 10px;border-radius:4px;
        border:1px solid color-mix(in srgb,var(--accent-color) 32%,var(--subtle-divider));
        background:color-mix(in srgb,var(--accent-color) 9%,transparent);
      ">
        <div style="display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:6px;">
          <div style="font-family:inherit;font-size:0.68rem;font-weight:600;color:var(--accent-color);">
            New API key generated
          </div>
          <button id="generated-key-copy-btn" type="button" style="
            font-family:inherit;font-size:0.68rem;padding:3px 10px;border-radius:3px;cursor:pointer;
            background:color-mix(in srgb,var(--accent-color) 15%,transparent);
            color:var(--accent-color);
            border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);
            white-space:nowrap;
          ">Copy</button>
        </div>
        <div style="font-family:inherit;font-size:0.68rem;color:var(--org-manager-muted-text);margin-bottom:6px;">
          Save this key now. It will not be shown again.
        </div>
        <code style="
          display:block;white-space:normal;overflow-wrap:anywhere;user-select:all;
          font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,'Liberation Mono','Courier New',monospace;
          font-size:0.68rem;line-height:1.5;color:var(--content-text);
        ">${this.esc(this.newlyGeneratedKey.key)}</code>
      </div>` : '';

    const header = `
      <button id="api-keys-toggle" style="
        font-family:inherit;width:100%;display:flex;align-items:center;justify-content:space-between;
        gap:6px;padding:8px 0;cursor:pointer;background:none;border:none;
        color:inherit;text-align:left;
        border-top:1px solid var(--subtle-divider);
      ">
        <span style="font-family:inherit;font-size:0.65rem;font-weight:600;letter-spacing:0.08em;
                     text-transform:uppercase;color:var(--org-manager-muted-text);flex:1;">
          API Keys <span style="color:var(--org-manager-secondary-text);font-weight:400;">(${keys.length}/5)</span>
        </span>
        <span style="font-family:inherit;font-size:0.7rem;color:var(--org-manager-muted-text);">${this.keySection ? '▲' : '▼'}</span>
      </button>`;

    return header + (this.keySection ? `
      <div style="padding-bottom:6px;">
        ${generatedKey}
        ${loading ? '<div style="font-family:inherit;font-size:0.7rem;color:var(--org-manager-subtle-text);padding:4px 0;">Loading…</div>' : keyRows}
        ${loading ? '' : addForm}
      </div>` : '');
  }

  private attachAPIKeyListeners(): void {
    this.querySelector('#api-keys-toggle')?.addEventListener('click', () => {
      this.keySection = !this.keySection;
      this.renderAPIKeySection();
    });
    this.querySelector('#key-create-btn')?.addEventListener('click', () => this.handleCreateKey());
    this.querySelector('#key-name-input')?.addEventListener('keydown', (e: Event) => {
      if ((e as KeyboardEvent).key === 'Enter') this.handleCreateKey();
    });
    this.querySelector<HTMLButtonElement>('#generated-key-copy-btn')?.addEventListener('click', btnEvent => {
      const btn = btnEvent.currentTarget as HTMLButtonElement;
      const key = this.newlyGeneratedKey?.key ?? '';
      if (!key) return;
      this.copyToClipboard(key).then(() => {
        btn.textContent = 'Copied';
        setTimeout(() => {
          if (btn.isConnected) btn.textContent = 'Copy';
        }, 1500);
      }).catch(() => {
        btn.textContent = 'Copy failed';
        setTimeout(() => {
          if (btn.isConnected) btn.textContent = 'Copy';
        }, 1800);
      });
    });
    this.querySelectorAll<HTMLElement>('.key-delete-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = parseInt(btn.dataset.id ?? '0', 10);
        if (id > 0) this.handleDeleteKey(id);
      });
    });
  }

  // -------------------------------------------------------------------------
  // Helpers
  // -------------------------------------------------------------------------
  private esc(s: string): string {
    return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  private isCurrentOrg(name: string): boolean {
    return name !== '' && name === this.currentOrgName;
  }

  private async copyToClipboard(text: string): Promise<void> {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }

    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    document.body.appendChild(textarea);
    textarea.select();
    try {
      if (!document.execCommand('copy')) {
        throw new Error('Clipboard copy failed');
      }
    } finally {
      textarea.remove();
    }
  }
}

customElements.define('organisations-widget', OrganisationsWidget);
