import { BaseComponent } from './base-component';
import { themeManager } from '../themes/theme-manager';

export class PreferencesDialog extends BaseComponent {
  private unsubscribeTheme: (() => void) | null = null;
  private unsubscribeWidgetDecoration: (() => void) | null = null;
  private unsubscribeThemeList: (() => void) | null = null;
  private unsubscribeWidgetDecorationList: (() => void) | null = null;

  protected render(): void {
    const themes = themeManager.getAvailableThemes();
    const decorations = themeManager.getAvailableWidgetDecorations();
    const currentTheme = themeManager.getTheme();
    const currentDecoration = themeManager.getWidgetDecoration();

    this.className = 'fixed inset-0 flex items-center justify-center hidden';
    this.style.zIndex = '20000';

    this.innerHTML = `
      <div id="pref-backdrop" class="absolute inset-0 bg-black/50"></div>
      <div class="relative w-full max-w-3xl mx-4 rounded-xl border shadow-2xl" style="background-color: var(--modal-bg); color: var(--modal-text); border-color: var(--border-color);">
        <div class="flex items-center justify-between px-5 py-4 border-b" style="border-color: var(--border-color);">
          <h2 class="text-lg font-semibold">Preferences</h2>
          <button id="pref-close" class="p-1 rounded-lg hover:opacity-70 transition-opacity" title="Close">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-5 p-5">
          <div>
            <h3 class="text-xs font-semibold uppercase tracking-wider mb-3 opacity-60">Theme Color</h3>
            <div class="grid grid-cols-1 gap-2" id="theme-list">
              ${this.renderThemeItems(themes, currentTheme)}
            </div>
          </div>

          <div>
            <h3 class="text-xs font-semibold uppercase tracking-wider mb-3 opacity-60">Widget Decoration</h3>
            <div class="grid grid-cols-1 gap-2" id="decoration-list">
              ${this.renderDecorationItems(decorations, currentDecoration)}
            </div>
          </div>
        </div>
      </div>
    `;
  }

  private renderThemeItems(themes: ReturnType<typeof themeManager.getAvailableThemes>, current: string): string {
    return themes.map(t => `
      <button class="theme-option flex items-center gap-3 px-4 py-3 rounded-lg border transition-all duration-150 ${t.id === current ? 'ring-2' : 'opacity-70 hover:opacity-100'}"
              style="border-color: var(--border-color); background-color: var(--content-bg); color: var(--content-text); ${t.id === current ? `--tw-ring-color: var(--accent-color);` : ''}"
              data-theme="${t.id}">
        <span class="w-8 h-8 rounded-full border shrink-0" style="background-color: ${t.preview}; border-color: color-mix(in srgb, ${t.preview} 50%, white);"></span>
        <span class="text-sm font-medium">${t.name}</span>
        ${t.id === current ? '<span class="active-badge ml-auto text-xs" style="color: var(--accent-color);">Active</span>' : ''}
      </button>
    `).join('');
  }

  private renderDecorationItems(
    decorations: ReturnType<typeof themeManager.getAvailableWidgetDecorations>,
    current: string
  ): string {
    return decorations.map(d => `
      <button class="decoration-option flex items-center gap-3 px-4 py-3 rounded-lg border transition-all duration-150 text-left ${d.id === current ? 'ring-2' : 'opacity-70 hover:opacity-100'}"
              style="border-color: var(--border-color); background-color: var(--content-bg); color: var(--content-text); ${d.id === current ? `--tw-ring-color: var(--accent-color);` : ''}"
              data-decoration="${d.id}">
        <span class="decoration-preview decoration-preview-${d.id}" aria-hidden="true"></span>
        <span class="min-w-0">
          <span class="block text-sm font-medium">${d.name}</span>
          <span class="block text-xs opacity-60">${d.description}</span>
        </span>
        ${d.id === current ? '<span class="active-badge ml-auto text-xs shrink-0" style="color: var(--accent-color);">Active</span>' : ''}
      </button>
    `).join('');
  }

  private refreshThemeList(): void {
    const list = this.querySelector('#theme-list');
    if (!list) return;
    const themes = themeManager.getAvailableThemes();
    const current = themeManager.getTheme();
    list.innerHTML = this.renderThemeItems(themes, current);
    // Re-attach click listeners for the newly rendered buttons
    this.querySelectorAll('.theme-option').forEach(el =>
      el.addEventListener('click', this.handleThemeSelect)
    );
  }

  private refreshDecorationList(): void {
    const list = this.querySelector('#decoration-list');
    if (!list) return;
    const decorations = themeManager.getAvailableWidgetDecorations();
    const current = themeManager.getWidgetDecoration();
    list.innerHTML = this.renderDecorationItems(decorations, current);
    this.querySelectorAll('.decoration-option').forEach(el =>
      el.addEventListener('click', this.handleDecorationSelect)
    );
  }

  protected attachEventListeners(): void {
    this.querySelector('#pref-close')?.addEventListener('click', this.close);
    this.querySelector('#pref-backdrop')?.addEventListener('click', this.close);
    this.querySelectorAll('.theme-option').forEach(el =>
      el.addEventListener('click', this.handleThemeSelect)
    );
    this.querySelectorAll('.decoration-option').forEach(el =>
      el.addEventListener('click', this.handleDecorationSelect)
    );
    document.addEventListener('keydown', this.handleKeydown);

    this.unsubscribeTheme = themeManager.onThemeChange((newTheme: string) => {
      this.updateActiveTheme(newTheme);
    });

    this.unsubscribeWidgetDecoration = themeManager.onWidgetDecorationChange((newDecoration: string) => {
      this.updateActiveDecoration(newDecoration);
    });

    this.unsubscribeThemeList = themeManager.onThemeListChange(() => {
      this.refreshThemeList();
    });

    this.unsubscribeWidgetDecorationList = themeManager.onWidgetDecorationListChange(() => {
      this.refreshDecorationList();
    });
  }

  protected detachEventListeners(): void {
    this.querySelector('#pref-close')?.removeEventListener('click', this.close);
    this.querySelector('#pref-backdrop')?.removeEventListener('click', this.close);
    this.querySelectorAll('.theme-option').forEach(el =>
      el.removeEventListener('click', this.handleThemeSelect)
    );
    this.querySelectorAll('.decoration-option').forEach(el =>
      el.removeEventListener('click', this.handleDecorationSelect)
    );
    document.removeEventListener('keydown', this.handleKeydown);

    if (this.unsubscribeTheme) {
      this.unsubscribeTheme();
      this.unsubscribeTheme = null;
    }

    if (this.unsubscribeWidgetDecoration) {
      this.unsubscribeWidgetDecoration();
      this.unsubscribeWidgetDecoration = null;
    }

    if (this.unsubscribeThemeList) {
      this.unsubscribeThemeList();
      this.unsubscribeThemeList = null;
    }

    if (this.unsubscribeWidgetDecorationList) {
      this.unsubscribeWidgetDecorationList();
      this.unsubscribeWidgetDecorationList = null;
    }
  }

  private handleThemeSelect = (e: Event): void => {
    const themeId = (e.currentTarget as HTMLElement).dataset.theme;
    if (themeId) {
      themeManager.setTheme(themeId);
    }
  };

  private handleDecorationSelect = (e: Event): void => {
    const decorationId = (e.currentTarget as HTMLElement).dataset.decoration;
    if (decorationId) {
      themeManager.setWidgetDecoration(decorationId);
    }
  };

  private handleKeydown = (e: KeyboardEvent): void => {
    if (e.key === 'Escape') this.close();
  };

  private updateActiveTheme(activeId: string): void {
    this.querySelectorAll('.theme-option').forEach(el => {
      const btn = el as HTMLElement;
      const id = btn.dataset.theme;
      const isActive = id === activeId;
      btn.className = `theme-option flex items-center gap-3 px-4 py-3 rounded-lg border transition-all duration-150 ${isActive ? 'ring-2' : 'opacity-70 hover:opacity-100'}`;
      // Update the "Active" badge
      const badge = btn.querySelector('.active-badge');
      if (badge) badge.remove();
      if (isActive) {
        btn.insertAdjacentHTML('beforeend', `<span class="active-badge ml-auto text-xs" style="color: var(--accent-color);">Active</span>`);
      }
    });
  }

  private updateActiveDecoration(activeId: string): void {
    this.querySelectorAll('.decoration-option').forEach(el => {
      const btn = el as HTMLElement;
      const id = btn.dataset.decoration;
      const isActive = id === activeId;
      btn.className = `decoration-option flex items-center gap-3 px-4 py-3 rounded-lg border transition-all duration-150 text-left ${isActive ? 'ring-2' : 'opacity-70 hover:opacity-100'}`;
      const badge = btn.querySelector('.active-badge');
      if (badge) badge.remove();
      if (isActive) {
        btn.insertAdjacentHTML('beforeend', `<span class="active-badge ml-auto text-xs shrink-0" style="color: var(--accent-color);">Active</span>`);
      }
    });
  }

  open = (): void => {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.classList.remove('hidden');
  };

  close = (): void => {
    this.classList.add('hidden');
  };
}

customElements.define('preferences-dialog', PreferencesDialog);
