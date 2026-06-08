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

  setEditMode(editing: boolean): void {
    this.setMode(editing ? 'edit' : 'view');
  }

  setMode(mode: WidgetCardMode): void {
    this.mode = mode;
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
  }

  protected render(): void {
    const canInspect = this.mode !== 'view';
    this.innerHTML = `
      <div class="widget-card" style="position:relative;">
        <div class="widget-header">
          <div class="wc-title">${this.sanitizeTitle(this.widgetTitle)}</div>
          <div class="wc-actions flex items-center gap-1" style="display: ${canInspect ? 'flex' : 'none'};">
            <span class="widget-action wc-configure" title="Configure widget" style="display: ${canInspect && this.hasProperties ? 'inline-block' : 'none'};">&#9881;</span>
            <span class="widget-action wc-duplicate" title="Duplicate widget" style="display: ${canInspect ? 'inline-block' : 'none'};">&#10697;</span>
            <span class="widget-action wc-delete" title="Delete widget" style="display: ${canInspect ? 'inline-block' : 'none'};">&#10005;</span>
          </div>
        </div>
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
  }

  protected detachEventListeners(): void {
    this.querySelector('.wc-configure')?.removeEventListener('click', this.handleConfigure);
    this.querySelector('.wc-duplicate')?.removeEventListener('click', this.handleDuplicate);
    this.querySelector('.wc-delete')?.removeEventListener('click', this.handleDelete);
  }

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
