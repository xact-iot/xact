export interface ThemeDefinition {
  id: string;
  name: string;
  preview: string; // CSS color for the swatch preview
}

export interface WidgetDecorationDefinition {
  id: string;
  name: string;
  description: string;
}

const THEMES: ThemeDefinition[] = [
  { id: 'dark-navy',        name: 'Dark Navy',        preview: '#0a1628' },
  { id: 'green-blue-chill', name: 'Green Blue Chill',  preview: '#0a2327' },
  { id: 'lilac-elegance',   name: 'Lilac Elegance',    preview: '#1a1530' },
  { id: 'voltage',          name: 'Voltage',           preview: '#1a1a1a' },
  { id: 'sandstone',        name: 'Sandstone',         preview: '#f4efe6' },
];

const WIDGET_DECORATIONS: WidgetDecorationDefinition[] = [
  { id: 'flat',     name: 'Flat',     description: 'Minimal widget surface' },
  { id: 'bordered', name: 'Bordered', description: 'Crisp framed widgets' },
  { id: 'shadowed', name: 'Shadowed', description: 'Soft depth below widgets' },
  { id: 'raised',   name: 'Raised',   description: 'Stronger widget elevation' },
];

const DEFAULT_THEME = 'dark-navy';
const DEFAULT_WIDGET_DECORATION = 'shadowed';

export class ThemeManager {
  private static instance: ThemeManager;
  private currentTheme: string = DEFAULT_THEME;
  private currentWidgetDecoration: string = DEFAULT_WIDGET_DECORATION;
  private pluginThemes: ThemeDefinition[] = [];
  private pluginWidgetDecorations: WidgetDecorationDefinition[] = [];
  private listeners: Set<(theme: string) => void> = new Set();
  private widgetDecorationListeners: Set<(decoration: string) => void> = new Set();
  private listChangeListeners: Set<() => void> = new Set();
  private widgetDecorationListChangeListeners: Set<() => void> = new Set();

  private constructor() {
    const savedTheme = localStorage.getItem('xact-theme');
    if (savedTheme && THEMES.some(t => t.id === savedTheme)) {
      this.setTheme(savedTheme);
    } else if (savedTheme) {
      // May be a plugin theme - set the attribute now so the CSS applies as
      // soon as the plugin injects its <style> block.
      this.currentTheme = savedTheme;
      document.documentElement.setAttribute('data-theme', savedTheme);
    } else {
      this.setTheme(DEFAULT_THEME);
    }

    const savedDecoration = localStorage.getItem('xact-widget-decoration');
    if (savedDecoration && WIDGET_DECORATIONS.some(d => d.id === savedDecoration)) {
      this.setWidgetDecoration(savedDecoration);
    } else if (savedDecoration) {
      // May be a plugin decoration. Apply the attribute now; registered CSS can
      // style it as soon as the plugin loads.
      this.currentWidgetDecoration = savedDecoration;
      document.documentElement.setAttribute('data-widget-decoration', savedDecoration);
    } else {
      this.setWidgetDecoration(DEFAULT_WIDGET_DECORATION);
    }
  }

  static getInstance(): ThemeManager {
    if (!ThemeManager.instance) {
      ThemeManager.instance = new ThemeManager();
    }
    return ThemeManager.instance;
  }

  getAvailableThemes(): ThemeDefinition[] {
    return [...THEMES, ...this.pluginThemes];
  }

  getAvailableWidgetDecorations(): WidgetDecorationDefinition[] {
    return [...WIDGET_DECORATIONS, ...this.pluginWidgetDecorations];
  }

  setTheme(theme: string): void {
    this.currentTheme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('xact-theme', theme);
    this.notifyListeners();
  }

  getTheme(): string {
    return this.currentTheme;
  }

  setWidgetDecoration(decoration: string): void {
    this.currentWidgetDecoration = decoration;
    document.documentElement.setAttribute('data-widget-decoration', decoration);
    localStorage.setItem('xact-widget-decoration', decoration);
    this.notifyWidgetDecorationListeners();
  }

  getWidgetDecoration(): string {
    return this.currentWidgetDecoration;
  }

  /**
   * Called by a plugin to inject a new theme into the application.
   * @param def     - Theme metadata (id, name, preview colour)
   * @param cssText - Complete CSS block for the theme, e.g.
   *                  `[data-theme="my-theme"] { --accent-color: red; … }`
   */
  registerTheme(def: ThemeDefinition, cssText: string): void {
    // Inject CSS into the document
    const existing = document.getElementById(`theme-plugin-${def.id}`);
    if (existing) existing.remove();

    const style = document.createElement('style');
    style.id = `theme-plugin-${def.id}`;
    style.textContent = cssText;
    document.head.appendChild(style);

    // Add to plugin theme registry if not already present
    if (!this.pluginThemes.some(t => t.id === def.id)) {
      this.pluginThemes.push(def);
    }

    // If this theme was already active (set from localStorage before plugin
    // loaded), notify change listeners so the UI can update.
    if (this.currentTheme === def.id) {
      this.notifyListeners();
    }

    // Always notify list-change listeners so the preferences dialog can
    // add the new theme to its list.
    this.notifyListChangeListeners();
  }

  /**
   * Called by a plugin to add a widget frame decoration independent of colour.
   * The CSS should target `[data-widget-decoration="<id>"]`.
   */
  registerWidgetDecoration(def: WidgetDecorationDefinition, cssText: string): void {
    const existing = document.getElementById(`widget-decoration-plugin-${def.id}`);
    if (existing) existing.remove();

    const style = document.createElement('style');
    style.id = `widget-decoration-plugin-${def.id}`;
    style.textContent = cssText;
    document.head.appendChild(style);

    if (!this.pluginWidgetDecorations.some(d => d.id === def.id)) {
      this.pluginWidgetDecorations.push(def);
    }

    if (this.currentWidgetDecoration === def.id) {
      this.notifyWidgetDecorationListeners();
    }

    this.notifyWidgetDecorationListChangeListeners();
  }

  onThemeChange(callback: (theme: string) => void): () => void {
    this.listeners.add(callback);
    return () => {
      this.listeners.delete(callback);
    };
  }

  onWidgetDecorationChange(callback: (decoration: string) => void): () => void {
    this.widgetDecorationListeners.add(callback);
    return () => {
      this.widgetDecorationListeners.delete(callback);
    };
  }

  onWidgetDecorationListChange(callback: () => void): () => void {
    this.widgetDecorationListChangeListeners.add(callback);
    return () => {
      this.widgetDecorationListChangeListeners.delete(callback);
    };
  }

  /** Subscribe to changes in the available theme list (plugin themes added). */
  onThemeListChange(callback: () => void): () => void {
    this.listChangeListeners.add(callback);
    return () => {
      this.listChangeListeners.delete(callback);
    };
  }

  private notifyListeners(): void {
    const snapshot = [...this.listeners];
    snapshot.forEach(callback => callback(this.currentTheme));
  }

  private notifyWidgetDecorationListeners(): void {
    const snapshot = [...this.widgetDecorationListeners];
    snapshot.forEach(callback => callback(this.currentWidgetDecoration));
  }

  private notifyWidgetDecorationListChangeListeners(): void {
    const snapshot = [...this.widgetDecorationListChangeListeners];
    snapshot.forEach(callback => callback());
  }

  private notifyListChangeListeners(): void {
    const snapshot = [...this.listChangeListeners];
    snapshot.forEach(callback => callback());
  }
}

export const themeManager = ThemeManager.getInstance();
