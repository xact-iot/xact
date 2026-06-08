import { afterEach, describe, expect, it, vi } from 'vitest';
import { BaseComponent } from '../src/components/base-component';
import '../src/components/app-dialog';
import { showAlert, showChoice, showConfirm } from '../src/components/app-dialog';

class TestBaseComponent extends BaseComponent {
  renders = 0;
  attached = 0;
  detached = 0;
  config: any = null;

  setConfig(config: any) {
    this.config = config;
    this.render();
    this.attachEventListeners();
  }

  protected render(): void {
    this.renders++;
    this.innerHTML = '<button id="emit">emit</button>';
  }

  protected attachEventListeners(): void {
    this.attached++;
    this.querySelector('#emit')?.addEventListener('click', () => this.emit('test-event', { ok: true }));
  }

  protected detachEventListeners(): void {
    this.detached++;
  }
}

if (!customElements.get('x-test-base-component')) {
  customElements.define('x-test-base-component', TestBaseComponent);
}

describe('BaseComponent', () => {
  it('uses setConfig when valid JSON config is present and emits composed events', () => {
    const el = document.createElement('x-test-base-component') as TestBaseComponent;
    el.setAttribute('config', '{"name":"configured"}');
    const events: any[] = [];
    el.addEventListener('test-event', event => events.push((event as CustomEvent).detail));
    document.body.appendChild(el);

    expect(el.config).toEqual({ name: 'configured' });
    expect(el.renders).toBe(1);
    el.querySelector<HTMLButtonElement>('#emit')?.click();
    expect(events).toEqual([{ ok: true }]);

    el.remove();
    expect(el.detached).toBe(1);
  });

  it('falls back to render when config JSON is malformed', () => {
    const el = document.createElement('x-test-base-component') as TestBaseComponent;
    el.setAttribute('config', '{bad');
    document.body.appendChild(el);
    expect(el.config).toBeNull();
    expect(el.renders).toBe(1);
    expect(el.attached).toBe(1);
    el.remove();
  });
});

describe('AppDialog', () => {
  afterEach(() => {
    document.querySelectorAll('app-dialog').forEach(dialog => dialog.remove());
    vi.restoreAllMocks();
  });

  it('resolves alert, confirm, and choice dialogs from buttons and keyboard', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });

    const alert = showAlert('Hello <world>', { title: 'Notice', confirmLabel: 'Done' });
    expect(document.querySelector('app-dialog')?.textContent).toContain('Hello <world>');
    document.querySelector<HTMLButtonElement>('#dialog-confirm')?.click();
    await expect(alert).resolves.toBeUndefined();

    const confirm = showConfirm('Danger?', { tone: 'danger' });
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await expect(confirm).resolves.toBe(false);

    const choice = showChoice('Pick one', {
      choices: [
        { value: 'first', label: 'First' },
        { value: 'second', label: 'Second', role: 'primary' },
      ],
    });
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    await expect(choice).resolves.toBe('second');
  });

  it('resolves backdrop and explicit choice interactions', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });

    const confirm = showConfirm('Cancel?');
    document.querySelector<HTMLElement>('#dialog-backdrop')?.click();
    await expect(confirm).resolves.toBe(false);

    const choice = showChoice('Pick', {
      choices: [
        { value: 'first', label: 'First', role: 'danger' },
        { value: 'second', label: 'Second' },
      ],
    });
    document.querySelectorAll<HTMLButtonElement>('.dialog-choice')[1]?.click();
    await expect(choice).resolves.toBe('second');
  });
});
