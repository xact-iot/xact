import { fetchMyPermissions } from '../api';
import { getCurrentUser } from '../auth';

type PermissionMap = Record<string, Record<string, boolean>>;

let permissions: PermissionMap = {};
let loaded = false;
let systemAdmin = false;

export async function loadPermissions(): Promise<void> {
  const user = getCurrentUser();
  systemAdmin = user?.roles?.includes('SystemAdmin') ?? false;

  if (systemAdmin) {
    // SystemAdmin has unrestricted access - no need to fetch permissions
    loaded = true;
    return;
  }

  try {
    permissions = await fetchMyPermissions();
    loaded = true;
  } catch (err) {
    console.error('Failed to load permissions:', err);
    permissions = {};
    loaded = true;
  }
}

export async function can(key: string): Promise<boolean> {
  // SystemAdmin has unrestricted access to everything
  if (systemAdmin) return true;

  if (!loaded) {
    const deadline = Date.now() + 10_000;
    while (!loaded && Date.now() < deadline) {
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    if (!loaded) return false;
  }
  // Re-check after waiting - loadPermissions may have set systemAdmin while we waited
  if (systemAdmin) return true;
  const [resource, action] = key.split('.');
  return permissions[resource]?.[action] === true;
}

export function getPermissions(): PermissionMap {
  return permissions;
}
