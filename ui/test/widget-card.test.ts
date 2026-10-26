import { describe, expect, it } from 'vitest';
import '../src/dashboards/widgets/widget-card';

describe('widget-card header visibility', () => {
  it('hides a blank header title in view mode', () => {
    const card = document.createElement('widget-card') as any;
    document.body.appendChild(card);

    card.setTitle('');

    const header = card.querySelector('.widget-header') as HTMLElement;
    expect(header.style.height).toBe('0px');

    card.remove();
  });

  it('shows the header for edit actions even when the title is blank', () => {
    const card = document.createElement('widget-card') as any;
    document.body.appendChild(card);

    card.setTitle('');
    card.setMode('edit');

    const header = card.querySelector('.widget-header') as HTMLElement;
    const title = card.querySelector('.wc-title') as HTMLElement;
    const actions = card.querySelector('.wc-actions') as HTMLElement;
    expect(header.style.height).toBe('');
    expect(title.style.display).toBe('none');
    expect(actions.style.display).toBe('flex');

    card.remove();
  });

  it('keeps edit actions in the header even for widgets that explicitly hide headers', () => {
    const card = document.createElement('widget-card') as any;
    document.body.appendChild(card);

    card.setTitle('Hidden in view');
    card.setHeaderVisible(false);
    card.setMode('edit');

    const header = card.querySelector('.widget-header') as HTMLElement;
    expect(header.style.height).toBe('');

    card.remove();
  });
});

describe('widget-card full screen', () => {
  const make = () => {
    const card = document.createElement('widget-card') as any;
    document.body.appendChild(card);
    const widget = document.createElement('div') as any;
    widget.calls = [] as boolean[];
    widget.setFullscreen = (v: boolean) => widget.calls.push(v);
    card.querySelector('.widget-body').appendChild(widget);
    return { card, widget, expand: () => card.querySelector('.wc-expand') as HTMLButtonElement };
  };

  it('shows the expand button only when allowed and not editing', () => {
    const { card, expand } = make();
    expect(expand().hidden).toBe(true);
    card.setFullscreenAllowed(true);
    expect(expand().hidden).toBe(false);
    card.setMode('inspect');
    expect(expand().hidden).toBe(false);
    card.setMode('edit');
    expect(expand().hidden).toBe(true);
    card.remove();
  });

  it('emits a toggle request from the edit-mode action and reflects the state', () => {
    const { card } = make();
    card.setWidgetId('w1');
    card.setMode('edit');
    const toggle = card.querySelector('.wc-fs-toggle') as HTMLElement;
    expect(toggle.style.display).toBe('inline-block');
    let detail: any = null;
    card.addEventListener('widget-fullscreen-allowed', (e: CustomEvent) => { detail = e.detail; });
    toggle.click();
    expect(detail).toEqual({ widgetId: 'w1', allowed: true });
    card.setFullscreenAllowed(true);
    expect(toggle.classList.contains('wc-on')).toBe(true);
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    card.setMode('view');
    expect(toggle.style.display).toBe('none');
    card.remove();
  });

  it('maximises in the page when the Fullscreen API is unavailable, and Esc restores it', async () => {
    const { card, widget, expand } = make();
    card.setFullscreenAllowed(true);
    expand().click();
    await Promise.resolve();
    expect(card.isFullscreen()).toBe(true);
    expect(card.classList.contains('wc-maximised')).toBe(true);
    expect(widget.calls).toEqual([true]);
    expect(expand().title).toContain('Exit full screen');
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    await Promise.resolve();
    expect(card.isFullscreen()).toBe(false);
    expect(card.classList.contains('wc-maximised')).toBe(false);
    expect(widget.calls).toEqual([true, false]);
    card.remove();
  });

  it('uses the Fullscreen API when available and follows the browser leaving it', async () => {
    const { card, widget, expand } = make();
    const inner = card.querySelector(':scope > .widget-card') as any;
    let fsEl: Element | null = null;
    Object.defineProperty(document, 'fullscreenEnabled', { configurable: true, get: () => true });
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => fsEl });
    inner.requestFullscreen = async () => { fsEl = inner; };
    card.setFullscreenAllowed(true);
    expand().click();
    await new Promise(r => setTimeout(r, 0));
    expect(card.isFullscreen()).toBe(true);
    expect(card.classList.contains('wc-maximised')).toBe(false);
    fsEl = null; // the user pressed Esc
    document.dispatchEvent(new Event('fullscreenchange'));
    expect(card.isFullscreen()).toBe(false);
    expect(widget.calls).toEqual([true, false]);
    delete (document as any).fullscreenEnabled;
    delete (document as any).fullscreenElement;
    card.remove();
  });

  it('leaves full screen when switched to edit mode or when no longer allowed', async () => {
    const { card, expand } = make();
    card.setFullscreenAllowed(true);
    expand().click();
    await Promise.resolve();
    card.setMode('edit');
    await Promise.resolve();
    expect(card.isFullscreen()).toBe(false);
    card.setMode('view');
    expand().click();
    await Promise.resolve();
    card.setFullscreenAllowed(false);
    await Promise.resolve();
    expect(card.isFullscreen()).toBe(false);
    card.remove();
  });
});
