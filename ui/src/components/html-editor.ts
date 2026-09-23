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

import { loadCodeMirror } from '../utils/vendor-loaders';

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
