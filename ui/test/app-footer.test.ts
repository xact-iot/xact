import { afterEach, describe, expect, it, vi } from 'vitest';
import '../src/components/app-footer';

describe('app-footer health status', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    vi.unstubAllGlobals();
  });

  it('shows connected in green when health succeeds', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ status: 'healthy', timezone: 'UTC', appVersion: '9.8.7-dev' }),
    } as Response)));

    const footer = document.createElement('app-footer');
    document.body.appendChild(footer);

    await vi.waitFor(() => {
      expect(footer.querySelector('#status-text')?.textContent).toBe('Connected');
    });
    expect(footer.querySelector('#app-version')?.textContent).toBe('2026 \u00a9 XACT v9.8.7-dev');
    expect(footer.querySelector('#app-version')?.textContent).not.toContain('v0.1.0');
    expect(footer.dataset.connectionState).toBe('online');
    expect(footer.style.backgroundColor).toBe('var(--footer-bg)');
    expect(footer.querySelector('#status-dot')?.className).toContain('bg-green-500');
  });

  it('shows a dramatic orange disconnected footer when health fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 503,
    } as Response)));

    const footer = document.createElement('app-footer');
    document.body.appendChild(footer);

    await vi.waitFor(() => {
      expect(footer.querySelector('#status-text')?.textContent).toBe('Server disconnected');
    });
    expect(footer.dataset.connectionState).toBe('offline');
    expect(footer.style.backgroundColor).toBe('rgb(249, 115, 22)');
    expect(footer.style.boxShadow).toContain('rgba(249, 115, 22, 0.45)');
    expect(footer.querySelector('#status-dot')?.className).toContain('bg-red-700');
    expect(footer.querySelector('#status-dot')?.className).toContain('animate-pulse');
  });

  it('opens an about modal with app, Go, and TypeScript versions', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        status: 'healthy',
        service: 'xact-rtdb-api',
        timestamp: 1780000000,
        timezone: 'UTC',
        appVersion: '0.1.0',
        goVersion: 'go1.25.0',
      }),
    } as Response)));

    const footer = document.createElement('app-footer');
    document.body.appendChild(footer);
    footer.querySelector<HTMLAnchorElement>('#about-link')?.click();

    await vi.waitFor(() => {
      expect(document.querySelector('app-dialog')?.textContent).toContain('About XACT');
    });
    const modalText = document.querySelector('app-dialog')?.textContent ?? '';
    expect(modalText).toContain('Application: XACT 0.1.0');
    expect(modalText).toContain('Go: go1.25.0');
    expect(modalText).toContain('TypeScript: 5.9.3');
  });
});
