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
