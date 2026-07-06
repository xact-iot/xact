import { afterEach, describe, expect, it, vi } from 'vitest';
import { getUiStore } from '../src/store/ui-store';
import '../src/dashboards/widgets/previous-period-widget';

describe('previous-period-widget', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    getUiStore().set('timeStart', null);
    getUiStore().set('timeEnd', null);
    vi.useRealTimers();
  });

  it('sets an open-ended rolling UI time range', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-06T12:00:00Z'));

    const widget = document.createElement('previous-period-widget');
    document.body.appendChild(widget);

    widget.querySelector<HTMLButtonElement>('button[data-ms="3600000"]')!.click();

    expect(getUiStore().get('timeStart')).toBe(new Date('2026-07-06T11:00:00Z').getTime());
    expect(getUiStore().get('timeEnd')).toBeNull();

    vi.advanceTimersByTime(30_000);

    expect(getUiStore().get('timeStart')).toBe(new Date('2026-07-06T11:00:30Z').getTime());
    expect(getUiStore().get('timeEnd')).toBeNull();
  });

  it('converts a restored fixed previous-period range into a rolling range', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-06T12:00:00Z'));
    getUiStore().set('timeStart', new Date('2026-07-05T09:30:00Z').getTime());
    getUiStore().set('timeEnd', new Date('2026-07-06T09:30:00Z').getTime());

    const widget = document.createElement('previous-period-widget');
    document.body.appendChild(widget);

    expect(getUiStore().get('timeStart')).toBe(new Date('2026-07-05T12:00:00Z').getTime());
    expect(getUiStore().get('timeEnd')).toBeNull();

    vi.advanceTimersByTime(30_000);

    expect(getUiStore().get('timeStart')).toBe(new Date('2026-07-05T12:00:30Z').getTime());
    expect(getUiStore().get('timeEnd')).toBeNull();
  });
});
