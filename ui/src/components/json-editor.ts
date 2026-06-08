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

// Reuse the same CodeMirror loader as html-editor (js mode is already loaded)
let cmJsonReady: Promise<void> | null = null;

function loadCodeMirrorForJSON(): Promise<void> {
  if (cmJsonReady) return cmJsonReady;
  cmJsonReady = new Promise<void>((resolve, reject) => {
    // If CM is already available (html-editor loaded it), resolve immediately
    const checkReady = () => {
      const CM = (window as any).CodeMirror;
      if (CM && CM.modes && CM.modes['javascript']) {
        resolve();
        return;
      }
      // Need to load
      const cssFiles = [
        'https://unpkg.com/codemirror@5.65.16/lib/codemirror.css',
        'https://unpkg.com/codemirror@5.65.16/theme/dracula.css',
      ];
      for (const href of cssFiles) {
        if (!document.querySelector(`link[href="${href}"]`)) {
          const link = document.createElement('link');
          link.rel = 'stylesheet';
          link.href = href;
          document.head.appendChild(link);
        }
      }

      const scripts = [
        'https://unpkg.com/codemirror@5.65.16/lib/codemirror.js',
        'https://unpkg.com/codemirror@5.65.16/mode/javascript/javascript.js',
      ];

      let idx = 0;
      function loadNext(): void {
        if (idx >= scripts.length) { resolve(); return; }
        const src = scripts[idx++];
        if (document.querySelector(`script[src="${src}"]`)) {
          loadNext(); // already loaded
          return;
        }
        const script = document.createElement('script');
        script.src = src;
        script.onload = loadNext;
        script.onerror = () => { cmJsonReady = null; reject(new Error(`Failed to load: ${src}`)); };
        document.head.appendChild(script);
      }
      loadNext();
    };

    if ((window as any).CodeMirror) {
      checkReady();
    } else {
      checkReady();
    }
  });
  return cmJsonReady;
}

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
