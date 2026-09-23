/**
 * json-editor - Reusable CodeMirror 5 JSON editor web component.
 *
 * Usage:
 *   <json-editor></json-editor>
 *
 * API:
 *   getValue(): string
 *   setValue(code: string): void
 *   refresh(): void
 *
 * Events:
 *   change - dispatched when content changes, detail: { value: string }
 */

import { loadCodeMirror as loadCodeMirrorForJSON } from '../utils/vendor-loaders';

export class JsonEditor extends HTMLElement {
  private editor: any = null;
  private pendingValue: string = '';

  connectedCallback(): void {
    this.style.display = 'block';
    this.style.border = '1px solid var(--border-color, #333)';
    this.style.borderRadius = '4px';
    this.style.overflow = 'hidden';
    this.style.minHeight = '120px';

    const textarea = document.createElement('textarea');
    textarea.value = this.pendingValue;
    this.appendChild(textarea);

    loadCodeMirrorForJSON().then(() => {
      const CM = (window as any).CodeMirror;
      if (!CM || !this.isConnected) return;

      this.editor = CM.fromTextArea(textarea, {
        mode: { name: 'javascript', json: true },
        theme: 'dracula',
        lineNumbers: true,
        lineWrapping: true,
        tabSize: 2,
        indentWithTabs: false,
        autofocus: false,
        matchBrackets: true,
        autoCloseBrackets: true,
      });

      if (this.pendingValue) {
        this.editor.setValue(this.pendingValue);
      }

      const wrapper = this.editor.getWrapperElement() as HTMLElement;
      wrapper.style.height = '100%';
      wrapper.style.fontSize = '12px';
      wrapper.style.fontFamily = "'IBM Plex Mono', monospace";

      this.editor.on('change', () => {
        this.dispatchEvent(new CustomEvent('change', {
          bubbles: true,
          composed: true,
          detail: { value: this.editor.getValue() },
        }));
      });
    }).catch(err => {
      console.error('json-editor: failed to load CodeMirror', err);
    });
  }

  disconnectedCallback(): void {
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
      }
    }
  }

  /** Refresh layout (call after becoming visible) */
  refresh(): void {
    if (this.editor) {
      setTimeout(() => this.editor.refresh(), 0);
    }
  }
}

if (!customElements.get('json-editor')) {
  customElements.define('json-editor', JsonEditor);
}
