export abstract class BaseComponent extends HTMLElement {
  constructor() {
    super();
  }

  connectedCallback() {
    if (this.hasAttribute('config') && typeof (this as any).setConfig === 'function') {
      try {
        (this as any).setConfig(JSON.parse(this.getAttribute('config')!));
        return; // setConfig calls rerender() = render + attachEventListeners
      } catch { /* malformed JSON - fall through */ }
    }
    this.render();
    this.attachEventListeners();
  }

  disconnectedCallback() {
    this.detachEventListeners();
  }

  protected abstract render(): void;
  protected abstract attachEventListeners(): void;
  protected abstract detachEventListeners(): void;

  protected emit(eventName: string, detail?: any): void {
    this.dispatchEvent(new CustomEvent(eventName, {
      bubbles: true,
      composed: true,
      detail
    }));
  }
}
