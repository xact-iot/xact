// Authentication module for XACT UI
const API_BASE_URL = '/xact/api';

let authToken: string | null = null;

export interface AuthResponse {
  token: string;
  token_type: string;
  expires_in: number;
  user: {
    id: string;
    username: string;
    tenant_id: string;
    roles: string[];
    allowed_orgs: string[];
  };
}

export interface BootstrapAdminStatus {
  setupRequired: boolean;
  passwordSet: boolean;
}

function storeAuthResponse(data: AuthResponse): AuthResponse {
  authToken = data.token;
  localStorage.setItem('xact_auth_token', authToken);
  localStorage.setItem('xact_auth_user', JSON.stringify(data.user));
  return data;
}

export async function login(username: string, password: string): Promise<AuthResponse> {
  const response = await fetch('/xact/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Login failed: ${response.status}`);
  }

  const data: AuthResponse = await response.json();
  return storeAuthResponse(data);
}

export async function getBootstrapAdminStatus(): Promise<BootstrapAdminStatus> {
  const response = await fetch(`${API_BASE_URL}/v1/bootstrap/admin`);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Bootstrap status failed: ${response.status}`);
  }
  return response.json();
}

export async function setBootstrapAdminPassword(password: string): Promise<AuthResponse> {
  const response = await fetch(`${API_BASE_URL}/v1/bootstrap/admin/password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Admin password setup failed: ${response.status}`);
  }

  const data: AuthResponse = await response.json();
  return storeAuthResponse(data);
}

export function logout(): void {
  authToken = null;
  localStorage.removeItem('xact_auth_token');
  localStorage.removeItem('xact_auth_user');
  clearPersistentNavigationState();
}

function clearPersistentNavigationState(): void {
  try {
    if (typeof history !== 'undefined' && typeof window !== 'undefined') {
      history.replaceState(null, '', '/xact/');
    }
  } catch {
    // Ignore navigation cleanup failures; auth state has already been cleared.
  }
}

export function getAuthToken(): string | null {
  if (!authToken) {
    authToken = localStorage.getItem('xact_auth_token');
  }
  return authToken;
}

export function getAuthHeaders(): Record<string, string> {
  const token = getAuthToken();
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

export function isAuthenticated(): boolean {
  const token = getAuthToken();
  return token !== null && !isTokenExpired(token);
}

export function getCurrentUser(): AuthResponse['user'] | null {
  const userStr = localStorage.getItem('xact_auth_user');
  if (!userStr) return null;
  try {
    return JSON.parse(userStr);
  } catch {
    return null;
  }
}

function isTokenExpired(token: string): boolean {
  try {
    const payload = JSON.parse(atob(token.split('.')[1]));
    if (payload.exp * 1000 < Date.now()) return true;
    // A token without an org context is unusable - treat it as expired so
    // the user is sent back to the login page to get a fresh one.
    if (!payload.tenant_id) return true;
    return false;
  } catch {
    return true;
  }
}

// Switches the stored active organisation token without navigating.
export async function switchOrgSession(orgName: string): Promise<AuthResponse> {
  const token = getAuthToken();
  if (!token) throw new Error('not authenticated');

  const response = await fetch(`${API_BASE_URL}/v1/auth/switch-org`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
    body: JSON.stringify({ org: orgName }),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || 'Failed to switch org');
  }

  const data: AuthResponse = await response.json();
  return storeAuthResponse(data);
}

// Switches the active organisation. Issues a new JWT and reloads to the home page.
export async function switchOrg(orgName: string): Promise<void> {
  await switchOrgSession(orgName);

  // Navigate to home and do a full refresh to clear any org-specific state.
  window.location.href = '/xact/?org=' + encodeURIComponent(orgName);
}

// Returns true if a valid token exists, false if the user needs to log in.
export function initializeAuth(): boolean {
  const existingToken = localStorage.getItem('xact_auth_token');
  if (existingToken && !isTokenExpired(existingToken)) {
    authToken = existingToken;
    return true;
  }
  // Clear expired token
  localStorage.removeItem('xact_auth_token');
  localStorage.removeItem('xact_auth_user');
  authToken = null;
  return false;
}
