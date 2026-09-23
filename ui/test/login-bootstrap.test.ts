import { afterEach, describe, expect, it, vi } from 'vitest';
import { getBootstrapAdminStatus, setBootstrapAdminPassword } from '../src/auth';
import '../src/components/login-page';

vi.mock('../src/auth', () => ({
  getBootstrapAdminStatus: vi.fn(),
  setBootstrapAdminPassword: vi.fn(),
  login: vi.fn(),
}));

afterEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

describe('operator-authorized bootstrap', () => {
  it('requires and sends the setup token with the initial password', async () => {
    vi.mocked(getBootstrapAdminStatus).mockResolvedValue({ setupRequired: true, setupEnabled: true, passwordSet: false });
    const page = document.createElement('login-page');
    document.body.append(page);
    await vi.waitFor(() => expect(page.shadowRoot?.querySelector('#setup-token')).toBeTruthy());
    const root = page.shadowRoot!;
    const input = (id: string) => root.querySelector<HTMLInputElement>(`#${id}`)!;
    input('password').value = 'new-admin-password';
    input('confirm-password').value = 'new-admin-password';
    root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }));
    expect(setBootstrapAdminPassword).not.toHaveBeenCalled();
    input('setup-token').value = 'operator-secret';
    root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }));
    expect(setBootstrapAdminPassword).toHaveBeenCalledWith('new-admin-password', 'operator-secret');
  });

  it('explains when the operator has not enabled initial setup', async () => {
    vi.mocked(getBootstrapAdminStatus).mockResolvedValue({ setupRequired: true, setupEnabled: false, passwordSet: false });
    const page = document.createElement('login-page');
    document.body.append(page);
    await vi.waitFor(() => expect(page.shadowRoot?.textContent).toContain('Initial admin setup must be enabled'));
    expect(page.shadowRoot?.querySelector('form')).toBeNull();
  });
});
