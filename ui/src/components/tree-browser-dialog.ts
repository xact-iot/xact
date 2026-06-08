import { BaseComponent } from './base-component';
import { getMirrorStore } from '../store/store';

const TAG_REFERENCE_SUFFIXES = [
  { label: 'Value',      suffix: '',   desc: "The tag's current value" },
  { label: 'Description', suffix: ':description', desc: "The tag's shared description" },
  { label: 'Units',      suffix: ':units', desc: "The tag's shared units" },
  { label: 'Deadband',   suffix: ':deadband', desc: "The tag's shared deadband" },
  { label: 'Undefined',  suffix: ':U', desc: 'Tag has never received a value' },
  { label: 'Stale',      suffix: ':S', desc: 'Tag data has not been updated recently' },
  { label: 'Alarm',      suffix: ':A', desc: 'Tag is in alarm' },
  { label: 'Deviation',  suffix: ':D', desc: 'Tag shows a deviation' },
  { label: 'Normal',     suffix: ':N', desc: 'Tag is operating normally' },
] as const;

export class TreeBrowserDialog extends BaseComponent {
  private isOpen = false;
  private rootPath = '';
  private dialogTitle = 'Select Node';
  private expandedNodes: Set<string> = new Set();
  private onSelect: ((path: string) => void) | null = null;
  private treeUnsubscribe: (() => void) | null = null;
  private includeLeaves = false;
  private selectedPath = '';
  // When non-null, the suffix picker is shown instead of the tree
  private pendingLeafPath: string | null = null;

  // ─── Public API ─────────────────────────────────────────────────────────────

  /**
   * @param rootPath     Start of the tree to display (empty string = full tree)
   * @param title        Dialog heading
   * @param onSelect     Called with the selected dot-path when the user picks a node/leaf
   * @param includeLeaves When true, tag leaves are shown and selectable in addition to nodes
   * @param expandTo     Optional path to auto-expand to (all ancestors along this path are opened)
   * @param selectedPath  Optional path to mark as the current selection
   */
  open(
    rootPath: string,
    title: string,
    onSelect: (path: string) => void,
    includeLeaves = false,
    expandTo = '',
    selectedPath = '',
  ): void {
    const store = getMirrorStore();

    // When rootPath is empty, default to the org node so its children are shown
    // directly - the user never sees the org name as a tree root.
    // Convert any relative rootPath to absolute for internal store operations.
    const effectiveRoot = rootPath
      ? store.toAbsolute(rootPath)
      : store.getOrg();

    this.rootPath = effectiveRoot;
    this.dialogTitle = title;
    this.onSelect = onSelect;
    this.includeLeaves = includeLeaves;
    this.selectedPath = selectedPath ? store.toAbsolute(this.pathWithoutStatusSuffix(selectedPath)) : '';
    this.expandedNodes = new Set([effectiveRoot]); // auto-expand root

    // Auto-expand every ancestor along the expandTo path
    const pathToExpand = expandTo || this.selectedPath;
    if (pathToExpand) {
      const absExpandTo = store.toAbsolute(this.pathWithoutStatusSuffix(pathToExpand));
      const parts = absExpandTo.split('.');
      const rootParts = effectiveRoot ? effectiveRoot.split('.') : [];
      for (let i = rootParts.length; i <= parts.length; i++) {
        const ancestor = parts.slice(0, i).join('.');
        this.expandedNodes.add(ancestor);
      }
    }

    this.pendingLeafPath = null;
    this.isOpen = true;

    if (this.treeUnsubscribe) this.treeUnsubscribe();
    this.treeUnsubscribe = store.subscribeToTreeChanges(effectiveRoot, () => {
      if (this.isOpen) this.rerender();
    });

    this.rerender();
  }

  close(): void {
    if (this.treeUnsubscribe) { this.treeUnsubscribe(); this.treeUnsubscribe = null; }
    this.isOpen = false;
    this.onSelect = null;
    this.selectedPath = '';
    this.pendingLeafPath = null;
    this.rerender();
  }

  // ─── Rendering ──────────────────────────────────────────────────────────────

  protected render(): void {
    if (!this.isOpen) {
      this.innerHTML = '';
      return;
    }

    const store = getMirrorStore();
    // Empty rootPath means "show the full tree from root" - treat as always existing,
    // since nodeExists('') splits on '.' and looks for a child named '' which never matches.
    const rootExists = !this.rootPath || store.nodeExists(this.rootPath);

    const body = this.pendingLeafPath
      ? this.renderSuffixPicker(this.pendingLeafPath)
      : (rootExists
          ? this.renderSubtree(this.rootPath, 0)
          : `<div class="px-4 py-6 text-xs text-center opacity-40">
               No "<strong>${this.rootPath}</strong>" node found in the tree.
             </div>`);

    const subtitle = this.pendingLeafPath
      ? `<span style="opacity:0.5">Choose what to return for this tag</span>`
      : (this.includeLeaves ? 'Click a node or tag to select it' : 'Click a node to select it');

    this.innerHTML = `
      <div class="fixed inset-0 flex items-center justify-center" style="background:rgba(0,0,0,.55);z-index:23000;">
        <div class="rounded-lg shadow-2xl flex flex-col" style="
            background:var(--content-bg);
            border:1px solid var(--border-color);
            width:420px;
            max-height:70vh;">

          <!-- Header -->
          <div class="flex items-center justify-between px-4 py-3 flex-shrink-0"
               style="border-bottom:1px solid var(--border-color)">
            <div>
              <h3 class="text-sm font-semibold" style="color:var(--accent-color)">${this.dialogTitle}</h3>
              <div class="text-xs mt-0.5">${subtitle}</div>
            </div>
            <button id="tbd-close" class="text-xl leading-none opacity-50 hover:opacity-100"
                    style="color:var(--content-text)">&times;</button>
          </div>

          <!-- Body -->
          <div class="flex-1 overflow-y-auto py-1">
            ${body}
          </div>

          <!-- Footer -->
          <div class="flex justify-end px-4 py-3 flex-shrink-0"
               style="border-top:1px solid var(--border-color)">
            ${this.pendingLeafPath
              ? `<button id="tbd-back" class="px-4 py-1.5 text-xs rounded font-medium mr-auto"
                         style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">
                   ← Back
                 </button>`
              : ''}
            <button id="tbd-cancel" class="px-4 py-1.5 text-xs rounded font-medium"
                    style="background:color-mix(in srgb,var(--border-color) 50%,transparent);color:var(--content-text)">
              Cancel
            </button>
          </div>
        </div>
      </div>`;
  }

  private tagTypeName(childPath: string): string {
    const typeNum = getMirrorStore().getNodeConfig(childPath)?.type;
    switch (typeNum) {
      case 0: return 'integer';
      case 1: return 'float';
      case 2: return 'string';
      case 3: return 'boolean';
      case 4: return 'enum';
      default: return '';
    }
  }

  private renderSubtree(path: string, depth: number): string {
    const store = getMirrorStore();
    const parentName = path ? path.split('.').pop()! : '';
    const children = store.listChildrenNames(path)
      .filter(c => !(c === parentName && !store.listChildrenNames(path ? `${path}.${c}` : c).length))
      .sort((a, b) => a.localeCompare(b));
    if (children.length === 0) return '';

    let html = '';
    for (const childName of children) {
      const childPath = path ? `${path}.${childName}` : childName;
      const isLeaf = store.getNodeType(childPath) === 'leaf';

      if (isLeaf && !this.includeLeaves) continue;

      const indent = depth * 16 + 12;

      if (isLeaf) {
        const isSelected = childPath === this.selectedPath;
        const typeName = this.tagTypeName(childPath);
        const typeBadge = typeName
          ? `<span style="flex-shrink:0;font-size:10px;padding:1px 5px;border-radius:3px;background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);opacity:0.75;letter-spacing:0.03em;">${typeName}</span>`
          : '';
        // Display name: numeric children of array parents show as [0], [1], etc.
        const leafParentIsArray = path ? store.getIsArray(path) : false;
        const leafDisplayName = leafParentIsArray && /^\d+$/.test(childName) ? `[${childName}]` : childName;
        // Leaf (tag) row - selectable, no expand toggle
        html += `
          <div class="tbd-node flex items-center gap-1 py-1 cursor-pointer hover:opacity-80 select-none"
               style="padding-left:${indent}px;padding-right:12px;border-bottom:1px solid color-mix(in srgb,var(--border-color) 15%,transparent);${isSelected ? 'background:color-mix(in srgb,var(--accent-color) 18%,transparent);' : ''}"
               ${isSelected ? 'data-selected="true"' : ''}
               data-path="${childPath}">
            <span style="width:14px;flex-shrink:0"></span>
            <svg class="w-3 h-3 flex-shrink:0" style="flex-shrink:0;opacity:0.4;color:var(--accent-color)" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M7 7h.01M7 3h5c.512 0 1.024.195 1.414.586l7 7a2 2 0 010 2.828l-7 7a2 2 0 01-2.828 0l-7-7A2 2 0 013 12V7a2 2 0 014-4z"/>
            </svg>
            <span style="font-size:12px;color:var(--content-text);opacity:0.75;" data-select="${childPath}">${leafDisplayName}</span>
            ${typeBadge}
          </div>`;
        continue;
      }

      const isExpanded = this.expandedNodes.has(childPath);
      const isSelected = childPath === this.selectedPath;
      const childNames = store.listChildrenNames(childPath);
      const isArrayNode = store.getIsArray(childPath);
      // When showing leaves, any child at all makes the node expandable.
      // When nodes-only mode, only non-leaf children count.
      const hasChildren = this.includeLeaves
        ? childNames.length > 0
        : childNames.some(n => store.getNodeType(childPath ? `${childPath}.${n}` : n) !== 'leaf');

      // Display name: numeric children of array parents show as [0], [1], etc.
      const parentIsArray = path ? store.getIsArray(path) : false;
      const displayName = parentIsArray && /^\d+$/.test(childName) ? `[${childName}]` : childName;

      // Icon: array nodes get brackets icon, regular nodes get folder icon
      const nodeIcon = isArrayNode
        ? `<svg class="w-3.5 h-3.5 flex-shrink-0 opacity-50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
             <path d="M8 3H6a2 2 0 00-2 2v14a2 2 0 002 2h2M16 3h2a2 2 0 012 2v14a2 2 0 01-2 2h-2"/>
           </svg>`
        : `<svg class="w-3.5 h-3.5 flex-shrink-0 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
             <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                   d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z"/>
           </svg>`;

      // Array badge showing element count
      const arrayBadge = isArrayNode
        ? `<span style="flex-shrink:0;font-size:10px;padding:1px 5px;border-radius:3px;background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color);opacity:0.75;letter-spacing:0.03em;">[${childNames.length}]</span>`
        : '';

      html += `
        <div class="tbd-node flex items-center gap-1 py-1.5 cursor-pointer hover:opacity-80 select-none"
             style="padding-left:${indent}px;padding-right:12px;border-bottom:1px solid color-mix(in srgb,var(--border-color) 25%,transparent);${isSelected ? 'background:color-mix(in srgb,var(--accent-color) 18%,transparent);' : ''}"
             ${isSelected ? 'data-selected="true"' : ''}
             data-path="${childPath}">
          <span class="tbd-toggle text-xs opacity-50 inline-block transition-transform flex-shrink-0"
                style="width:14px;color:var(--content-text);${isExpanded ? 'transform:rotate(90deg)' : ''}"
                data-toggle="${childPath}">
            ${hasChildren ? '▶' : ''}
          </span>
          ${nodeIcon}
          <span class="text-xs font-medium" style="color:var(--accent-color)" data-select="${childPath}">${displayName}</span>
          ${arrayBadge}
        </div>`;

      if (isExpanded) {
        html += this.renderSubtree(childPath, depth + 1);
      }
    }
    return html;
  }

  private renderSuffixPicker(leafPath: string): string {
    const displayPath = getMirrorStore().toRelative(leafPath);
    let html = `
      <div class="px-4 pt-3 pb-2">
        <div class="text-xs opacity-50 mb-2 font-mono" style="word-break:break-all;">${displayPath}</div>
      </div>`;
    for (const opt of TAG_REFERENCE_SUFFIXES) {
      const resultPath = leafPath + opt.suffix;
      const badge = opt.suffix
        ? `<span style="flex-shrink:0;font-size:10px;padding:1px 6px;border-radius:3px;background:color-mix(in srgb,var(--accent-color) 15%,transparent);color:var(--accent-color);font-family:monospace;letter-spacing:0.05em;">${opt.suffix}</span>`
        : `<span style="flex-shrink:0;font-size:10px;padding:1px 6px;border-radius:3px;background:color-mix(in srgb,var(--border-color) 40%,transparent);color:var(--content-text);opacity:0.5;font-family:monospace;">value</span>`;
      html += `
        <div class="tbd-suffix flex items-center gap-2 px-4 py-2.5 cursor-pointer hover:opacity-80 select-none"
             style="border-bottom:1px solid color-mix(in srgb,var(--border-color) 20%,transparent)"
             data-result="${resultPath}">
          ${badge}
          <div>
            <div class="text-xs font-medium" style="color:var(--content-text)">${opt.label}</div>
            <div class="text-xs opacity-40">${opt.desc}</div>
          </div>
        </div>`;
    }
    return html;
  }

  // ─── Events ─────────────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.querySelector('#tbd-close')?.addEventListener('click', this.handleClose);
    this.querySelector('#tbd-cancel')?.addEventListener('click', this.handleClose);
    this.querySelector('#tbd-back')?.addEventListener('click', this.handleBack);

    this.querySelectorAll('.tbd-node').forEach(el => {
      el.addEventListener('click', this.handleNodeClick);
    });

    this.querySelectorAll('.tbd-suffix').forEach(el => {
      el.addEventListener('click', this.handleSuffixClick);
    });

    // Close on backdrop click
    this.querySelector('.fixed')?.addEventListener('click', this.handleBackdropClick);
  }

  protected detachEventListeners(): void {
    this.querySelector('#tbd-close')?.removeEventListener('click', this.handleClose);
    this.querySelector('#tbd-cancel')?.removeEventListener('click', this.handleClose);
    this.querySelector('#tbd-back')?.removeEventListener('click', this.handleBack);
    this.querySelectorAll('.tbd-node').forEach(el => {
      el.removeEventListener('click', this.handleNodeClick);
    });
    this.querySelectorAll('.tbd-suffix').forEach(el => {
      el.removeEventListener('click', this.handleSuffixClick);
    });
    this.querySelector('.fixed')?.removeEventListener('click', this.handleBackdropClick);
  }

  private handleClose = (): void => { this.close(); };

  private handleBackdropClick = (e: Event): void => {
    if (e.target === e.currentTarget) this.close();
  };

  private handleBack = (): void => {
    this.pendingLeafPath = null;
    this.rerender();
  };

  private handleSuffixClick = (e: Event): void => {
    e.stopPropagation();
    const el = e.currentTarget as HTMLElement;
    const result = el.dataset.result;
    if (result === undefined) return;
    if (this.onSelect) {
      // Strip org prefix so callers receive org-relative paths
      const store = getMirrorStore();
      // result may include a :STATUS suffix - strip org from the path portion only
      const colonIdx = result.lastIndexOf(':');
      if (colonIdx !== -1) {
        const basePath = result.slice(0, colonIdx);
        const suffix = result.slice(colonIdx);
        this.onSelect(store.toRelative(basePath) + suffix);
      } else {
        this.onSelect(store.toRelative(result));
      }
    }
    this.close();
  };

  private handleNodeClick = (e: Event): void => {
    e.stopPropagation();
    const el = e.currentTarget as HTMLElement;
    const path = el.dataset.path;
    if (!path) return;

    const target = e.target as HTMLElement;
    const store = getMirrorStore();
    const isLeaf = store.getNodeType(path) === 'leaf';

    // Clicking the node name text explicitly selects
    if (target.dataset.select || target.closest('[data-select]')) {
      if (isLeaf) {
        this.pendingLeafPath = path;
        this.rerender();
      } else {
        if (this.onSelect) this.onSelect(store.toRelative(path));
        this.close();
      }
      return;
    }

    // For non-leaf nodes, clicking anywhere else on the row toggles expand/collapse
    if (!isLeaf) {
      if (this.expandedNodes.has(path)) this.expandedNodes.delete(path);
      else this.expandedNodes.add(path);
      this.rerender();
      return;
    }

    // Leaf rows have no expand - any click selects
    this.pendingLeafPath = path;
    this.rerender();
  };

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.scrollSelectedIntoView();
  }

  private pathWithoutStatusSuffix(path: string): string {
    return getMirrorStore().baseTagPath(path);
  }

  private scrollSelectedIntoView(): void {
    const selected = this.querySelector<HTMLElement>('[data-selected="true"]');
    selected?.scrollIntoView({ block: 'nearest' });
  }

  disconnectedCallback(): void {
    if (this.treeUnsubscribe) { this.treeUnsubscribe(); this.treeUnsubscribe = null; }
    super.disconnectedCallback();
  }
}

customElements.define('tree-browser-dialog', TreeBrowserDialog);

// ─── Singleton helper ────────────────────────────────────────────────────────
// Keeps a single dialog element attached to <body> so it works across all widgets.

let _singleton: TreeBrowserDialog | null = null;

export function getTreeBrowserDialog(): TreeBrowserDialog {
  if (!_singleton || !document.body.contains(_singleton)) {
    _singleton = document.createElement('tree-browser-dialog') as unknown as TreeBrowserDialog;
    document.body.appendChild(_singleton);
  }
  return _singleton;
}
