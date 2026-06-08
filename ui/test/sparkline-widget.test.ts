import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../src/dashboards/widgets/sparkline-widget';

describe('sparkline-widget', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    document.querySelectorAll('sparkline-widget').forEach(el => el.remove());
  });

  it('aborts an in-flight metrics request when disconnected', async () => {
    let signal: AbortSignal | undefined;
    vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => {
      signal = init?.signal as AbortSignal | undefined;
      return new Promise<Response>(() => {});
    }));

    const widget = document.createElement('sparkline-widget');
    widget.setAttribute('device', 'LA_LongBeach.AirQuality.AQ-S-0150');
    widget.setAttribute('metric', 'gas.so2');
    document.body.appendChild(widget);

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(signal?.aborted).toBe(false);

    widget.remove();

    expect(signal?.aborted).toBe(true);
  });

  it('does not stack another metrics request while one is already pending', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})));

    const widget = document.createElement('sparkline-widget') as any;
    widget.setAttribute('device', 'LA_LongBeach.AirQuality.AQ-S-0150');
    widget.setAttribute('metric', 'gas.so2');
    widget.setAttribute('refresh-interval', '1');
    document.body.appendChild(widget);

    expect(fetch).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(3_000);

    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
