import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { getCurrentUser } from '../../auth';
import { can } from '../../permissions/permissions';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { showAlert } from '../../components/app-dialog';
import { TEMPLATES_ROOT } from '../../constants';
import { formatUnixMillis } from '../../utils/time';
import {
  updateNode, loadNode, createNode, deleteNode,
  createTag, deleteTag, updateTag, updateTagValue, debugTagPipeline,
  loadBlockSchemas, listNotificationProfiles,
  type PipelineBlockEnvelope, type NodeType, type BlockSchema, type NotificationProfile,
} from '../../api';

registerWidgetType({
  type: 'tags-manager-widget',
  name: 'Tags Manager',
  icon: '🏷️',
  category: 'System',
  defaultW: 12,
  defaultH: 16,
  minW: 8,
  minH: 8,
});

// ─── Types ────────────────────────────────────────────────────────────────────

interface CachedValue {
  value: any;
  timestamp: number;
  status: string;
}

interface NodeData {
  name: string;
  description: string;
  templateName: string;
  path: string;
  nodeType?: string;
}

interface TagData {
  path: string;
  name: string;
  description: string;
  units: string;
  deadband: string;
  type: string;
  enumValues: Record<string, string>;
}

interface PipelineBlockUI {
  type: string;
  params: Record<string, any>;
}

interface DebugStep {
  type: string;
  input: any;
  output: any;
  error?: string;
  stateChange?: string;
}

// BlockSchema is imported from api.ts (server-driven)

// ─── Node type labels ─────────────────────────────────────────────────────────

const NODE_TYPE_LABELS: Record<string, string> = {
  Organisation: 'ORG',
  Device:       'DEV',
  Standard:     '',
};

const TAGS_MANAGER_MODAL_Z_INDEX = 19000;

// ─── Helpers ──────────────────────────────────────────────────────────────────

function scalarTypeToString(type: any): string {
  // Handle null/undefined
  if (type == null) return 'string';

  // Handle numeric ScalarType: 0=integer, 1=float, 2=string, 3=boolean, 4=enum
  if (typeof type === 'number') {
    switch (type) {
      case 0: return 'integer';
      case 1: return 'float';
      case 2: return 'string';
      case 3: return 'boolean';
      case 4: return 'enum';
      default: return 'string';
    }
  }

  // Handle string (convert to lowercase)
  if (typeof type === 'string') {
    return type.toLowerCase();
  }

  // Fallback for unexpected types
  console.warn('Unexpected type value:', type, typeof type);
  return 'string';
}

function normalizeEnumValues(raw: any): Record<string, string> {
  if (!raw) return {};
  if (Array.isArray(raw)) {
    return raw.reduce((acc, label, idx) => {
      if (label !== undefined && label !== null) acc[String(idx)] = String(label);
      return acc;
    }, {} as Record<string, string>);
  }
  if (typeof raw === 'object') {
    const values: Record<string, string> = {};
    for (const [key, label] of Object.entries(raw)) {
      if (label !== undefined && label !== null) values[String(key)] = String(label);
    }
    return values;
  }
  return {};
}

function sortEnumKeys(keys: string[]): string[] {
  return keys.sort((a, b) => {
    const ai = Number.parseInt(a, 10);
    const bi = Number.parseInt(b, 10);
    if (Number.isFinite(ai) && Number.isFinite(bi) && ai !== bi) return ai - bi;
    return a.localeCompare(b);
  });
}

function escapeHtml(value: any): string {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// ─── Widget ───────────────────────────────────────────────────────────────────

export class TagsManagerWidget extends BaseComponent {

  // Widget configuration
  private config = {
    showHeader: true,
  };
  private canWrite = false;

  // Core view state
  private expandedNodes: Set<string> = new Set();
  private searchQuery = '';
  private statusFilter: string | null = null;
  private subscribedPaths: Set<string> = new Set();
  private valueCache: Map<string, CachedValue> = new Map();
  private tagCountCache: Map<string, number> = new Map();
  private matchingTagCountCache: Map<string, number> = new Map();
  private searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  private treeUnsubscribe: (() => void) | null = null;

  // Lock state (populated lazily via API)
  private lockedNodes: Map<string, boolean> = new Map();

  // Node modal state
  private isModalOpen = false;
  private modalMode: 'edit' | 'add-child' | 'add-root' | 'add-sibling' = 'edit';
  private editingNodePath: string | null = null;
  private editingNodeData: NodeData | null = null;
  private parentPathForAdd: string | null = null;
  private validationErrors: Map<string, string> = new Map();

  // Tag modal state
  private isTagModalOpen = false;
  private tagModalMode: 'edit' | 'add' = 'edit';
  private editingTagPath: string | null = null;
  private editingTagData: TagData | null = null;
  private tagModalParentPath: string | null = null;
  private tagPipeline: PipelineBlockUI[] = [];
  private tagValidationErrors: Map<string, string> = new Map();
  private tagArrayMode = false;
  private tagArraySize = 4;
  private enumValuesExpanded = true;
  // 'inherited' = pipeline comes from template (read-only); 'editing' = local pipeline
  private pipelineMode: 'inherited' | 'editing' = 'editing';
  // true when the tag has a template ref so "Revert to template" is available
  private pipelineHasTemplate = false;

  // Delete confirmation state
  private isDeleteConfirmOpen = false;
  private deleteTarget: { type: 'node' | 'tag'; path: string } | null = null;

  // Block schemas (loaded from server)
  private blockSchemas: BlockSchema[] = [];

  // Notification profiles (loaded from server, used by limitcheck block editor)
  private notificationProfiles: NotificationProfile[] = [];

  // Collapsed state for pipeline blocks (indices not in set are collapsed)
  private expandedPipelineBlocks: Set<number> = new Set();

  // Pipeline debugger state
  private isDebuggerOpen = false;
  private debugTagPath: string | null = null;
  private debugInput = '';
  private debugResults: DebugStep[] | null = null;
  private debugFinalOutput: any = undefined;
  private debugRunning = false;
  private debugError = '';

  // Value edit state
  private isValueEditOpen = false;
  private valueEditPath: string | null = null;
  private valueEditType: string | null = null;
  private valueEditCurrent: any = null;

  // ─── Lifecycle ──────────────────────────────────────────────────────────────

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canRead, canWrite] = await Promise.all([can('tags.read'), can('tags.write')]);
    this.canWrite = canWrite;
    if (!canRead && !canWrite) {
      this.innerHTML = `<div class="p-4 text-sm opacity-60">Insufficient permissions</div>`;
      return;
    }
    const store = getMirrorStore();
    this.treeUnsubscribe = store.subscribeToTreeChanges('', (path, data) => {
      if (data === null) {
        // A node subtree was deleted. The store discarded those Node objects
        // (and their subscriber lists). Evict the affected paths from
        // subscribedPaths and valueCache so ensureSubscribed() re-registers
        // callbacks on the fresh Node objects when the subtree reappears.
        const prefix = path + '.';
        for (const p of Array.from(this.subscribedPaths)) {
          if (p === path || p.startsWith(prefix)) {
            this.subscribedPaths.delete(p);
            this.valueCache.delete(p);
          }
        }
      }
      this.rerender();
    });
    // Eagerly hydrate the store from the REST API so the initial render shows
    // real tree data rather than waiting for NATS events to trickle in.
    const orgRoot = getCurrentUser()?.tenant_id ?? '';
    if (orgRoot) {
      store.loadTreeFromAPI('', -1).then(() => this.rerender()).catch(() => {});
    }
    this.rerender();
  }

  disconnectedCallback(): void {
    if (this.searchDebounceTimer) clearTimeout(this.searchDebounceTimer);
    if (this.treeUnsubscribe) { this.treeUnsubscribe(); this.treeUnsubscribe = null; }
    this.applyModalStacking(false);
    super.disconnectedCallback();
  }

  setConfig(config: Record<string, any>): void {
    if (config.showHeader !== undefined) {
      this.config.showHeader = Boolean(config.showHeader);
    }
    // Re-render to apply new config if widget is already connected
    if (this.isConnected) {
      this.rerender();
    }
  }

  getConfig(): Record<string, any> {
    return { ...this.config };
  }

  static getPropertySchema(): any[] {
    return [
      {
        name: 'showHeader',
        type: 'boolean',
        label: 'Show Header',
        description: 'Display the widget header with "Tags Manager" title',
        default: true,
      },
    ];
  }

  // ─── Helpers ────────────────────────────────────────────────────────────────

  private statusBadgeHtml(status: string): string {
    const code = status.toUpperCase();
    let html = '';
    if (code.includes('A')) html += `<span class="px-1.5 py-0.5 rounded text-xs font-medium mr-1" style="background:rgba(239,68,68,.35);color:#f87171">ALARM</span>`;
    if (code.includes('S')) html += `<span class="px-1.5 py-0.5 rounded text-xs font-medium mr-1" style="background:rgba(245,158,11,.35);color:#fbbf24">STALE</span>`;
    if (code.includes('D')) html += `<span class="px-1.5 py-0.5 rounded text-xs font-medium mr-1" style="background:rgba(59,130,246,.35);color:#60a5fa">DEV</span>`;
    if (html === '') {
      html = this.statusMatchesFilter(code, 'U')
        ? `<span class="px-1.5 py-0.5 rounded text-xs font-medium" style="background:rgba(156,163,175,.3);color:#d1d5db">UNDEF</span>`
        : `<span class="px-1.5 py-0.5 rounded text-xs font-medium" style="background:rgba(34,197,94,.35);color:#4ade80">NORMAL</span>`;
    }
    return html;
  }

  private statusMatchesFilter(status: string, filter: string): boolean {
    const code = status.toUpperCase();
    if (filter === 'N') return code === '' || code === 'N';
    if (filter === 'U') return code === 'U';
    return code.includes(filter);
  }

  private statusFilterLabel(): string {
    switch (this.statusFilter) {
      case 'N': return 'NORMAL';
      case 'A': return 'ALARM';
      case 'S': return 'STALE';
      case 'D': return 'DEV';
      case 'U': return 'UNDEF';
      default: return '';
    }
  }

  private statusFilterColor(): string {
    switch (this.statusFilter) {
      case 'N': return '#22c55e';
      case 'A': return '#ef4444';
      case 'S': return '#f59e0b';
      case 'D': return '#3b82f6';
      case 'U': return '#9ca3af';
      default: return 'var(--accent-color)';
    }
  }

  private countLeaves(path: string): number {
    const cached = this.tagCountCache.get(path);
    if (cached !== undefined) return cached;
    const store = getMirrorStore();
    let count = 0;
    for (const child of store.listChildrenNames(path)) {
      const cp = path ? `${path}.${child}` : child;
      count += store.getNodeType(cp) === 'leaf' ? 1 : this.countLeaves(cp);
    }
    this.tagCountCache.set(path, count);
    return count;
  }

  private countMatchingLeaves(path: string): number {
    const cached = this.matchingTagCountCache.get(path);
    if (cached !== undefined) return cached;
    const store = getMirrorStore();
    let count = 0;
    for (const child of store.listChildrenNames(path)) {
      const cp = path ? `${path}.${child}` : child;
      const nt = store.getNodeType(cp);
      if (nt === 'leaf' || (nt === 'unknown' && !store.listChildrenNames(cp).length)) {
        if (this.leafMatchesFilters(cp)) count += 1;
      } else {
        count += this.countMatchingLeaves(cp);
      }
    }
    this.matchingTagCountCache.set(path, count);
    return count;
  }

  private escapeId(path: string): string { return path.replace(/\./g, '_'); }

  private nodeMatchesSearch(path: string, query: string): boolean {
    const lq = query.toLowerCase();
    const store = getMirrorStore();
    const name = path.split('.').pop()!;
    if (name.toLowerCase().includes(lq) || path.toLowerCase().includes(lq)) return true;
    for (const child of store.listChildrenNames(path)) {
      if (this.nodeMatchesSearch(path ? `${path}.${child}` : child, query)) return true;
    }
    return false;
  }

  private leafMatchesFilters(path: string): boolean {
    const cached = this.valueCache.get(path);
    if (this.statusFilter) {
      const store = getMirrorStore();
      const status = store.getNodeType(path) !== 'unknown' ? store.getNodeStatus(path) : cached?.status ?? '';
      if (!this.statusMatchesFilter(status, this.statusFilter)) return false;
    }
    if (this.searchQuery) {
      const lq = this.searchQuery.toLowerCase();
      const name = path.split('.').pop()!;
      if (!name.toLowerCase().includes(lq) && !path.toLowerCase().includes(lq)) return false;
    }
    return true;
  }

  private ensureSubscribed(leafPath: string): void {
    if (this.subscribedPaths.has(leafPath)) return;
    this.subscribedPaths.add(leafPath);
    const store = getMirrorStore();
    store.subscribe(leafPath, (value: any) => {
      if (!this.isConnected) return;
      const ts = store.getNodeTimestamp(leafPath);
      const status = store.getNodeStatus(leafPath);
      this.valueCache.set(leafPath, { value, timestamp: ts, status });
      const eid = this.escapeId(leafPath);
      const rowEl = this.querySelector(`[data-leaf-path="${leafPath}"]`);
      if (this.statusFilter && rowEl && !this.statusMatchesFilter(status, this.statusFilter)) {
        this.rerender();
        return;
      }
      const valEl  = this.querySelector(`#val-${eid}`);
      const statEl = this.querySelector(`#stat-${eid}`);
      const timeEl = this.querySelector(`#time-${eid}`);
      if (valEl) {
        const units = store.getNodeShared(leafPath)?.units ?? '';
        valEl.innerHTML = this.formatValue(value) +
          (units ? `<span style="font-weight:500;opacity:0.85;font-size:0.8em">${units}</span>` : '');
      }
      if (statEl) statEl.innerHTML = this.statusBadgeHtml(status);
      if (timeEl) timeEl.textContent = this.formatTimestamp(ts);
    });
  }

  private formatValue(val: any): string {
    if (val === undefined || val === null) return '-';
    if (typeof val === 'boolean') return val ? 'true' : 'false';
    if (typeof val === 'number' && !Number.isInteger(val)) return val.toFixed(2);
    return String(val);
  }

  private formatTimestamp(ts: number): string {
    if (!ts) return '-';
    return formatUnixMillis(ts, getUiStore().get('serverTimezone')) ?? '-';
  }

  private getBlockSchema(type: string): BlockSchema | undefined {
    return this.blockSchemas.find(s => s.type === type);
  }

  // ─── Tree Rendering ──────────────────────────────────────────────────────────

  /** Render the whole tree starting from path ('' = root). */
  private renderSubtree(path: string, depth: number): string {
    const store = getMirrorStore();
    const parentName = path ? path.split('.').pop()! : '';
    const children = store.listChildrenNames(path)
      .filter(c => !(c === parentName && !store.listChildrenNames(path ? `${path}.${c}` : c).length))
      .sort((a, b) => a.localeCompare(b));
    if (children.length === 0) return '';

    const nodes: string[] = [];
    const leaves: string[] = [];
    for (const child of children) {
      const cp = path ? `${path}.${child}` : child;
      const nt = store.getNodeType(cp);
      if (nt === 'leaf' || (nt === 'unknown' && !store.listChildrenNames(cp).length)) leaves.push(cp);
      else nodes.push(cp);
    }

    let html = '';

    // Direct leaves rendered before child nodes so tags appear just below the "Add Tag" button
    if (leaves.length > 0 && depth > 0) {
      const filtered = leaves.filter(p => this.leafMatchesFilters(p));
      if (filtered.length > 0) html += this.renderLeafTable(filtered, depth);
    }

    for (const nodePath of nodes) {
      if (this.searchQuery && !this.nodeMatchesSearch(nodePath, this.searchQuery)) continue;

      const name       = nodePath.split('.').pop()!;
      const isExpanded = this.expandedNodes.has(nodePath);
      const leafCount  = this.countLeaves(nodePath);
      const matchCount = this.statusFilter ? this.countMatchingLeaves(nodePath) : 0;
      const indent     = depth * 20;
      const desc       = store.getNodeShared(nodePath)?.description || '';

      // Node type badge (Organisation / Device only)
      const nodeConfig = store.getNodeConfig(nodePath);
      const nodeTypeKey = nodeConfig?.type || '';
      const typeLabel  = NODE_TYPE_LABELS[nodeTypeKey] ?? '';
      const typeBadge  = typeLabel
        ? `<span class="px-1 py-0.5 rounded text-xs font-bold mr-1" style="background:color-mix(in srgb,var(--accent-color) 18%,transparent);color:var(--accent-color)">${typeLabel}</span>`
        : '';

      // Array node indicator
      const isArrayNode = store.getIsArray(nodePath);
      const arrayBadge = isArrayNode
        ? `<span class="px-1 py-0.5 rounded text-xs font-bold mr-1" style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);font-family:monospace">[ ]</span>`
        : '';
      const matchBadge = this.statusFilter && matchCount > 0
        ? `<span class="tv-status-match-badge ml-2 px-1.5 py-0.5 rounded text-xs font-semibold"
                 data-status-match="${this.statusFilter}"
                 style="background:color-mix(in srgb,${this.statusFilterColor()} 24%,transparent);color:${this.statusFilterColor()};border:1px solid color-mix(in srgb,${this.statusFilterColor()} 45%,transparent)">${matchCount} ${this.statusFilterLabel()}</span>`
        : '';

      let latestTs = 0;
      for (const [cp, c] of this.valueCache) {
        if (cp.startsWith(nodePath + '.') && c.timestamp > latestTs) latestTs = c.timestamp;
      }

      html += `
        <div class="tv-node-row flex items-center py-1 px-2 cursor-pointer hover:opacity-80"
             style="padding-left:${indent + 8}px;border-bottom:1px solid color-mix(in srgb,var(--border-color) 30%,transparent)"
             data-node-path="${nodePath}">
          <span class="mr-1 text-xs opacity-60 ${isExpanded ? 'rotate-90' : ''} inline-block transition-transform" style="width:16px">▶</span>
          ${typeBadge}${arrayBadge}
          <span class="font-medium text-sm" style="color:var(--accent-color)">${store.getIsArray(path) && /^\d+$/.test(name) ? `[${name}]` : name}</span>
          ${desc ? `<span class="ml-2 text-xs opacity-50">${desc}</span>` : ''}
          <span class="ml-2 px-1.5 py-0.5 rounded text-xs" style="background:color-mix(in srgb,var(--accent-color) 20%,transparent);color:var(--accent-color)">${leafCount} tags</span>
          ${matchBadge}
          <span class="ml-auto text-xs opacity-40">${latestTs ? this.formatTimestamp(latestTs) : ''}</span>
          <span class="flex items-center gap-1 ml-3" style="flex-shrink:0">
            <button class="tv-node-action px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100"
                    data-action="edit" title="${this.canWrite ? 'Edit node' : 'View node'}"><span style="font-size:0.7rem;line-height:1;vertical-align:middle">✏️</span></button>
            ${this.canWrite ? `
            <button class="tv-node-action px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100"
                    data-action="add-sibling" title="Add sibling node"><span style="font-size:1.2rem;line-height:1;vertical-align:middle">⊕</span></button>
            <button class="tv-node-action px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100"
                    data-action="add-child" title="Add child node"><span style="font-size:0.7rem;line-height:1;vertical-align:middle">⤵️</span></button>
            <button class="tv-node-action px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100"
                    data-action="delete" title="Delete node" style="color:#f87171"><span style="font-size:0.7rem;line-height:1;vertical-align:middle">🗑️</span></button>
            ` : ''}
          </span>
        </div>`;

      if (isExpanded) {
        html += `
          ${this.canWrite ? `<div class="flex items-center py-1" style="padding-left:${(depth + 1) * 20 + 8}px;border-bottom:1px solid color-mix(in srgb,var(--border-color) 20%,transparent)">
            <button class="tv-add-tag-btn px-2 py-0.5 text-xs rounded"
                    style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);border:1px dashed var(--accent-color);opacity:.7"
                    data-parent-path="${nodePath}">＋ Add Tag</button>
          </div>` : ''}`;
        html += this.renderSubtree(nodePath, depth + 1);
      }
    }

    return html;
  }

  private renderLeafTable(leaves: string[], depth: number): string {
    const indent = depth * 20;
    const store  = getMirrorStore();
    for (const lp of leaves) this.ensureSubscribed(lp);

    const sorted = [...leaves].sort((a, b) => a.localeCompare(b));

    let rows = '';
    for (const leafPath of sorted) {
      if (!this.leafMatchesFilters(leafPath)) continue;
      const name   = leafPath.split('.').pop()!;
      const eid    = this.escapeId(leafPath);
      const cached = this.valueCache.get(leafPath);
      const val    = cached?.value;
      const hasStoreNode = store.getNodeType(leafPath) !== 'unknown';
      const status = hasStoreNode ? store.getNodeStatus(leafPath) : cached?.status ?? '';
      const ts     = hasStoreNode ? store.getNodeTimestamp(leafPath) : cached?.timestamp ?? 0;
      const units  = store.getNodeShared(leafPath)?.units ?? '';

      rows += `
        <tr class="tv-leaf-row" data-leaf-path="${leafPath}" style="border-bottom:1px solid color-mix(in srgb,var(--border-color) 30%,transparent)">
          <td class="px-2 py-1">
            <div class="text-xs">${name}</div>
            <div class="text-xs opacity-40">${leafPath.split('.').slice(1).join('.')}</div>
          </td>
          <td class="px-2 py-1">
            <span class="tv-value-cell px-2 rounded cursor-pointer hover:opacity-80 text-xs"
                  id="val-${eid}"
                  data-leaf-path="${leafPath}"
                  title="Click to edit value"
                  style="min-width:120px;min-height:1.5rem;display:inline-flex;align-items:center;gap:0.25rem;border:1px solid var(--border-color);background:color-mix(in srgb,var(--accent-color) 8%,transparent);color:var(--accent-color);font-weight:600">
              ${this.formatValue(val)}${units ? `<span style="font-weight:500;opacity:0.85;font-size:0.8em">${units}</span>` : ''}
            </span>
          </td>
          <td class="px-2 py-1" id="stat-${eid}">${this.statusBadgeHtml(status)}</td>
          <td class="px-2 py-1 text-xs opacity-60" id="time-${eid}">${this.formatTimestamp(ts)}</td>
          <td class="px-2 py-1">
             <div class="flex items-center gap-1">
              <button class="tv-edit-tag px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100" data-action="edit-tag"  title="${this.canWrite ? 'Edit tag' : 'View tag'}">✏️</button>
              ${this.canWrite ? `<button class="tv-debug-tag px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100" data-action="debug-tag" title="Debug pipeline">🔬</button>
              <button class="tv-delete-tag px-1.5 py-0.5 text-xs rounded opacity-60 hover:opacity-100" data-action="delete-tag" title="Delete tag" style="color:#f87171">🗑️</button>` : ''}
            </div>
          </td>
        </tr>`;
    }

    return `
      <table class="w-full text-xs" style="margin-left:${indent}px;width:calc(100% - ${indent}px)">
        <thead>
          <tr style="border-bottom:1px solid var(--border-color)">
            <th class="px-2 py-1 text-left font-medium uppercase tracking-wider opacity-70 text-xs" style="color:var(--accent-color)">Tag</th>
            <th class="px-2 py-1 text-left font-medium uppercase tracking-wider opacity-70 text-xs" style="color:var(--accent-color)">Value</th>
            <th class="px-2 py-1 text-left font-medium uppercase tracking-wider opacity-70 text-xs" style="color:var(--accent-color)">Status</th>
            <th class="px-2 py-1 text-left font-medium uppercase tracking-wider opacity-70 text-xs" style="color:var(--accent-color)">Updated</th>
            <th class="px-2 py-1 text-left font-medium uppercase tracking-wider opacity-70 text-xs" style="color:var(--accent-color)">Actions</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>`;
  }

  // ─── Node Modal ──────────────────────────────────────────────────────────────

  private renderModal(): string {
    if (!this.isModalOpen || !this.editingNodeData) return '';
    const data  = this.editingNodeData;
    const isAdd = this.modalMode !== 'edit';
    let title: string;
    if (this.modalMode === 'add-root')         title = 'Add Organisation / Top-Level Node';
    else if (this.modalMode === 'add-child')   title = 'Add Child Node';
    else if (this.modalMode === 'add-sibling') title = 'Add Sibling Node';
    else {
      title = data.nodeType ? `Edit Node - ${data.nodeType.toLowerCase()}` : 'Edit Node';
    }
    const nameError = this.validationErrors.get('name');
    const disabled = this.canWrite ? '' : 'disabled';

    return `
      <div id="node-editor-modal" class="fixed inset-0 z-50 flex items-center justify-center" style="z-index:${TAGS_MANAGER_MODAL_Z_INDEX};background:rgba(0,0,0,.55)">
        <div class="rounded-lg shadow-2xl p-6 w-full max-w-md" style="background:var(--content-bg);border:1px solid var(--border-color)">
          <div class="flex items-center justify-between mb-5">
            <h3 class="text-base font-semibold" style="color:var(--accent-color)">${title}</h3>
            <button id="modal-close" class="text-2xl leading-none opacity-50 hover:opacity-100" style="color:var(--content-text)">&times;</button>
          </div>
          <form id="node-edit-form" class="space-y-4">
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">${isAdd ? 'Parent Path' : 'Path'}</label>
              <input type="text" value="${data.path}" disabled
                     class="w-full px-3 py-2 text-xs rounded border opacity-50"
                     style="border-color:var(--border-color);background:color-mix(in srgb,var(--content-bg) 90%,black);color:var(--content-text)">
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Name</label>
              <input type="text" id="node-name" value="${data.name}"
                     class="w-full px-3 py-2 text-xs rounded border"
                     style="border-color:${nameError ? '#f87171' : 'var(--border-color)'};background:var(--content-bg);color:var(--content-text)"
                     placeholder="Node name (letters, numbers, _ -)" ${disabled}>
              ${nameError ? `<p class="mt-1 text-xs" style="color:#f87171">${nameError}</p>` : ''}
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Description</label>
              <textarea id="node-description" rows="2"
                        class="w-full px-3 py-2 text-xs rounded border resize-none"
                        style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>${data.description}</textarea>
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Template</label>
              <div class="flex gap-1">
                <input type="text" id="node-template" value="${data.templateName}"
                       class="flex-1 px-3 py-2 text-xs rounded border"
                       style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                       placeholder="Optional - click ⋯ to browse" ${disabled}>
                <button type="button" id="node-template-browse"
                        class="px-2.5 py-2 text-xs rounded border hover:opacity-80 flex-shrink-0"
                        style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                        title="Browse ${TEMPLATES_ROOT} tree" ${disabled}>⋯</button>
              </div>
            </div>
            ${isAdd ? `
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Node Type</label>
              <select id="node-type" class="w-full px-3 py-2 text-xs rounded border" ${disabled}
                      style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)">
                <option value="Standard">Standard - plain container</option>
                <option value="Device">Device - adds meta &amp; kpi structure</option>
              </select>
            </div>` : ''}
            <div class="flex gap-2 pt-2">
              ${this.canWrite ? `<button type="submit" class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:var(--accent-color);color:var(--accent-text)">${isAdd ? 'Create Node' : 'Save Changes'}</button>` : ''}
              <button type="button" id="modal-cancel" class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">${this.canWrite ? 'Cancel' : 'Close'}</button>
            </div>
          </form>
        </div>
      </div>`;
  }

  // ─── Tag Modal ───────────────────────────────────────────────────────────────

  private renderPipelineEditor(): string {
    const blocks   = this.tagPipeline;
    const readOnly = this.pipelineMode === 'inherited' || !this.canWrite;
    const selectorOptions = this.blockSchemas.map(s => `<option value="${s.type}">${s.label}</option>`).join('');

    let blockRows = '';
    for (let i = 0; i < blocks.length; i++) {
      const block  = blocks[i];
      const schema = this.getBlockSchema(block.type);
      const label  = schema?.label ?? block.type;
      const desc   = schema?.description ?? '';

      let fields = '';
      if (schema) {
        const inputStyle = `border-color:var(--border-color);background:var(--content-bg);color:var(--content-text);${readOnly ? 'opacity:0.5;pointer-events:none' : ''}`;
        for (const [k, def] of Object.entries(schema.params)) {
          const val = this.getNestedParam(block.params, k) ?? def.default ?? '';
          if (def.type === 'boolean') {
            fields += `<label class="flex items-center gap-1.5 text-xs opacity-80">
              <input type="checkbox" class="pipeline-param" data-block-idx="${i}" data-param="${k}" ${val ? 'checked' : ''} ${readOnly ? 'disabled' : ''} style="accent-color:var(--accent-color)">
              ${def.label}</label>`;
          } else if (def.type === 'select') {
            const opts = (def.options ?? []).map(o =>
              `<option value="${o}" ${o === val ? 'selected' : ''}>${o}</option>`
            ).join('');
            fields += `<div class="flex items-center gap-1">
              <label class="text-xs opacity-50 w-20 flex-shrink-0">${def.label}</label>
              <select class="pipeline-param flex-1 px-2 py-0.5 text-xs rounded border"
                      style="${inputStyle}" data-block-idx="${i}" data-param="${k}" ${readOnly ? 'disabled' : ''}>
                ${opts}
              </select>
            </div>`;
          } else if (def.type === 'notification-profile') {
            const noneOpt = `<option value="0" ${!val || val === 0 ? 'selected' : ''}>(none)</option>`;
            const profileOpts = this.notificationProfiles.map(p =>
              `<option value="${p.id}" ${String(p.id) === String(val) ? 'selected' : ''}>${p.name}</option>`
            ).join('');
            fields += `<div class="flex items-center gap-1">
              <label class="text-xs opacity-50 w-20 flex-shrink-0">${def.label}</label>
              <select class="pipeline-param flex-1 px-2 py-0.5 text-xs rounded border"
                      style="${inputStyle}" data-block-idx="${i}" data-param="${k}" ${readOnly ? 'disabled' : ''}>
                ${noneOpt}${profileOpts}
              </select>
            </div>`;
          } else if (def.type === 'string') {
            fields += `<div class="flex items-center gap-1">
              <label class="text-xs opacity-50 w-20 flex-shrink-0">${def.label}</label>
              <input type="text" class="pipeline-param flex-1 px-2 py-0.5 text-xs rounded border"
                     style="${inputStyle}" data-block-idx="${i}" data-param="${k}"
                     value="${val}" placeholder="${def.required ? 'required' : 'optional'}" ${readOnly ? 'disabled' : ''}>
            </div>`;
          } else {
            fields += `<div class="flex items-center gap-1">
              <label class="text-xs opacity-50 w-20 flex-shrink-0">${def.label}</label>
              <input type="number" step="any" class="pipeline-param flex-1 px-2 py-0.5 text-xs rounded border"
                     style="${inputStyle}" data-block-idx="${i}" data-param="${k}"
                     value="${val}" placeholder="${def.required ? 'required' : 'optional'}" ${readOnly ? 'disabled' : ''}>
            </div>`;
          }
        }
      }

      const actionButtons = readOnly ? '' : `
        <div class="flex gap-1 ml-2" onclick="event.stopPropagation()">
          ${i > 0 ? `<button class="pipeline-move-up" data-idx="${i}" title="Move up"
            style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:4px;border:1px solid color-mix(in srgb,var(--accent-color) 40%,transparent);background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);cursor:pointer;flex-shrink:0"
            onmouseover="this.style.background='color-mix(in srgb,var(--accent-color) 28%,transparent)'"
            onmouseout="this.style.background='color-mix(in srgb,var(--accent-color) 12%,transparent)'">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="2,8 6,4 10,8"/></svg>
          </button>` : '<span style="display:inline-block;width:22px"></span>'}
          ${i < blocks.length - 1 ? `<button class="pipeline-move-down" data-idx="${i}" title="Move down"
            style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:4px;border:1px solid color-mix(in srgb,var(--accent-color) 40%,transparent);background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);cursor:pointer;flex-shrink:0"
            onmouseover="this.style.background='color-mix(in srgb,var(--accent-color) 28%,transparent)'"
            onmouseout="this.style.background='color-mix(in srgb,var(--accent-color) 12%,transparent)'">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="2,4 6,8 10,4"/></svg>
          </button>` : '<span style="display:inline-block;width:22px"></span>'}
          <button class="pipeline-remove" data-idx="${i}" title="Remove block"
            style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;border-radius:4px;border:1px solid rgba(248,113,113,0.35);background:rgba(248,113,113,0.1);color:#f87171;cursor:pointer;flex-shrink:0"
            onmouseover="this.style.background='rgba(248,113,113,0.25)'"
            onmouseout="this.style.background='rgba(248,113,113,0.1)'">
            <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="1" y1="1" x2="9" y2="9"/><line x1="9" y1="1" x2="1" y2="9"/></svg>
          </button>
        </div>`;

      const isExpanded = this.expandedPipelineBlocks.has(i);
      const chevron = `<svg class="pipeline-block-chevron" width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="transition:transform 0.15s;transform:rotate(${isExpanded ? '90deg' : '0deg'})"><polyline points="3,2 7,5 3,8"/></svg>`;
      blockRows += `
        <div class="pipeline-block rounded mb-1" style="background:color-mix(in srgb,var(--accent-color) 8%,transparent);border:1px solid color-mix(in srgb,var(--accent-color) 18%,transparent)${readOnly ? ';opacity:0.75' : ''}">
          <div class="pipeline-block-toggle flex items-center gap-1.5 px-2 py-1.5 cursor-pointer select-none" data-block-idx="${i}" style="min-height:28px">
            ${chevron}
            <span class="text-xs font-semibold" style="color:var(--accent-color)">${i + 1}. ${label}</span>
            ${!isExpanded ? `<span class="text-xs opacity-40 ml-1 truncate flex-1">${desc}</span>` : '<span class="flex-1"></span>'}
            ${actionButtons}
          </div>
          ${isExpanded ? `<div class="px-2 pb-2 space-y-1 border-t" style="border-color:color-mix(in srgb,var(--accent-color) 18%,transparent)">${fields}</div>` : ''}
        </div>`;
    }

    const headerRight = readOnly
      ? (this.canWrite && this.pipelineMode === 'inherited'
      ? `<button id="pipeline-override" type="button" class="px-2 py-0.5 text-xs rounded font-medium"
           style="border:1px solid var(--accent-color);color:var(--accent-color);background:transparent;cursor:pointer">
           Override
         </button>`
      : '')
      : `<div class="flex items-center gap-1">
           ${this.pipelineHasTemplate ? `<button id="pipeline-revert" type="button" class="px-2 py-0.5 text-xs rounded"
             style="border:1px solid color-mix(in srgb,var(--border-color) 80%,transparent);color:var(--content-text);opacity:0.6;background:transparent;cursor:pointer">
             Revert to template
           </button>` : ''}
           <select id="pipeline-block-type" class="px-2 py-0.5 text-xs rounded border"
                   style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)">
             ${selectorOptions}
           </select>
           <button id="pipeline-add-block" type="button" class="px-2 py-0.5 text-xs rounded font-medium" style="background:var(--accent-color);color:var(--accent-text)">Add</button>
         </div>`;

    const emptyMsg = readOnly
      ? `<div class="text-xs opacity-40 py-2 text-center">No blocks - value passes through unchanged.</div>`
      : `<div class="text-xs opacity-40 py-2 text-center">No blocks - value passes through unchanged.</div>`;

    return `
      <div class="mt-1">
        <div class="flex items-center justify-between mb-2">
          <div class="flex items-center gap-1.5">
            <label class="text-xs font-semibold opacity-70">Processing Pipeline</label>
            ${readOnly ? `<span class="text-xs px-1.5 py-0.5 rounded" style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);opacity:0.8">from template</span>` : ''}
          </div>
          ${headerRight}
        </div>
        <div id="pipeline-blocks-list" class="max-h-48 overflow-y-auto">
          ${blocks.length === 0 ? emptyMsg : blockRows}
        </div>
      </div>`;
  }

  private renderTagModal(): string {
    if (!this.isTagModalOpen || !this.editingTagData) return '';
    const data  = this.editingTagData;
    const isAdd = this.tagModalMode === 'add';
    const typeOpts = ['float','integer','string','boolean','enum']
      .map(t => `<option value="${t}" ${data.type === t ? 'selected' : ''}>${t}</option>`).join('');
    const nameErr = this.tagValidationErrors.get('name');
    const disabled = this.canWrite ? '' : 'disabled';
    const enumEditor = data.type === 'enum' ? this.renderEnumValuesEditor(disabled) : '';

    return `
      <div id="tag-editor-modal" class="fixed inset-0 z-50 flex items-center justify-center" style="z-index:${TAGS_MANAGER_MODAL_Z_INDEX};background:rgba(0,0,0,.55)">
        <div class="rounded-lg shadow-2xl p-6 w-full max-w-lg" style="background:var(--content-bg);border:1px solid var(--border-color);max-height:90vh;overflow-y:auto">
          <div class="flex items-center justify-between mb-5">
            <h3 class="text-base font-semibold" style="color:var(--accent-color)">${isAdd ? 'Add Tag' : 'Edit Tag'}</h3>
            <button id="tag-modal-close" class="text-2xl leading-none opacity-50 hover:opacity-100" style="color:var(--content-text)">&times;</button>
          </div>
          <form id="tag-edit-form" class="space-y-3">
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">${isAdd ? 'Parent Path' : 'Path'}</label>
              <input type="text" value="${escapeHtml(data.path)}" disabled class="w-full px-3 py-2 text-xs rounded border opacity-50"
                     style="border-color:var(--border-color);background:color-mix(in srgb,var(--content-bg) 90%,black);color:var(--content-text)">
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Name</label>
              <input type="text" id="tag-name" value="${escapeHtml(data.name)}"
                     class="w-full px-3 py-2 text-xs rounded border"
                     style="border-color:${nameErr ? '#f87171' : 'var(--border-color)'};background:var(--content-bg);color:var(--content-text)" ${disabled}>
              ${nameErr ? `<p class="mt-1 text-xs" style="color:#f87171">${nameErr}</p>` : ''}
            </div>
            ${isAdd ? `
            <div class="flex items-center gap-3">
              <label class="flex items-center gap-2 text-xs font-medium cursor-pointer">
                <input type="checkbox" id="tag-is-array" class="rounded"
                       style="accent-color:var(--accent-color)"
                       ${this.tagArrayMode ? 'checked' : ''} ${disabled}>
                <span>Array</span>
              </label>
              ${this.tagArrayMode ? `
              <div class="flex items-center gap-2">
                <label class="text-xs font-medium opacity-70">Size</label>
                <input type="number" id="tag-array-size" value="${this.tagArraySize}" min="1" max="256"
                       class="w-20 px-2 py-1 text-xs rounded border text-center"
                       style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>
              </div>` : ''}
            </div>` : ''}
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">${isAdd && this.tagArrayMode ? 'Element Type' : 'Type'}</label>
              <select id="tag-type" ${isAdd && this.canWrite ? '' : 'disabled'} class="w-full px-3 py-2 text-xs rounded border"
                      style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text);${isAdd ? '' : 'opacity:.6'}">${typeOpts}</select>
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Description</label>
              <textarea id="tag-description" rows="2" class="w-full px-3 py-2 text-xs rounded border resize-none"
                        style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>${escapeHtml(data.description)}</textarea>
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Units</label>
              <input type="text" id="tag-units" value="${escapeHtml(data.units)}" placeholder="e.g. °C, kPa, m/s"
                     class="w-full px-3 py-2 text-xs rounded border"
                     style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>
            </div>
            <div>
              <label class="block text-xs font-medium mb-1 opacity-70">Deadband</label>
              <input type="number" id="tag-deadband" value="${escapeHtml(data.deadband)}" min="0" step="any" placeholder="0"
                     class="w-full px-3 py-2 text-xs rounded border"
                     style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>
            </div>
            ${enumEditor}
            ${this.tagArrayMode ? '' : this.renderPipelineEditor()}
            <div class="flex gap-2 pt-2">
              ${this.canWrite ? `<button type="submit" class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:var(--accent-color);color:var(--accent-text)">${isAdd ? 'Create Tag' : 'Save Changes'}</button>` : ''}
              <button type="button" id="tag-modal-cancel" class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">${this.canWrite ? 'Cancel' : 'Close'}</button>
            </div>
          </form>
        </div>
      </div>`;
  }

  private renderEnumValuesEditor(disabled: string): string {
    const values = this.editingTagData?.enumValues ?? {};
    const keys = sortEnumKeys(Object.keys(values));
    const isExpanded = this.enumValuesExpanded;
    const chevron = `<svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="transition:transform 0.15s;transform:rotate(${isExpanded ? '90deg' : '0deg'})"><polyline points="3,2 7,5 3,8"/></svg>`;
    const rows = keys.map((key, idx) => `
      <div class="enum-value-row grid grid-cols-[5.5rem_1fr_auto] gap-2 items-center" data-idx="${idx}">
        <input type="number" step="1" class="enum-value-key px-2 py-1.5 text-xs rounded border"
               value="${escapeHtml(key)}" placeholder="0"
               style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>
        <input type="text" class="enum-value-label px-2 py-1.5 text-xs rounded border"
               value="${escapeHtml(values[key])}" placeholder="Display label"
               style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)" ${disabled}>
        ${this.canWrite ? `<button type="button" class="enum-value-remove px-2 py-1.5 text-xs rounded"
                 data-idx="${idx}"
                 style="background:color-mix(in srgb,#ef4444 12%,transparent);color:#f87171">Remove</button>` : '<span></span>'}
      </div>`).join('');

    return `
      <div class="rounded border" style="border-color:var(--border-color);background:color-mix(in srgb,var(--content-bg) 94%,var(--accent-color))">
        <div id="enum-values-toggle" class="flex items-center justify-between px-2 py-1.5 cursor-pointer select-none">
          <div class="flex items-center gap-1.5">
            ${chevron}
            <label class="block text-xs font-medium opacity-70 cursor-pointer">Enum Values</label>
            <span class="text-xs opacity-45">${keys.length}</span>
          </div>
          ${this.canWrite ? `<button type="button" id="enum-value-add" class="px-2 py-1 text-xs rounded font-medium"
             style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color)">Add Value</button>` : ''}
        </div>
        ${isExpanded ? `<div id="enum-values-list" class="space-y-2 px-2 pb-2">
          ${rows || `<div class="text-xs opacity-45 py-2 text-center rounded border" style="border-color:var(--border-color)">No enum values configured.</div>`}
        </div>` : ''}
      </div>`;
  }

  // ─── Delete Confirm ──────────────────────────────────────────────────────────

  private renderDeleteConfirm(): string {
    if (!this.isDeleteConfirmOpen || !this.deleteTarget) return '';
    const { type, path } = this.deleteTarget;
    return `
      <div id="delete-confirm-modal" class="fixed inset-0 z-50 flex items-center justify-center" style="z-index:${TAGS_MANAGER_MODAL_Z_INDEX};background:rgba(0,0,0,.55)">
        <div class="rounded-lg shadow-2xl p-6 w-full max-w-sm" style="background:var(--content-bg);border:1px solid rgba(248,113,113,.4)">
          <h3 class="text-base font-semibold mb-2" style="color:#f87171">Confirm Delete</h3>
          <p class="text-xs mb-1 opacity-80">Delete ${type} <strong>${path}</strong>?</p>
          ${type === 'node'
            ? `<p class="text-xs mb-4" style="color:#f87171">⚠ All child nodes and tags will be permanently deleted.</p>`
            : `<p class="text-xs mb-4 opacity-50">This action cannot be undone.</p>`}
          <div class="flex gap-2">
            <button id="delete-confirm-yes" class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:#ef4444;color:#fff">Delete</button>
            <button id="delete-confirm-no"  class="flex-1 px-4 py-2 text-xs font-semibold rounded" style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">Cancel</button>
          </div>
        </div>
      </div>`;
  }

  // ─── Pipeline Debugger ───────────────────────────────────────────────────────

  private renderDebugger(): string {
    if (!this.isDebuggerOpen) return '';

    let resultsHtml = '';
    if (this.debugRunning) {
      resultsHtml = `<div class="text-xs opacity-50 py-4 text-center">Running…</div>`;
    } else if (this.debugError) {
      resultsHtml = `<div class="text-xs py-2" style="color:#f87171">${this.debugError}</div>`;
    } else if (this.debugResults !== null) {
      if (this.debugResults.length === 0) {
        resultsHtml = `<div class="text-xs opacity-50 py-2 text-center">No pipeline blocks - value passes through unchanged.</div>`;
      } else {
        resultsHtml = this.debugResults.map((step, i) => {
          const label = this.getBlockSchema(step.type)?.label ?? step.type;
          const state = step.stateChange
            ? `<span class="ml-1 px-1 rounded text-xs" style="background:rgba(239,68,68,.3);color:#f87171">→ ${step.stateChange}</span>` : '';
          const err = step.error ? `<div class="text-xs mt-1" style="color:#f87171">Error: ${step.error}</div>` : '';
          return `
            <div class="rounded p-2 mb-1" style="background:color-mix(in srgb,var(--accent-color) 6%,transparent);border:1px solid color-mix(in srgb,var(--accent-color) 15%,transparent)">
              <div class="flex items-center gap-1 mb-1">
                <span class="text-xs font-semibold" style="color:var(--accent-color)">${i + 1}. ${label}</span>${state}
              </div>
              <div class="flex items-center gap-2 text-xs flex-wrap">
                <span class="opacity-40">In:</span>
                <code class="px-1 rounded" style="background:color-mix(in srgb,var(--border-color) 40%,transparent)">${JSON.stringify(step.input)}</code>
                <span class="opacity-30">→</span>
                <span class="opacity-40">Out:</span>
                <code class="px-1 rounded font-bold" style="background:color-mix(in srgb,var(--accent-color) 18%,transparent);color:var(--accent-color)">${JSON.stringify(step.output)}</code>
              </div>
              ${err}
            </div>`;
        }).join('') + `
          <div class="flex items-center gap-2 mt-2 pt-2 text-xs" style="border-top:1px solid var(--border-color)">
            <span class="opacity-50">Final output:</span>
            <code class="px-2 py-0.5 rounded font-bold" style="background:color-mix(in srgb,var(--accent-color) 20%,transparent);color:var(--accent-color)">${JSON.stringify(this.debugFinalOutput)}</code>
          </div>`;
      }
    }

    return `
      <div id="debugger-modal" class="fixed inset-0 z-50 flex items-center justify-center" style="z-index:${TAGS_MANAGER_MODAL_Z_INDEX};background:rgba(0,0,0,.55)">
        <div class="rounded-lg shadow-2xl p-6 w-full max-w-lg" style="background:var(--content-bg);border:1px solid var(--border-color);max-height:90vh;overflow-y:auto">
          <div class="flex items-center justify-between mb-4">
            <div>
              <h3 class="text-base font-semibold" style="color:var(--accent-color)">Pipeline Debugger</h3>
              <div class="text-xs opacity-40 mt-0.5">${this.debugTagPath ?? ''}</div>
            </div>
            <button id="debugger-close" class="text-2xl leading-none opacity-50 hover:opacity-100" style="color:var(--content-text)">&times;</button>
          </div>
          <div class="flex items-center gap-2 mb-4">
            <input type="text" id="debug-input" value="${this.debugInput}"
                   class="flex-1 px-3 py-2 text-xs rounded border font-mono"
                   style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                   placeholder='Test input value - JSON (e.g. 42 or "hello")'>
            <button id="debug-run" class="px-4 py-2 text-xs font-semibold rounded flex-shrink-0" style="background:var(--accent-color);color:var(--accent-text)">▶ Run</button>
          </div>
          <div id="debug-results">${resultsHtml}</div>
        </div>
      </div>`;
  }

  // ─── Value Edit Modal ────────────────────────────────────────────────────────

  private renderValueEditModal(): string {
    if (!this.isValueEditOpen || !this.valueEditPath) return '';

    const store = getMirrorStore();
    const config = store.getNodeConfig(this.valueEditPath);
    const typeRaw = config?.type ?? this.valueEditType ?? 'string';
    const type = scalarTypeToString(typeRaw);
    const currentVal = this.valueEditCurrent ?? '';

    let inputHtml = '';
    switch (type) {
      case 'boolean':
        inputHtml = `<label class="flex items-center gap-2 px-3 py-2 text-sm">
          <input type="checkbox" id="value-edit-input" ${currentVal ? 'checked' : ''} style="accent-color:var(--accent-color)">
          <span>Value</span>
        </label>`;
        break;
      case 'integer':
      case 'float':
        inputHtml = `<input type="number" id="value-edit-input" value="${currentVal}" step="${type === 'float' ? 'any' : '1'}"
                            class="w-full px-3 py-2 text-sm rounded border"
                            style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                            placeholder="Enter ${type} value">`;
        break;
      default:
        inputHtml = `<input type="text" id="value-edit-input" value="${currentVal}"
                            class="w-full px-3 py-2 text-sm rounded border"
                            style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                            placeholder="Enter value">`;
    }

    return `
      <div id="value-edit-modal" class="fixed inset-0 z-50 flex items-center justify-center" style="z-index:${TAGS_MANAGER_MODAL_Z_INDEX};background:rgba(0,0,0,.55)">
        <div class="rounded-lg shadow-2xl p-6 w-full max-w-sm" style="background:var(--content-bg);border:1px solid var(--border-color)">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-base font-semibold" style="color:var(--accent-color)">Edit Value</h3>
            <button id="value-edit-close" class="text-2xl leading-none opacity-50 hover:opacity-100" style="color:var(--content-text)">&times;</button>
          </div>
          <div class="text-xs opacity-60 mb-3">${this.valueEditPath}</div>
          <div class="mb-4">${inputHtml}</div>
          <div class="flex gap-2">
            <button id="value-edit-accept" class="flex-1 px-4 py-2 text-sm font-semibold rounded" style="background:var(--accent-color);color:var(--accent-text)">Accept</button>
            <button id="value-edit-cancel" class="flex-1 px-4 py-2 text-sm font-semibold rounded" style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">Cancel</button>
          </div>
        </div>
      </div>`;
  }

  private hasOpenModal(): boolean {
    return this.isModalOpen || this.isTagModalOpen || this.isDeleteConfirmOpen || this.isDebuggerOpen || this.isValueEditOpen;
  }

  private applyModalStacking(open: boolean): void {
    const stackValue = open ? String(TAGS_MANAGER_MODAL_Z_INDEX) : '';
    const widgetBody = this.closest('widget-card')?.querySelector<HTMLElement>('.widget-body');
    if (widgetBody) {
      widgetBody.style.zIndex = stackValue;
      widgetBody.style.overflow = open ? 'visible' : '';
    }
    const gridItem = this.closest<HTMLElement>('.grid-stack-item');
    if (gridItem) gridItem.style.zIndex = stackValue;
  }

  // ─── Main Render ─────────────────────────────────────────────────────────────

  protected render(): void {
    this.tagCountCache.clear();
    this.matchingTagCountCache.clear();

    const sFilters = [
      { key: 'N', label: 'NORMAL', rgb: '34,197,94',   col: '#22c55e' },
      { key: 'A', label: 'ALARM',  rgb: '239,68,68',   col: '#ef4444' },
      { key: 'S', label: 'STALE',  rgb: '245,158,11',  col: '#f59e0b' },
      { key: 'D', label: 'DEV',    rgb: '59,130,246',  col: '#3b82f6' },
      { key: 'U', label: 'UNDEF',  rgb: '156,163,175', col: '#9ca3af' },
    ];
    const filterBtns = sFilters.map(f => {
      const active = this.statusFilter === f.key;
      return `<button class="tv-status-filter px-2 py-1 text-xs rounded font-medium"
                      style="background:${active ? f.col : `rgba(${f.rgb},.15)`};color:${active ? '#fff' : f.col};border:1px solid ${active ? f.col : `rgba(${f.rgb},.3)`}"
                      data-status="${f.key}">${f.label}</button>`;
    }).join('');

    // Render only the current user's org. Using tenant_id (rather than
    // iterating all store roots) prevents other orgs' NATS events from
    // leaking into this view.
    const orgRoot = getCurrentUser()?.tenant_id ?? '';
    const treeHtml = (orgRoot ? this.renderSubtree(orgRoot, 0) : '') ||
      `<div class="p-6 text-xs opacity-40 text-center">No nodes found.</div>`;

    // Control the widget-card header visibility (the card's header IS the title bar)
    const widgetCard = this.closest('widget-card');
    if (widgetCard) {
      const cardHeader = widgetCard.querySelector('.widget-header') as HTMLElement | null;
      if (cardHeader) cardHeader.style.display = this.config.showHeader ? '' : 'none';
    }
    this.applyModalStacking(this.hasOpenModal());

    this.innerHTML = `
      <div class="flex flex-col h-full overflow-hidden" style="color:var(--content-text)">
        <!-- Toolbar -->
        <div class="flex items-center gap-2 px-3 py-2 flex-shrink-0 flex-wrap" style="border-bottom:1px solid var(--border-color);background:color-mix(in srgb,var(--widget-header-bg,var(--content-bg)) 80%,transparent)">
          <input id="tv-search" type="text" class="flex-1 min-w-36 px-3 py-1 text-xs rounded border"
                 style="border-color:var(--border-color);background:var(--content-bg);color:var(--content-text)"
                 placeholder="Search tags and nodes…" value="${escapeHtml(this.searchQuery)}">
          ${filterBtns}
        </div>

        <!-- Tree body -->
        <div class="flex-1 overflow-auto">${treeHtml}</div>
      </div>

      ${this.renderModal()}
      ${this.renderTagModal()}
      ${this.renderDeleteConfirm()}
      ${this.renderDebugger()}
      ${this.renderValueEditModal()}`;
  }

  // ─── Event Wiring ────────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.querySelectorAll('.tv-node-row').forEach(el => el.addEventListener('click', this.handleNodeToggle));
    this.querySelectorAll('.tv-node-action').forEach(el => el.addEventListener('click', this.handleNodeAction));
    this.querySelectorAll('.tv-add-tag-btn').forEach(el => el.addEventListener('click', this.handleAddTagBtn));
    this.querySelectorAll('.tv-edit-tag').forEach(el => el.addEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-debug-tag').forEach(el => el.addEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-delete-tag').forEach(el => el.addEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-value-cell').forEach(el => el.addEventListener('click', this.handleValueCellClick));

    this.querySelector('#tv-search')?.addEventListener('input', this.handleSearchInput);
    this.querySelectorAll('.tv-status-filter').forEach(el => el.addEventListener('click', this.handleStatusFilter));

    // Node modal
    this.querySelector('#modal-close')?.addEventListener('click', this.closeModal);
    this.querySelector('#modal-cancel')?.addEventListener('click', this.closeModal);
    this.querySelector('#node-edit-form')?.addEventListener('submit', this.handleModalSubmit);
    this.querySelector('#node-editor-modal')?.addEventListener('click', e => { if (e.target === e.currentTarget) this.closeModal(); });
    if (this.canWrite) this.querySelector('#node-template-browse')?.addEventListener('click', this.handleTemplateBrowse);

    // Tag modal
    this.querySelector('#tag-modal-close')?.addEventListener('click', this.closeTagModal);
    this.querySelector('#tag-modal-cancel')?.addEventListener('click', this.closeTagModal);
    this.querySelector('#tag-edit-form')?.addEventListener('submit', this.handleTagModalSubmit);
    if (this.canWrite) this.querySelector('#tag-is-array')?.addEventListener('change', this.handleArrayToggle);
    this.querySelector('#tag-type')?.addEventListener('change', this.handleTagTypeChange);
    this.querySelector('#enum-values-toggle')?.addEventListener('click', this.handleEnumValuesToggle);
    this.querySelector('#enum-value-add')?.addEventListener('click', this.handleEnumValueAdd);
    this.querySelectorAll('.enum-value-remove').forEach(el => el.addEventListener('click', this.handleEnumValueRemove));
    this.querySelector('#tag-editor-modal')?.addEventListener('click', e => { if (e.target === e.currentTarget) this.closeTagModal(); });
    this.querySelector('#pipeline-add-block')?.addEventListener('click', this.handleAddPipelineBlock);
    this.querySelector('#pipeline-override')?.addEventListener('click', this.handlePipelineOverride);
    this.querySelector('#pipeline-revert')?.addEventListener('click', this.handlePipelineRevert);
    this.querySelectorAll('.pipeline-block-toggle').forEach(el => el.addEventListener('click', this.handleTogglePipelineBlock));
    this.querySelectorAll('.pipeline-remove').forEach(el => el.addEventListener('click', this.handleRemovePipelineBlock));
    this.querySelectorAll('.pipeline-move-up').forEach(el => el.addEventListener('click', this.handleMovePipelineBlock));
    this.querySelectorAll('.pipeline-move-down').forEach(el => el.addEventListener('click', this.handleMovePipelineBlock));

    // Delete confirm
    this.querySelector('#delete-confirm-yes')?.addEventListener('click', this.handleConfirmDelete);
    this.querySelector('#delete-confirm-no')?.addEventListener('click', this.closeDeleteConfirm);
    this.querySelector('#delete-confirm-modal')?.addEventListener('click', e => { if (e.target === e.currentTarget) this.closeDeleteConfirm(); });

    // Debugger
    this.querySelector('#debugger-close')?.addEventListener('click', this.closeDebugger);
    this.querySelector('#debug-run')?.addEventListener('click', this.handleRunDebug);
    this.querySelector('#debugger-modal')?.addEventListener('click', e => { if (e.target === e.currentTarget) this.closeDebugger(); });

    // Value edit modal
    this.querySelector('#value-edit-close')?.addEventListener('click', this.closeValueEdit);
    this.querySelector('#value-edit-cancel')?.addEventListener('click', this.closeValueEdit);
    this.querySelector('#value-edit-accept')?.addEventListener('click', this.handleValueEditAccept);
    this.querySelector('#value-edit-modal')?.addEventListener('click', e => { if (e.target === e.currentTarget) this.closeValueEdit(); });
  }

  protected detachEventListeners(): void {
    this.querySelectorAll('.tv-node-row').forEach(el => el.removeEventListener('click', this.handleNodeToggle));
    this.querySelectorAll('.tv-node-action').forEach(el => el.removeEventListener('click', this.handleNodeAction));
    this.querySelectorAll('.tv-add-tag-btn').forEach(el => el.removeEventListener('click', this.handleAddTagBtn));
    this.querySelectorAll('.tv-edit-tag').forEach(el => el.removeEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-debug-tag').forEach(el => el.removeEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-delete-tag').forEach(el => el.removeEventListener('click', this.handleTagAction));
    this.querySelectorAll('.tv-value-cell').forEach(el => el.removeEventListener('click', this.handleValueCellClick));
    this.querySelector('#tv-search')?.removeEventListener('input', this.handleSearchInput);
    this.querySelectorAll('.tv-status-filter').forEach(el => el.removeEventListener('click', this.handleStatusFilter));
    this.querySelector('#modal-close')?.removeEventListener('click', this.closeModal);
    this.querySelector('#modal-cancel')?.removeEventListener('click', this.closeModal);
    this.querySelector('#node-edit-form')?.removeEventListener('submit', this.handleModalSubmit);
    this.querySelector('#node-template-browse')?.removeEventListener('click', this.handleTemplateBrowse);
    this.querySelector('#tag-modal-close')?.removeEventListener('click', this.closeTagModal);
    this.querySelector('#tag-modal-cancel')?.removeEventListener('click', this.closeTagModal);
    this.querySelector('#tag-edit-form')?.removeEventListener('submit', this.handleTagModalSubmit);
    this.querySelector('#tag-is-array')?.removeEventListener('change', this.handleArrayToggle);
    this.querySelector('#tag-type')?.removeEventListener('change', this.handleTagTypeChange);
    this.querySelector('#enum-values-toggle')?.removeEventListener('click', this.handleEnumValuesToggle);
    this.querySelector('#enum-value-add')?.removeEventListener('click', this.handleEnumValueAdd);
    this.querySelectorAll('.enum-value-remove').forEach(el => el.removeEventListener('click', this.handleEnumValueRemove));
    this.querySelector('#pipeline-add-block')?.removeEventListener('click', this.handleAddPipelineBlock);
    this.querySelector('#pipeline-override')?.removeEventListener('click', this.handlePipelineOverride);
    this.querySelector('#pipeline-revert')?.removeEventListener('click', this.handlePipelineRevert);
    this.querySelector('#delete-confirm-yes')?.removeEventListener('click', this.handleConfirmDelete);
    this.querySelector('#delete-confirm-no')?.removeEventListener('click', this.closeDeleteConfirm);
    this.querySelector('#debugger-close')?.removeEventListener('click', this.closeDebugger);
    this.querySelector('#debug-run')?.removeEventListener('click', this.handleRunDebug);
    this.querySelector('#value-edit-close')?.removeEventListener('click', this.closeValueEdit);
    this.querySelector('#value-edit-cancel')?.removeEventListener('click', this.closeValueEdit);
    this.querySelector('#value-edit-accept')?.removeEventListener('click', this.handleValueEditAccept);
  }

  // ─── Toolbar Handlers ─────────────────────────────────────────────────────────

  private handleNodeToggle = (e: Event): void => {
    if ((e.target as HTMLElement).closest('button')) return;
    const path = (e.currentTarget as HTMLElement).dataset.nodePath;
    if (!path) return;
    const wasExpanded = this.expandedNodes.has(path);
    if (wasExpanded) this.expandedNodes.delete(path);
    else this.expandedNodes.add(path);
    this.rerender();
    if (!wasExpanded) {
      // Scroll expanded node to top of tree body so its children are visible below
      const nodeRow = this.querySelector(`[data-node-path="${path}"]`) as HTMLElement | null;
      nodeRow?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  };

  private handleSearchInput = (e: Event): void => {
    this.searchQuery = (e.target as HTMLInputElement).value;
    if (this.searchDebounceTimer) clearTimeout(this.searchDebounceTimer);
    this.searchDebounceTimer = setTimeout(() => {
      this.searchDebounceTimer = null;
      this.rerender();
    }, 200);
  };

  private handleStatusFilter = (e: Event): void => {
    const key = (e.currentTarget as HTMLElement).dataset.status!;
    this.statusFilter = this.statusFilter === key ? null : key;
    this.rerender();
  };

  // ─── Node Actions ─────────────────────────────────────────────────────────────

  private handleNodeAction = (e: Event): void => {
    e.stopPropagation();
    const btn    = e.currentTarget as HTMLElement;
    const action = btn.dataset.action;
    const row    = btn.closest('.tv-node-row') as HTMLElement;
    const path   = row?.dataset.nodePath;
    if (!action || !path) return;

    switch (action) {
      case 'edit':
        this.openEditModal(path);
        break;
      case 'delete':
        if (!this.canWrite) return;
        this.deleteTarget = { type: 'node', path };
        this.isDeleteConfirmOpen = true;
        this.rerender();
        break;
      case 'add-child':
        if (!this.canWrite) return;
        this.modalMode = 'add-child';
        this.parentPathForAdd = path;
        this.editingNodeData  = { path, name: '', description: '', templateName: '' };
        this.isModalOpen = true;
        this.validationErrors.clear();
        this.rerender();
        break;
      case 'add-sibling': {
        if (!this.canWrite) return;
        const parts = path.split('.');
        const parentPath = parts.length > 1 ? parts.slice(0, -1).join('.') : '';
        this.modalMode = 'add-sibling';
        this.parentPathForAdd = parentPath;
        this.editingNodeData  = { path: parentPath || '(root)', name: '', description: '', templateName: '' };
        this.isModalOpen = true;
        this.validationErrors.clear();
        this.rerender();
        break;
      }
    }
  };

  // ─── Tag Actions ──────────────────────────────────────────────────────────────

  private handleTagAction = (e: Event): void => {
    e.stopPropagation();
    const btn    = e.currentTarget as HTMLElement;
    const action = btn.dataset.action;
    const row    = btn.closest('.tv-leaf-row') as HTMLElement;
    const path   = row?.dataset.leafPath;
    if (!action || !path) return;
    switch (action) {
      case 'edit-tag':   this.openEditTagModal(path); break;
      case 'delete-tag': if (this.canWrite) { this.deleteTarget = { type: 'tag', path }; this.isDeleteConfirmOpen = true; this.rerender(); } break;
      case 'debug-tag':  if (this.canWrite) this.openDebugger(path); break;
    }
  };

  private handleAddTagBtn = (e: Event): void => {
    if (!this.canWrite) return;
    e.stopPropagation();
    const btn = e.currentTarget as HTMLElement;
    this.openAddTagModal(btn.dataset.parentPath || '');
  };

  private handleArrayToggle = (): void => {
    this.preserveTagModalFormValues();
    const arrayEl = this.querySelector('#tag-is-array') as HTMLInputElement | null;
    this.tagArrayMode = arrayEl?.checked ?? this.tagArrayMode;
    this.rerender();
  };

  private preserveOpenDialogFormValues(): void {
    if (this.isModalOpen) this.preserveNodeModalFormValues();
    if (this.isTagModalOpen) this.preserveTagModalFormValues();
    if (this.isDebuggerOpen) {
      const input = this.querySelector('#debug-input') as HTMLInputElement | null;
      if (input) this.debugInput = input.value;
    }
    if (this.isValueEditOpen) {
      const input = this.querySelector('#value-edit-input') as HTMLInputElement | null;
      if (input) this.valueEditCurrent = input.type === 'checkbox' ? input.checked : input.value;
    }
  }

  private preserveNodeModalFormValues(): void {
    if (!this.editingNodeData) return;
    const nameEl = this.querySelector('#node-name') as HTMLInputElement | null;
    const descEl = this.querySelector('#node-description') as HTMLTextAreaElement | null;
    const templateEl = this.querySelector('#node-template') as HTMLInputElement | null;
    const typeEl = this.querySelector('#node-type') as HTMLSelectElement | null;
    if (nameEl) this.editingNodeData.name = nameEl.value;
    if (descEl) this.editingNodeData.description = descEl.value;
    if (templateEl) this.editingNodeData.templateName = templateEl.value;
    if (typeEl) this.editingNodeData.nodeType = typeEl.value;
  }

  private preserveTagModalFormValues(): void {
    const nameEl = this.querySelector('#tag-name') as HTMLInputElement | null;
    const descEl = this.querySelector('#tag-description') as HTMLTextAreaElement | null;
    const unitsEl = this.querySelector('#tag-units') as HTMLInputElement | null;
    const deadbandEl = this.querySelector('#tag-deadband') as HTMLInputElement | null;
    const typeEl = this.querySelector('#tag-type') as HTMLSelectElement | null;
    const arrayEl = this.querySelector('#tag-is-array') as HTMLInputElement | null;
    const sizeEl = this.querySelector('#tag-array-size') as HTMLInputElement | null;
    if (nameEl && this.editingTagData) this.editingTagData.name = nameEl.value;
    if (descEl && this.editingTagData) this.editingTagData.description = descEl.value;
    if (unitsEl && this.editingTagData) this.editingTagData.units = unitsEl.value;
    if (deadbandEl && this.editingTagData) this.editingTagData.deadband = deadbandEl.value;
    if (typeEl && this.editingTagData) this.editingTagData.type = typeEl.value;
    if (arrayEl) this.tagArrayMode = arrayEl.checked;
    this.syncEnumValuesFromDOM();
    this.syncPipelineFromDOM();
    if (sizeEl) this.tagArraySize = Math.max(1, Math.min(256, parseInt(sizeEl.value, 10) || 4));
  }

  private syncEnumValuesFromDOM(): void {
    if (!this.editingTagData) return;
    if (!this.querySelector('#enum-values-list')) return;
    const values: Record<string, string> = {};
    this.querySelectorAll('.enum-value-row').forEach(row => {
      const key = (row.querySelector('.enum-value-key') as HTMLInputElement | null)?.value.trim();
      const label = (row.querySelector('.enum-value-label') as HTMLInputElement | null)?.value.trim();
      if (!key || !label) return;
      values[key] = label;
    });
    this.editingTagData.enumValues = values;
  }

  private handleEnumValuesToggle = (e: Event): void => {
    if ((e.target as HTMLElement).closest('#enum-value-add')) return;
    this.preserveTagModalFormValues();
    this.enumValuesExpanded = !this.enumValuesExpanded;
    this.rerender();
  };

  private handleTagTypeChange = (): void => {
    this.preserveTagModalFormValues();
    if (this.editingTagData?.type === 'enum' && Object.keys(this.editingTagData.enumValues).length === 0) {
      this.editingTagData.enumValues = { '0': 'Off', '1': 'On' };
    }
    this.enumValuesExpanded = this.editingTagData?.type === 'enum';
    this.rerender();
  };

  private handleEnumValueAdd = (e: Event): void => {
    e.preventDefault();
    e.stopPropagation();
    if (!this.canWrite || !this.editingTagData) return;
    this.preserveTagModalFormValues();
    const keys = Object.keys(this.editingTagData.enumValues).map(k => Number.parseInt(k, 10)).filter(Number.isFinite);
    const next = keys.length > 0 ? Math.max(...keys) + 1 : 0;
    this.editingTagData.enumValues[String(next)] = '';
    this.enumValuesExpanded = true;
    this.rerender();
  };

  private handleEnumValueRemove = (e: Event): void => {
    e.preventDefault();
    if (!this.canWrite || !this.editingTagData) return;
    const idx = parseInt((e.currentTarget as HTMLElement).dataset.idx ?? '-1', 10);
    this.preserveTagModalFormValues();
    const keys = sortEnumKeys(Object.keys(this.editingTagData.enumValues));
    if (idx >= 0 && idx < keys.length) delete this.editingTagData.enumValues[keys[idx]];
    this.rerender();
  };

  // ─── Node Modal Logic ─────────────────────────────────────────────────────────

  private openEditModal = async (path: string): Promise<void> => {
    const name = path.split('.').pop() || path;
    try {
      const data = await loadNode(path);
      this.editingNodeData = { path, name: data.name || name, description: data.description || '', templateName: data.templateName || '', nodeType: data.type || '' };
      if (typeof data.locked === 'boolean') this.lockedNodes.set(path, data.locked);
    } catch {
      this.editingNodeData = { path, name, description: '', templateName: '' };
    }
    this.modalMode = 'edit';
    this.editingNodePath = path;
    this.isModalOpen = true;
    this.validationErrors.clear();
    this.rerender();
  };

  private closeModal = (): void => {
    this.isModalOpen = false;
    this.editingNodePath = null;
    this.editingNodeData = null;
    this.parentPathForAdd = null;
    this.validationErrors.clear();
    this.rerender();
  };

  private handleTemplateBrowse = (): void => {
    if (!this.canWrite) return;
    getTreeBrowserDialog().open(
      TEMPLATES_ROOT,
      `Select Template`,
      (path) => {
        const input = this.querySelector('#node-template') as HTMLInputElement | null;
        if (input) input.value = path;
      },
    );
  };

  private handleModalSubmit = async (e: Event): Promise<void> => {
    e.preventDefault();
    if (!this.canWrite) return;
    if (!this.editingNodeData) return;

    const name         = (this.querySelector('#node-name')        as HTMLInputElement)?.value.trim()  || '';
    const description  = (this.querySelector('#node-description') as HTMLTextAreaElement)?.value       || '';
    const templateName = (this.querySelector('#node-template')    as HTMLInputElement)?.value.trim()  || '';
    const nodeTypeEl   = this.querySelector('#node-type')         as HTMLSelectElement | null;
    const nodeType     = (nodeTypeEl?.value ?? 'Standard') as NodeType;

    this.validationErrors.clear();
    if (!name) { this.validationErrors.set('name', 'Name is required'); this.rerender(); return; }
    if (!/^[a-zA-Z0-9_-]+$/.test(name)) { this.validationErrors.set('name', 'Only letters, numbers, _ and - allowed'); this.rerender(); return; }

    try {
      if (this.modalMode === 'edit') {
        await updateNode(this.editingNodePath!, { name, description, templateName });
      } else {
        const parentPath = this.parentPathForAdd ?? '';
        const newPath    = parentPath ? `${parentPath}.${name}` : name;
        await createNode(newPath, nodeType);
        if (description || templateName) await updateNode(newPath, { description, templateName });
        if (parentPath) this.expandedNodes.add(parentPath);
        else this.expandedNodes.add(newPath); // auto-expand new top-level node
      }
    } catch (err) {
      await showAlert(`Failed to save node: ${err}`, {
        title: 'Save failed',
        tone: 'danger',
      });
      return;
    }
    this.closeModal();
  };

  // ─── Tag Modal Logic ──────────────────────────────────────────────────────────

  private openAddTagModal = async (parentPath: string): Promise<void> => {
    if (!this.canWrite) return;
    this.tagModalMode = 'add';
    this.tagModalParentPath = parentPath;
    this.editingTagPath = null;
    this.editingTagData = { path: parentPath, name: '', description: '', units: '', deadband: '', type: 'float', enumValues: {} };
    this.tagPipeline = [];
    this.expandedPipelineBlocks = new Set();
    this.tagValidationErrors.clear();
    this.tagArrayMode = false;
    this.tagArraySize = 4;
    this.enumValuesExpanded = false;
    this.isTagModalOpen = true;
    try { this.blockSchemas = await loadBlockSchemas(); } catch { /* use cached */ }
    try { this.notificationProfiles = await listNotificationProfiles(); } catch { /* use cached */ }
    this.rerender();
  };

  private openEditTagModal = async (path: string): Promise<void> => {
    const name = path.split('.').pop() || path;
    this.tagModalMode = 'edit';
    this.editingTagPath = path;
    this.editingTagData = { path, name, description: '', units: '', deadband: '', type: 'float', enumValues: {} };
    this.tagPipeline = [];
    this.expandedPipelineBlocks = new Set();
    this.pipelineMode = 'editing';
    this.pipelineHasTemplate = false;
    this.enumValuesExpanded = false;
    this.tagValidationErrors.clear();
    try {
      const store = getMirrorStore();
      const shared = store.getNodeShared(path) || {};
      const config = store.getNodeConfig(path) || {};
      const pipeline = shared.pipeline || [];
      this.editingTagData = {
        path,
        name:        name,
        description: shared.description || '',
        units:       shared.units || '',
        deadband:    shared.deadband !== undefined && shared.deadband !== null ? String(shared.deadband) : '',
        type:        config.type !== undefined ? scalarTypeToString(config.type) : 'float',
        enumValues:  normalizeEnumValues(shared.enumValues),
      };
      this.tagPipeline = pipeline.map((env: any) => ({ type: env.type, params: env.parameters || {} }));
      this.pipelineMode = shared.pipelineInherited ? 'inherited' : 'editing';
      this.pipelineHasTemplate = !!(config.templateName);
      this.enumValuesExpanded = this.editingTagData.type === 'enum';
    } catch { /* use defaults */ }
    try { this.blockSchemas = await loadBlockSchemas(); } catch { /* use cached */ }
    try { this.notificationProfiles = await listNotificationProfiles(); } catch { /* use cached */ }
    this.isTagModalOpen = true;
    this.rerender();
  };

  private closeTagModal = (): void => {
    this.isTagModalOpen = false;
    this.editingTagPath = null;
    this.editingTagData = null;
    this.tagPipeline = [];
    this.pipelineMode = 'editing';
    this.pipelineHasTemplate = false;
    this.enumValuesExpanded = true;
    this.tagValidationErrors.clear();
    this.rerender();
  };

  // Read a value from params using a dot-path key (e.g. "hiEvent.enabled").
  private getNestedParam(params: Record<string, any>, key: string): any {
    const parts = key.split('.');
    let cur: any = params;
    for (const p of parts) {
      if (cur == null || typeof cur !== 'object') return undefined;
      cur = cur[p];
    }
    return cur;
  }

  // Write a value into params using a dot-path key, creating intermediate objects as needed.
  private setNestedParam(params: Record<string, any>, key: string, value: any): void {
    const parts = key.split('.');
    let cur: any = params;
    for (let i = 0; i < parts.length - 1; i++) {
      if (cur[parts[i]] == null || typeof cur[parts[i]] !== 'object') {
        cur[parts[i]] = {};
      }
      cur = cur[parts[i]];
    }
    cur[parts[parts.length - 1]] = value;
  }

  private syncPipelineFromDOM(): void {
    this.querySelectorAll('.pipeline-param').forEach(el => {
      const input = el as HTMLInputElement;
      const idx   = parseInt(input.dataset.blockIdx ?? '0');
      const param = input.dataset.param ?? '';
      if (idx >= this.tagPipeline.length || !param) return;
      const schema = this.getBlockSchema(this.tagPipeline[idx].type)?.params[param];
      let value: any;
      if (input.type === 'checkbox') {
        value = input.checked;
      } else if (schema?.type === 'number') {
        const n = parseFloat(input.value);
        value = isNaN(n) ? undefined : n;
      } else if (schema?.type === 'notification-profile') {
        value = parseInt(input.value, 10) || 0;
      } else {
        value = input.value;
      }
      this.setNestedParam(this.tagPipeline[idx].params, param, value);
    });
  }

  private handlePipelineOverride = (): void => {
    if (!this.canWrite) return;
    this.pipelineMode = 'editing';
    this.rerender();
  };

  private handlePipelineRevert = async (): Promise<void> => {
    if (!this.canWrite) return;
    if (!this.editingTagPath) return;
    try {
      const { updateTag } = await import('../../api');
      await updateTag(this.editingTagPath, { pipeline: [] } as any);
      // The NATS subscription will update the store with inherited pipeline.
      // Just update UI state to reflect the reverted state.
      this.tagPipeline = [];
      this.pipelineMode = 'inherited';
      this.rerender();
    } catch {
      await showAlert('Failed to revert pipeline to template', {
        title: 'Revert failed',
        tone: 'danger',
      });
    }
  };

  private handleTogglePipelineBlock = (e: Event): void => {
    const header = (e.currentTarget as HTMLElement);
    const idx = parseInt(header.dataset.blockIdx ?? '0');
    this.syncPipelineFromDOM();
    if (this.expandedPipelineBlocks.has(idx)) {
      this.expandedPipelineBlocks.delete(idx);
    } else {
      this.expandedPipelineBlocks.add(idx);
    }
    this.rerender();
  };

  private handleAddPipelineBlock = (e: Event): void => {
    if (!this.canWrite) return;
    e.preventDefault();
    this.syncPipelineFromDOM();
    const type   = (this.querySelector('#pipeline-block-type') as HTMLSelectElement)?.value ?? 'scaling';
    const schema = this.getBlockSchema(type);
    const params: Record<string, any> = {};
    if (schema) for (const [k, def] of Object.entries(schema.params)) if (def.default !== undefined) params[k] = def.default;
    this.tagPipeline.push({ type, params });
    this.rerender();
  };

  private handleRemovePipelineBlock = (e: Event): void => {
    if (!this.canWrite) return;
    e.preventDefault();
    this.syncPipelineFromDOM();
    const idx = parseInt((e.currentTarget as HTMLElement).dataset.idx ?? '0');
    this.tagPipeline.splice(idx, 1);
    this.expandedPipelineBlocks = new Set(
      [...this.expandedPipelineBlocks].filter(n => n !== idx).map(n => n > idx ? n - 1 : n)
    );
    this.rerender();
  };

  private handleMovePipelineBlock = (e: Event): void => {
    if (!this.canWrite) return;
    e.preventDefault();
    this.syncPipelineFromDOM();
    const btn  = e.currentTarget as HTMLElement;
    const idx  = parseInt(btn.dataset.idx ?? '0');
    const swap = btn.classList.contains('pipeline-move-up') ? idx - 1 : idx + 1;
    if (swap < 0 || swap >= this.tagPipeline.length) return;
    [this.tagPipeline[idx], this.tagPipeline[swap]] = [this.tagPipeline[swap], this.tagPipeline[idx]];
    const idxExp  = this.expandedPipelineBlocks.has(idx);
    const swapExp = this.expandedPipelineBlocks.has(swap);
    idxExp  ? this.expandedPipelineBlocks.add(swap)  : this.expandedPipelineBlocks.delete(swap);
    swapExp ? this.expandedPipelineBlocks.add(idx)   : this.expandedPipelineBlocks.delete(idx);
    this.rerender();
  };

  private handleTagModalSubmit = async (e: Event): Promise<void> => {
    e.preventDefault();
    if (!this.canWrite) return;
    if (!this.editingTagData) return;
    this.syncEnumValuesFromDOM();
    this.syncPipelineFromDOM();

    const name        = (this.querySelector('#tag-name')        as HTMLInputElement)?.value.trim() || '';
    const type        = (this.querySelector('#tag-type')        as HTMLSelectElement)?.value || 'float';
    const description = (this.querySelector('#tag-description') as HTMLTextAreaElement)?.value || '';
    const units       = (this.querySelector('#tag-units')       as HTMLInputElement)?.value.trim() || '';
    const deadbandRaw = (this.querySelector('#tag-deadband')    as HTMLInputElement)?.value.trim() || '';
    const deadband    = deadbandRaw === '' ? 0 : Number(deadbandRaw);
    const enumValues  = type === 'enum' ? { ...this.editingTagData.enumValues } : undefined;
    const pipeline: PipelineBlockEnvelope[] = this.tagPipeline.map(b => ({ type: b.type, parameters: b.params }));

    this.tagValidationErrors.clear();
    if (!name) { this.tagValidationErrors.set('name', 'Name is required'); this.rerender(); return; }
    if (!/^[a-zA-Z0-9_-]+$/.test(name)) { this.tagValidationErrors.set('name', 'Only letters, numbers, _ and - allowed'); this.rerender(); return; }
    if (!Number.isFinite(deadband) || deadband < 0) {
      await showAlert('Deadband must be zero or a positive number.', {
        title: 'Invalid deadband',
        tone: 'danger',
      });
      return;
    }
    if (type === 'enum' && Object.keys(enumValues ?? {}).length === 0) {
      await showAlert('Enum tags need at least one value.', {
        title: 'Enum values required',
      });
      return;
    }
    if (type === 'enum' && Object.keys(enumValues ?? {}).some(key => !/^-?\d+$/.test(key))) {
      await showAlert('Enum value IDs must be whole numbers.', {
        title: 'Invalid enum value',
      });
      return;
    }

    try {
      if (this.tagModalMode === 'add') {
        const parent  = this.tagModalParentPath ?? '';
        const newPath = parent ? `${parent}.${name}` : name;

        if (this.tagArrayMode) {
          // Create array container node + numbered child tags
          const size = parseInt((this.querySelector('#tag-array-size') as HTMLInputElement)?.value || String(this.tagArraySize), 10) || this.tagArraySize;
          await createNode(newPath, undefined, true);
          for (let i = 0; i < size; i++) {
            const elemPath = `${newPath}.${i}`;
            await createTag(elemPath, type, { description, units, deadband, enumValues });
            if (units || deadband !== 0) await updateTag(elemPath, { description, units, deadband });
          }
        } else {
          await createTag(newPath, type, { description, units, deadband, enumValues });
          if (pipeline.length > 0) await updateTag(newPath, { pipeline });
        }
        if (parent) this.expandedNodes.add(parent);
      } else if (this.pipelineMode === 'inherited') {
        // Pipeline is from template - only save description/units, leave pipeline untouched
        await updateTag(this.editingTagPath!, { name, description, units, deadband, enumValues } as any);
      } else {
        // Local override - save the edited pipeline blocks
        await updateTag(this.editingTagPath!, { name, description, units, deadband, enumValues, pipeline });
      }
    } catch (err) {
      await showAlert(`Failed to save tag: ${err}`, {
        title: 'Save failed',
        tone: 'danger',
      });
      return;
    }
    this.closeTagModal();
  };

  // ─── Delete Logic ─────────────────────────────────────────────────────────────

  private closeDeleteConfirm = (): void => { this.isDeleteConfirmOpen = false; this.deleteTarget = null; this.rerender(); };

  private handleConfirmDelete = async (): Promise<void> => {
    if (!this.canWrite) return;
    if (!this.deleteTarget) return;
    const { type, path } = this.deleteTarget;
    try {
      if (type === 'node') {
        // Unlock the node itself first - top-level nodes are auto-locked on creation
        await updateNode(path, { locked: false } as any).catch(() => {});
        await deleteNode(path);
      } else {
        await deleteTag(path);
      }
    } catch (err) {
      await showAlert(`Failed to delete: ${err}`, {
        title: 'Delete failed',
        tone: 'danger',
      });
      return;
    }
    this.isDeleteConfirmOpen = false;
    this.deleteTarget = null;
    this.lockedNodes.delete(path);
    getMirrorStore().removeNode(path);
    this.rerender();
  };

  // ─── Debugger Logic ───────────────────────────────────────────────────────────

  private openDebugger(path: string): void {
    this.debugTagPath = path; this.debugInput = ''; this.debugResults = null;
    this.debugFinalOutput = undefined; this.debugError = ''; this.debugRunning = false;
    this.isDebuggerOpen = true;
    this.rerender();
  }

  private closeDebugger = (): void => { this.isDebuggerOpen = false; this.debugTagPath = null; this.debugResults = null; this.rerender(); };

  private handleRunDebug = async (): Promise<void> => {
    if (!this.canWrite) return;
    if (!this.debugTagPath) return;
    const raw = (this.querySelector('#debug-input') as HTMLInputElement)?.value ?? '';
    this.debugInput = raw; this.debugRunning = true; this.debugError = ''; this.debugResults = null;
    this.rerender();
    let parsed: any;
    try { parsed = JSON.parse(raw); } catch { parsed = raw; }
    try {
      const res = await debugTagPipeline(this.debugTagPath, parsed);
      this.debugResults = res.steps; this.debugFinalOutput = res.finalOutput; this.debugError = '';
    } catch (err) { this.debugError = String(err); this.debugResults = null; }
    this.debugRunning = false;
    this.rerender();
  };

  // ─── Value Edit Handlers ──────────────────────────────────────────────────────

  private handleValueCellClick = (e: Event): void => {
    if (!this.canWrite) return;
    e.stopPropagation();
    const cell = e.currentTarget as HTMLElement;
    const path = cell.dataset.leafPath;
    if (!path) return;

    const store = getMirrorStore();
    const config = store.getNodeConfig(path);
    const currentValue = store.getNodeValue(path);

    this.valueEditPath = path;
    this.valueEditType = scalarTypeToString(config?.type);
    this.valueEditCurrent = currentValue?.value ?? currentValue ?? '';
    this.isValueEditOpen = true;
    this.rerender();
  };

  private closeValueEdit = (): void => {
    this.isValueEditOpen = false;
    this.valueEditPath = null;
    this.valueEditType = null;
    this.valueEditCurrent = null;
    this.querySelector('#value-edit-modal')?.remove();
  };

  private handleValueEditAccept = async (): Promise<void> => {
    if (!this.canWrite) return;
    if (!this.valueEditPath) return;

    const input = this.querySelector('#value-edit-input') as HTMLInputElement;
    if (!input) return;

    let value: any;
    const type = scalarTypeToString(this.valueEditType ?? 'string');

    switch (type) {
      case 'boolean':
        value = input.checked;
        break;
      case 'integer':
        value = parseInt(input.value, 10);
        if (isNaN(value)) {
          await showAlert('Invalid integer value', {
            title: 'Invalid value',
            tone: 'danger',
          });
          return;
        }
        break;
      case 'float':
        value = parseFloat(input.value);
        if (isNaN(value)) {
          await showAlert('Invalid float value', {
            title: 'Invalid value',
            tone: 'danger',
          });
          return;
        }
        break;
      default:
        value = input.value;
    }

    try {
      await updateTagValue(this.valueEditPath, value);
      this.closeValueEdit();
    } catch (err) {
      await showAlert(`Failed to update value: ${err}`, {
        title: 'Update failed',
        tone: 'danger',
      });
    }
  };

  // ─── Rerender ─────────────────────────────────────────────────────────────────

  private rerender(): void {
    const activeSearchInput = this.querySelector<HTMLInputElement>('#tv-search');
    const restoreSearchFocus = document.activeElement === activeSearchInput;
    if (restoreSearchFocus && activeSearchInput) {
      this.searchQuery = activeSearchInput.value;
    }
    const searchSelectionStart = restoreSearchFocus ? activeSearchInput?.selectionStart : null;
    const searchSelectionEnd = restoreSearchFocus ? activeSearchInput?.selectionEnd : null;

    this.preserveOpenDialogFormValues();
    const treeBody = this.querySelector('.flex-1.overflow-auto') as HTMLElement | null;
    const scrollTop = treeBody?.scrollTop ?? 0;
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    if (scrollTop > 0) {
      const newTreeBody = this.querySelector('.flex-1.overflow-auto') as HTMLElement | null;
      if (newTreeBody) newTreeBody.scrollTop = scrollTop;
    }
    if (restoreSearchFocus) {
      const newSearchInput = this.querySelector<HTMLInputElement>('#tv-search');
      if (newSearchInput) {
        newSearchInput.focus();
        const valueLength = newSearchInput.value.length;
        const start = Math.min(searchSelectionStart ?? valueLength, valueLength);
        const end = Math.min(searchSelectionEnd ?? start, valueLength);
        newSearchInput.setSelectionRange(start, end);
      }
    }
  }
}

customElements.define('tags-manager-widget', TagsManagerWidget);
