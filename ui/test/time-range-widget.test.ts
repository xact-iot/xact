import { afterEach, describe, expect, it, vi } from 'vitest';
import { getUiStore } from '../src/store/ui-store';
import '../src/dashboards/widgets/time-range-widget';

describe('time-range-widget', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    getUiStore().set('timeStart', null);
    getUiStore().set('timeEnd', null);
  });

  it('renders visible datetime picker buttons for dark themes', () => {
    const widget = document.createElement('time-range-widget');
    document.body.appendChild(widget);

    expect(widget.querySelectorAll('input[type="datetime-local"]')).toHaveLength(2);
    expect(widget.querySelectorAll('.trw-picker-btn')).toHaveLength(2);
    expect(widget.querySelector('style')?.textContent).toContain('::-webkit-calendar-picker-indicator');
    expect(widget.querySelector('style')?.textContent).toContain('opacity: 0');
    expect(widget.querySelector('style')?.textContent).toContain('color: var(--accent-color)');
    expect(widget.querySelector('style')?.textContent).toContain('color-scheme: dark');
  });

  it('opens the native picker from the visible picker button when supported', () => {
    const widget = document.createElement('time-range-widget');
    document.body.appendChild(widget);
    const input = widget.querySelector<HTMLInputElement>('#trw-start') as HTMLInputElement & { showPicker?: () => void };
    input.showPicker = vi.fn();

    widget.querySelector<HTMLButtonElement>('.trw-picker-btn[data-target="trw-start"]')!.click();

    expect(input.showPicker).toHaveBeenCalled();
    expect(document.activeElement).toBe(input);
  });

  it('updates the UI store from datetime changes', () => {
    const widget = document.createElement('time-range-widget');
    document.body.appendChild(widget);

    const start = widget.querySelector<HTMLInputElement>('#trw-start')!;
    start.value = '2026-06-05T08:30';
    start.dispatchEvent(new Event('change'));

    expect(getUiStore().get('timeStart')).toBe(new Date('2026-06-05T08:30').getTime());
  });
});
