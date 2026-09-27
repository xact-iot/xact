import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppHeader } from '../src/components/app-header';
import { initializeAuth, logout } from '../src/auth';

describe('app-header menu', () => {
  const useAndroidClient = () => vi
    .spyOn(navigator, 'userAgent', 'get')
    .mockReturnValue('Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36');

  afterEach(() => {
    document.body.innerHTML = '';
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('offers Login and hides account actions when signed out', () => {
    const header = new AppHeader();
    document.body.appendChild(header);
    const listener = vi.fn();
    header.addEventListener('user-action', listener);

    expect(header.querySelector('#user-btn')?.textContent).toContain('Not signed in');
    expect(header.querySelector('[data-action="login"]')).not.toBeNull();
    expect(header.querySelector('[data-action="profile"]')).toBeNull();
    expect(header.querySelector('[data-action="preferences"]')).toBeNull();
    expect(header.querySelector('[data-action="logout"]')).toBeNull();

    header.querySelector<HTMLElement>('[data-action="login"]')?.click();
    expect(listener).toHaveBeenCalledOnce();
    expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ action: 'login' });
  });

  it('shows account actions after sign-in and restores Login after logout', () => {
    const payload = btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 3600, tenant_id: 'default' }));
    localStorage.setItem('xact_auth_token', `header.${payload}.signature`);
    localStorage.setItem('xact_auth_user', JSON.stringify({ username: 'james', tenant_id: 'default' }));
    expect(initializeAuth()).toBe(true);

    const header = new AppHeader();
    document.body.appendChild(header);
    expect(header.querySelector('[data-action="login"]')).toBeNull();
    expect(header.querySelector('[data-action="profile"]')).not.toBeNull();
    expect(header.querySelector('[data-action="logout"]')).not.toBeNull();

    logout();
    header.clearUser();
    expect(header.querySelector('[data-action="login"]')).not.toBeNull();
    expect(header.querySelector('[data-action="logout"]')).toBeNull();
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
