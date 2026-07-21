import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppHeader } from '../src/components/app-header';

describe('app-header menu', () => {
  const useAndroidClient = () => vi
    .spyOn(navigator, 'userAgent', 'get')
    .mockReturnValue('Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36');

  afterEach(() => {
    document.body.innerHTML = '';
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('places the Android download below a separator after Edit Dashboard', () => {
    useAndroidClient();
    const header = new AppHeader();
    document.body.appendChild(header);
    header.setIsOnDashboard(true);
    header.setDashboardCapabilities({ canEdit: true, canInspect: true });

    const edit = header.querySelector<HTMLElement>('[data-action="toggle-edit"]');
    const separator = edit?.nextElementSibling;
    const download = separator?.nextElementSibling as HTMLElement | null;

    expect(edit?.textContent).toContain('Edit Dashboard');
    expect(separator?.classList.contains('menu-separator')).toBe(true);
    expect(download?.dataset.action).toBe('download-android-app');
    expect(download?.textContent).toContain('Download Android App');
  });

  it('emits the Android download action', () => {
    useAndroidClient();
    const header = new AppHeader();
    document.body.appendChild(header);
    const listener = vi.fn();
    header.addEventListener('dashboard-action', listener);

    header.querySelector<HTMLElement>('[data-action="download-android-app"]')?.click();

    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({
      action: 'download-android-app',
    });
  });

  it('hides the Android download and separator from non-Android clients', () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36',
    );
    const header = new AppHeader();
    document.body.appendChild(header);
    header.setIsOnDashboard(true);
    header.setDashboardCapabilities({ canEdit: true, canInspect: true });

    expect(header.querySelector('[data-action="download-android-app"]')).toBeNull();
    expect(header.querySelector('.menu-separator')).toBeNull();
  });
});
