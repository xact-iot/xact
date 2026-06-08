import { afterEach, describe, expect, it, vi } from 'vitest';
import '../src/components/html-editor';
import '../src/components/icon-picker';

describe('html-editor', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    document.head.querySelectorAll('script,link').forEach(el => el.remove());
    vi.unstubAllGlobals();
  });

  it('applies pending values, emits changes, inserts text, refreshes, and disconnects', async () => {
    const refresh = vi.fn();
    const focus = vi.fn();
    const toTextArea = vi.fn();
    let currentValue = '';
    let onChange: (() => void) | null = null;
    const editor = {
      setValue: vi.fn((value: string) => { currentValue = value; }),
      getValue: vi.fn(() => currentValue),
      replaceSelection: vi.fn((text: string) => { currentValue += text; }),
      focus,
      refresh,
      toTextArea,
      getWrapperElement: vi.fn(() => document.createElement('div')),
      on: vi.fn((_event: string, cb: () => void) => { onChange = cb; }),
    };
    const fromTextArea = vi.fn(() => editor);
    vi.stubGlobal('CodeMirror', { fromTextArea });
    vi.stubGlobal('ResizeObserver', class {
      observe = vi.fn();
      disconnect = vi.fn();
    });
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });

    const el = document.createElement('html-editor') as any;
    el.setValue('<p>pending</p>');
    document.body.appendChild(el);
    await Promise.resolve();

    expect(fromTextArea).toHaveBeenCalled();
    expect(editor.setValue).toHaveBeenCalledWith('<p>pending</p>');

    const changes: string[] = [];
    el.addEventListener('change', (event: CustomEvent) => changes.push(event.detail.value));
    currentValue = '<p>changed</p>';
    onChange?.();
    expect(changes).toEqual(['<p>changed</p>']);

    el.insertText('<b>!</b>');
    expect(editor.replaceSelection).toHaveBeenCalledWith('<b>!</b>');
    expect(focus).toHaveBeenCalled();
    el.refresh();
    expect(refresh).toHaveBeenCalled();

    el.remove();
    expect(toTextArea).toHaveBeenCalled();
  });
});

describe('icon-picker', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    vi.unstubAllGlobals();
  });

  it('opens the modal, searches icons, selects a result, and closes on disconnect', async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        prefix: 'mdi',
        icons: {
          pump: { body: '<path d="M0 0h1v1z"/>' },
          valve: { body: '<path d="M0 0h2v2z"/>' },
        },
      }),
    } as Response));
    vi.stubGlobal('fetch', fetchMock);

    const picker = document.createElement('icon-picker') as any;
    const changes: string[] = [];
    picker.addEventListener('change', (event: CustomEvent) => changes.push(event.detail));
    document.body.appendChild(picker);
    await Promise.resolve();

    picker.querySelector<HTMLButtonElement>('.ip-choose-btn')?.click();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(document.querySelector('.ip-dialog')).toBeTruthy();

    const input = document.querySelector<HTMLInputElement>('.ip-search')!;
    input.value = 'pump';
    input.dispatchEvent(new Event('input'));
    vi.advanceTimersByTime(200);
    await Promise.resolve();
    await Promise.resolve();

    const iconButton = document.querySelector<HTMLButtonElement>('.ip-grid button')!;
    expect(iconButton.title).toBe('pump');
    iconButton.dispatchEvent(new Event('mouseenter'));
    iconButton.dispatchEvent(new Event('mouseleave'));
    iconButton.click();

    expect(picker.value).toBe('mdi:pump');
    expect(changes).toEqual(['mdi:pump']);
    expect(document.querySelector('.ip-dialog')).toBeNull();

    picker.querySelector<HTMLButtonElement>('.ip-choose-btn')?.click();
    expect(document.querySelector('.ip-dialog')).toBeTruthy();
    picker.remove();
    expect(document.querySelector('.ip-dialog')).toBeNull();
    vi.useRealTimers();
  });
});
