export interface PermissionDef {
  name: string;
  description: string;
}

export interface ResourcePermissions {
  resource: string;
  description: string;
  tooltip: string;
  permissions: PermissionDef[];
}

const registry = new Map<string, ResourcePermissions>();

export function registerPermissions(resource: string, description: string, permissions: PermissionDef[], tooltip = ''): void {
  registry.set(resource, { resource, description, tooltip, permissions });
}

export function getRegistry(): ResourcePermissions[] {
  return Array.from(registry.values());
}
