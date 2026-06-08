/**
 * html-editor - Reusable CodeMirror 5 HTML editor web component.
 *
 * Usage:
 *   <html-editor></html-editor>
 *
 * API:
 *   getValue(): string
 *   setValue(code: string): void
 *
 * Events:
 *   change - dispatched when content changes
 */

// Singleton promise so CodeMirror resources load only once
let cmReady: Promise<void> | null = null;

function loadCodeMirror(): Promise<void> {
  if (cmReady) return cmReady;
  cmReady = new Promise<void>((resolve, reject) => {
    if ((window as any).CodeMirror) { resolve(); return; }

    // Load CSS files
    const cssFiles = [
      'https://unpkg.com/codemirror@5.65.16/lib/codemirror.css',
      'https://unpkg.com/codemirror@5.65.16/theme/dracula.css',
    ];
    for (const href of cssFiles) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = href;
      document.head.appendChild(link);
    }

    // Load scripts sequentially
    const scripts = [
      'https://unpkg.com/codemirror@5.65.16/lib/codemirror.js',
      'https://unpkg.com/codemirror@5.65.16/mode/xml/xml.js',
      'https://unpkg.com/codemirror@5.65.16/mode/javascript/javascript.js',
      'https://unpkg.com/codemirror@5.65.16/mode/css/css.js',
      'https://unpkg.com/codemirror@5.65.16/mode/htmlmixed/htmlmixed.js',
    ];

    let idx = 0;
    function loadNext(): void {
      if (idx >= scripts.length) { resolve(); return; }
      const src = scripts[idx++];
      const script = document.createElement('script');
      script.src = src;
      script.onload = loadNext;
      script.onerror = () => { cmReady = null; reject(new Error(`Failed to load: ${src}`)); };
      document.head.appendChild(script);
    }
    loadNext();
  });
  return cmReady;
}

export class HtmlEditor extends HTMLElement {
  private editor: any = null;
  private pendingValue: string = '';
  private resizeObserver: ResizeObserver | null = null;

  connectedCallback(): void {
    this.style.display = 'block';
    this.style.border = '1px solid var(--border-color, #333)';
    this.style.borderRadius = '4px';
    this.style.overflow = 'hidden';
    this.style.minHeight = '120px';

    const textarea = document.createElement('textarea');
    textarea.value = this.pendingValue;
    this.appendChild(textarea);

    loadCodeMirror().then(() => {
      const CM = (window as any).CodeMirror;
      if (!CM || !this.isConnected) return;

      this.editor = CM.fromTextArea(textarea, {
        mode: 'htmlmixed',
        theme: 'dracula',
        lineNumbers: true,
        lineWrapping: true,
        tabSize: 2,
        indentWithTabs: false,
        autofocus: false,
      });

      // setValue() may have been called before CM finished loading - apply now
      if (this.pendingValue) {
        this.editor.setValue(this.pendingValue);
      }

      // Style the CM wrapper to fill available space
      const wrapper = this.editor.getWrapperElement() as HTMLElement;
      wrapper.style.height = '100%';
      wrapper.style.fontSize = '12px';
      wrapper.style.fontFamily = "'IBM Plex Mono', monospace";
      wrapper.style.lineHeight = '1.4';

      this.resizeObserver = new ResizeObserver(() => this.refresh());
      this.resizeObserver.observe(this);

      this.editor.on('change', () => {
        this.dispatchEvent(new CustomEvent('change', {
          bubbles: true,
          composed: true,
          detail: { value: this.editor.getValue() },
        }));
      });

      this.scheduleInitialRefresh();
    }).catch(err => {
      console.error('html-editor: failed to load CodeMirror', err);
    });
  }

  disconnectedCallback(): void {
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    if (this.editor) {
      try { this.editor.toTextArea(); } catch { /* ignore */ }
      this.editor = null;
    }
  }

  getValue(): string {
    return this.editor ? this.editor.getValue() : this.pendingValue;
  }

  setValue(code: string): void {
    this.pendingValue = code;
    if (this.editor) {
      const current = this.editor.getValue();
      if (current !== code) {
        this.editor.setValue(code);
        this.scheduleInitialRefresh();
      }
    }
  }

  insertText(text: string): void {
    if (this.editor) {
      this.editor.replaceSelection(text);
      this.pendingValue = this.editor.getValue();
      this.editor.focus();
      return;
    }
    this.pendingValue += text;
  }

  /** Refresh layout (call after becoming visible) */
  refresh(): void {
    if (!this.editor) return;
    requestAnimationFrame(() => {
      if (!this.editor || !this.isConnected) return;
      this.editor.refresh();
    });
  }

  private scheduleInitialRefresh(): void {
    const refreshNow = () => {
      if (!this.editor || !this.isConnected) return;
      this.editor.refresh();
    };

    requestAnimationFrame(() => {
      refreshNow();
      requestAnimationFrame(refreshNow);
    });
    setTimeout(refreshNow, 80);
    setTimeout(refreshNow, 240);
    document.fonts?.ready.then(refreshNow).catch(() => { /* ignore */ });
  }
}

if (!customElements.get('html-editor')) {
  customElements.define('html-editor', HtmlEditor);
}
