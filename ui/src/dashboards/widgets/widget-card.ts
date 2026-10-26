import { BaseComponent } from '../../components/base-component';
import { showConfirm } from '../../components/app-dialog';
import { sanitizeHtml } from '../../utils/html-sanitize';

export type WidgetCardMode = 'view' | 'inspect' | 'edit';

export class WidgetCard extends BaseComponent {
  private widgetTitle = 'Widget';
  private widgetId = '';
  private mode: WidgetCardMode = 'view';
  private hasProperties = false;
  private headerVisible = true;
  /** Whether viewers may expand this widget to full screen. */
  private fullscreenAllowed = false;
  /** 'native' (Fullscreen API), 'window' (maximised in the page) or null. */
  private fullscreenState: 'native' | 'window' | null = null;
  private static readonly HEADER_TITLE_TAGS = new Set([
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'b', 'strong', 'i', 'em', 'u', 's', 'span', 'small', 'br', 'code', 'sub', 'sup',
  ]);

  setTitle(title: string): void {
    this.widgetTitle = title;
    const titleEl = this.querySelector('.wc-title');
    if (titleEl) titleEl.innerHTML = this.sanitizeTitle(title);
    this._applyHeaderVisibility();
  }

  setWidgetId(id: string): void {
    this.widgetId = id;
  }

  getWidgetId(): string {
    return this.widgetId;
  }

  setHeaderVisible(visible: boolean): void {
    this.headerVisible = visible;
    this._applyHeaderVisibility();
  }

  setHasProperties(hasProps: boolean): void {
    this.hasProperties = hasProps;
    this.updateActionsDisplay();
  }

  /** Allows or forbids expanding this widget to full screen. */
  setFullscreenAllowed(allowed: boolean): void {
    this.fullscreenAllowed = allowed;
    if (!allowed && this.fullscreenState) void this.exitFullscreen();
    this.updateActionsDisplay();
  }

  isFullscreenAllowed(): boolean {
    return this.fullscreenAllowed;
  }

  isFullscreen(): boolean {
    return this.fullscreenState !== null;
  }

  /**
   * Expands the widget to fill the screen. The widget stays where it is in
   * the page (moving it would disconnect it and reset its state): the
   * browser's Fullscreen API shows the card alone, and where that is not
   * available (iPhone Safari, frames without permission) the card is
   * maximised over the page instead.
   */
  async enterFullscreen(): Promise<void> {
    if (this.fullscreenState || !this.fullscreenAllowed) return;
    const target = this.cardElement();
    if (!target) return;
    let native = false;
    if (document.fullscreenEnabled && typeof target.requestFullscreen === 'function') {
      try {
        await target.requestFullscreen({ navigationUI: 'hide' });
        native = true;
      } catch { /* refused: maximise in the page instead */ }
    }
    if (!native) {
      this.classList.add('wc-maximised');
      this.closest('.grid-stack-item')?.classList.add('wc-maximised-item');
      document.addEventListener('keydown', this.handleKeydown);
    }
    this.setFullscreenState(native ? 'native' : 'window');
  }

  async exitFullscreen(): Promise<void> {
    if (!this.fullscreenState) return;
    if (this.fullscreenState === 'native' && document.fullscreenElement === this.cardElement()) {
      try { await document.exitFullscreen(); } catch { /* already left */ }
    }
    this.classList.remove('wc-maximised');
    this.closest('.grid-stack-item')?.classList.remove('wc-maximised-item');
    document.removeEventListener('keydown', this.handleKeydown);
    this.setFullscreenState(null);
  }

  private cardElement(): HTMLElement | null {
    return this.querySelector<HTMLElement>(':scope > .widget-card');
  }

  private setFullscreenState(state: 'native' | 'window' | null): void {
    if (this.fullscreenState === state) return;
    this.fullscreenState = state;
    this.classList.toggle('wc-fullscreen', state !== null);
    this.updateActionsDisplay();
    // Tell the widget, then let size-dependent widgets (maps, charts,
    // canvases) re-measure once the new layout has applied.
    const widget = this.querySelector('.widget-body')?.firstElementChild as any;
    if (widget && typeof widget.setFullscreen === 'function') {
      try { widget.setFullscreen(state !== null); } catch (err) { console.error('setFullscreen failed', err); }
    }
    this.emit('widget-fullscreen-change', { widgetId: this.widgetId, fullscreen: state !== null });
    requestAnimationFrame(() => window.dispatchEvent(new Event('resize')));
  }

  setEditMode(editing: boolean): void {
    this.setMode(editing ? 'edit' : 'view');
  }

  setMode(mode: WidgetCardMode): void {
    this.mode = mode;
    if (mode === 'edit' && this.fullscreenState) void this.exitFullscreen();
    this.updateActionsDisplay();
    this._applyHeaderVisibility();
  }

  private _applyHeaderVisibility(): void {
    const header = this.querySelector<HTMLElement>('.widget-header');
    const title  = this.querySelector<HTMLElement>('.wc-title');
    const actions = this.querySelector<HTMLElement>('.wc-actions');
    if (!header) return;
    const canInspect = this.mode !== 'view';
    const hasTitle = this.widgetTitle.trim() !== '';
    const showHeader = canInspect || (this.headerVisible && hasTitle);

    if (showHeader) {
      header.style.height    = '';
      header.style.padding   = '';
      header.style.border    = '';
      header.style.overflow  = '';
      header.style.position  = '';
      if (title)   { title.style.display   = hasTitle ? '' : 'none'; }
      if (actions) { actions.style.position = ''; actions.style.top = ''; actions.style.right = ''; }
    } else {
      header.style.height    = '0';
      header.style.padding   = '0';
      header.style.border    = 'none';
      header.style.overflow  = 'hidden';
      header.style.position  = 'relative';
      if (title) title.style.display = 'none';
      if (actions) {
        actions.style.position = '';
        actions.style.top      = '';
        actions.style.right    = '';
      }
    }
  }

  private updateActionsDisplay(): void {
    const actions = this.querySelector('.wc-actions') as HTMLElement | null;
    const configBtn = this.querySelector('.wc-configure') as HTMLElement | null;
    const duplicateBtn = this.querySelector('.wc-duplicate') as HTMLElement | null;
    const deleteBtn = this.querySelector('.wc-delete') as HTMLElement | null;
    const fsToggle = this.querySelector('.wc-fs-toggle') as HTMLElement | null;
    const expandBtn = this.querySelector('.wc-expand') as HTMLButtonElement | null;
    const canInspect = this.mode !== 'view';
    if (actions) {
      actions.style.display = canInspect ? 'flex' : 'none';
    }
    if (configBtn) {
      configBtn.style.display = canInspect && this.hasProperties ? 'inline-block' : 'none';
    }
    if (duplicateBtn) {
      duplicateBtn.style.display = canInspect ? 'inline-block' : 'none';
    }
    if (deleteBtn) {
      deleteBtn.style.display = canInspect ? 'inline-block' : 'none';
    }
    if (fsToggle) {
      fsToggle.style.display = this.mode === 'edit' ? 'inline-block' : 'none';
      fsToggle.classList.toggle('wc-on', this.fullscreenAllowed);
      fsToggle.setAttribute('aria-pressed', String(this.fullscreenAllowed));
      fsToggle.title = this.fullscreenAllowed
        ? 'Full screen allowed: viewers can expand this widget (click to turn off)'
        : 'Allow viewers to expand this widget to full screen';
    }
    if (expandBtn) {
      // Not in edit mode, where dragging and resizing own the card.
      expandBtn.hidden = !this.fullscreenAllowed || this.mode === 'edit';
      const full = this.fullscreenState !== null;
      expandBtn.innerHTML = full ? WidgetCard.COLLAPSE_ICON : WidgetCard.EXPAND_ICON;
      expandBtn.title = full ? 'Exit full screen (Esc)' : 'Full screen';
      expandBtn.setAttribute('aria-label', expandBtn.title);
    }
  }

  private static readonly EXPAND_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"/></svg>';
  private static readonly COLLAPSE_ICON = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7"/></svg>';

  protected render(): void {
    const canInspect = this.mode !== 'view';
    this.innerHTML = `
      <div class="widget-card" style="position:relative;">
        <div class="widget-header">
          <div class="wc-title">${this.sanitizeTitle(this.widgetTitle)}</div>
          <div class="wc-actions flex items-center gap-1" style="display: ${canInspect ? 'flex' : 'none'};">
            <span class="widget-action wc-fs-toggle" role="button" tabindex="0" style="display: ${this.mode === 'edit' ? 'inline-block' : 'none'};">&#10530;</span>
            <span class="widget-action wc-configure" title="Configure widget" style="display: ${canInspect && this.hasProperties ? 'inline-block' : 'none'};">&#9881;</span>
            <span class="widget-action wc-duplicate" title="Duplicate widget" style="display: ${canInspect ? 'inline-block' : 'none'};">&#10697;</span>
            <span class="widget-action wc-delete" title="Delete widget" style="display: ${canInspect ? 'inline-block' : 'none'};">&#10005;</span>
          </div>
        </div>
        <button type="button" class="wc-expand" hidden></button>
        <div class="widget-body">
          <slot></slot>
        </div>
      </div>
    `;
    // Move existing children (the actual widget) into .widget-body
    const body = this.querySelector('.widget-body')!;
    const slotEl = body.querySelector('slot');
    if (slotEl) slotEl.remove();
    this.updateActionsDisplay();
    this._applyHeaderVisibility();
  }

  protected attachEventListeners(): void {
    this.querySelector('.wc-configure')?.addEventListener('click', this.handleConfigure);
    this.querySelector('.wc-duplicate')?.addEventListener('click', this.handleDuplicate);
    this.querySelector('.wc-delete')?.addEventListener('click', this.handleDelete);
    this.querySelector('.wc-fs-toggle')?.addEventListener('click', this.handleFullscreenToggle);
    this.querySelector<HTMLElement>('.wc-fs-toggle')?.addEventListener('keydown', this.handleToggleKey);
    this.querySelector('.wc-expand')?.addEventListener('click', this.handleExpand);
    document.addEventListener('fullscreenchange', this.handleFullscreenChange);
  }

  protected detachEventListeners(): void {
    this.querySelector('.wc-configure')?.removeEventListener('click', this.handleConfigure);
    this.querySelector('.wc-duplicate')?.removeEventListener('click', this.handleDuplicate);
    this.querySelector('.wc-delete')?.removeEventListener('click', this.handleDelete);
    this.querySelector('.wc-fs-toggle')?.removeEventListener('click', this.handleFullscreenToggle);
    this.querySelector<HTMLElement>('.wc-fs-toggle')?.removeEventListener('keydown', this.handleToggleKey);
    this.querySelector('.wc-expand')?.removeEventListener('click', this.handleExpand);
    document.removeEventListener('fullscreenchange', this.handleFullscreenChange);
    document.removeEventListener('keydown', this.handleKeydown);
    if (this.fullscreenState === 'window') {
      this.closest('.grid-stack-item')?.classList.remove('wc-maximised-item');
      this.classList.remove('wc-maximised', 'wc-fullscreen');
      this.fullscreenState = null;
    }
  }

  private handleFullscreenToggle = (): void => {
    this.emit('widget-fullscreen-allowed', { widgetId: this.widgetId, allowed: !this.fullscreenAllowed });
  };

  private handleToggleKey = (e: KeyboardEvent): void => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      this.handleFullscreenToggle();
    }
  };

  private handleExpand = (e: Event): void => {
    e.stopPropagation();
    if (this.fullscreenState) void this.exitFullscreen();
    else void this.enterFullscreen();
  };

  // The browser leaves native full screen on Esc or its own controls.
  private handleFullscreenChange = (): void => {
    if (this.fullscreenState === 'native' && document.fullscreenElement !== this.cardElement()) {
      this.setFullscreenState(null);
    }
  };

  private handleKeydown = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && this.fullscreenState === 'window') {
      e.stopPropagation();
      void this.exitFullscreen();
    }
  };

  private handleConfigure = (): void => {
    this.emit('widget-configure', { widgetId: this.widgetId });
  };

  private handleDuplicate = (): void => {
    this.emit('widget-duplicate', { widgetId: this.widgetId });
  };

  private handleDelete = async (): Promise<void> => {
    const confirmed = await showConfirm(`Delete "${this.widgetTitle}"?`, {
      title: 'Delete widget',
      confirmLabel: 'Delete',
      cancelLabel: 'Keep',
      tone: 'danger',
    });
    if (!confirmed) return;
    this.emit('widget-delete', { widgetId: this.widgetId });
  };

  private sanitizeTitle(title: string): string {
    return sanitizeHtml(title, { allowedTags: WidgetCard.HEADER_TITLE_TAGS });
  }
}

customElements.define('widget-card', WidgetCard);
