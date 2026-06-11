/**
 * REST API wrapper - handles all server communication
 * The server is the source of truth; changes propagate back via NATS
 */
import { getCurrentUser } from './auth';

const BASE_URL = '/xact'

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

// --- Health / Server Info ---

export interface HealthInfo {
  status: string;
  timezone?: string;
  service?: string;
  timestamp?: number;
  appVersion?: string;
  goVersion?: string;
}

export async function fetchHealth(): Promise<HealthInfo> {
  const r = await fetch(`${BASE_URL}/health`);
  if (!r.ok) throw new Error(`Health check failed: ${r.status}`);
  return r.json();
}

// --- NATS Config ---

export interface NATSConfig {
  username: string;
  password: string;
  natsWsPath: string;
  natsWsUrl?: string;
}

export async function fetchNATSConfig(): Promise<NATSConfig> {
  const r = await fetch(`${BASE_URL}/api/v1/system/nats-config`, {
    headers: getHeaders(),
  });
  if (!r.ok) throw new Error(`NATS config fetch failed: ${r.status}`);
  return r.json();
}

// Auth headers provider
let getAuthHeadersFn: (() => HeadersInit) | null = null;

export function setAuthHeadersProvider(fn: () => HeadersInit): void {
  getAuthHeadersFn = fn;
}

function getHeaders(): HeadersInit {
  return getAuthHeadersFn ? getAuthHeadersFn() : {};
}

// Convert dotted or slash paths to org-relative slash paths for REST API URLs.
function toSlashPath(path: string): string {
  const apiPath = toOrgRelativePath(path);
  if (!apiPath) return '/';
  return '/' + apiPath.split('/').map(encodeURIComponent).join('/');
}

function toOrgRelativePath(path: string): string {
  const org = getCurrentUser()?.tenant_id ?? '';
  let apiPath = String(path ?? '')
    .trim()
    .replace(/\./g, '/')
    .replace(/^\/+|\/+$/g, '');
  if (org && (apiPath === org || apiPath.startsWith(`${org}/`))) {
    apiPath = apiPath.slice(org.length).replace(/^\/+/, '');
  }
  return apiPath;
}

/**
 * Load node metadata from server
 * @param path Dotted path (e.g., 'building.floor1')
 * @param depth Optional: number of levels to fetch recursively (-1 for entire subtree)
 */
export async function loadNode(path: string, depth?: number): Promise<any> {
  let url = `${BASE_URL}/api/v1/nodes${toSlashPath(path)}`;
  if (depth !== undefined) {
    url += `?depth=${depth}`;
  }
  const response = await fetch(url, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to load node: ${response.status}`);
  }

  return response.json();
}

/**
 * Load tag metadata from server
 * @param path Dotted path (e.g., 'building.floor1.room101.temperature')
 */
export async function loadTag(path: string): Promise<any> {
  const response = await fetch(`${BASE_URL}/api/v1/tags${toSlashPath(path)}`, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to load tag: ${response.status}`);
  }

  return response.json();
}

export type NodeType = 'Standard' | 'Device' | 'Organisation';

// ScalarType enum matching server (tree.ScalarType)
export enum ScalarType {
  Integer = 0,
  Float = 1,
  String = 2,
  Boolean = 3,
  Enum = 4,
}

// Map type string to ScalarType enum
function parseScalarType(typeStr: string): ScalarType {
  switch (typeStr.toLowerCase()) {
    case 'integer': return ScalarType.Integer;
    case 'float': return ScalarType.Float;
    case 'string': return ScalarType.String;
    case 'boolean': return ScalarType.Boolean;
    case 'enum': return ScalarType.Enum;
    default: return ScalarType.String;
  }
}

/**
 * Create a new node on the server
 * @param path Dotted path (e.g., 'building.floor1')
 * @param nodeType Optional type - 'Standard' (default), 'Device', or 'Organisation'
 */
export async function createNode(path: string, nodeType?: NodeType, isArray?: boolean): Promise<void> {
  const payload: Record<string, any> = { path: toSlashPath(path), nodeType: nodeType ?? 'Standard' };
  if (isArray) payload.isArray = true;
  const response = await fetch(`${BASE_URL}/api/v1/nodes/`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(payload),
  });

  if (!response.ok) {
    throw new Error(`Failed to create node: ${response.status}`);
  }
}

/**
 * Create a new tag on the server
 * @param path Dotted path (e.g., 'building.floor1.room101.temperature')
 */
export async function createTag(path: string, type: string, metadata?: any): Promise<void> {
  const shared: Record<string, any> = {};
  if (metadata?.description) shared.description = metadata.description;
  if (metadata?.units) shared.units = metadata.units;
  if (metadata?.deadband !== undefined) shared.deadband = metadata.deadband;
  if (metadata?.enumValues && Object.keys(metadata.enumValues).length > 0) {
    shared.enumValues = metadata.enumValues;
  }

  const response = await fetch(`${BASE_URL}/api/v1/tags/`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({
      path: toSlashPath(path),
      type: parseScalarType(type),
      config: { type: parseScalarType(type) },
      shared,
    }),
  });

  if (!response.ok) {
    throw new Error(`Failed to create tag: ${response.status}`);
  }
}

/**
 * Delete a node from the server
 * @param path Dotted path (e.g., 'building.floor1')
 */
export async function deleteNode(path: string): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/nodes${toSlashPath(path)}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${response.status}`);
  }
}

/**
 * Delete a tag from the server
 * @param path Dotted path (e.g., 'building.floor1.room101.temperature')
 */
export async function deleteTag(path: string): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/tags${toSlashPath(path)}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${response.status}`);
  }
}

/**
 * Update a tag value on the server
 * @param path Dotted path (e.g., 'building.floor1.room101.temperature')
 */
export async function updateTagValue(path: string, value: any): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/tags${toSlashPath(path)}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify({ value }),
  });

  if (!response.ok) {
    throw new Error(`Failed to update tag: ${response.status}`);
  }
}

export interface PipelineBlockEnvelope {
  type: string;
  parameters: Record<string, any>;
}

export interface DebugStepResult {
  type: string;
  input: any;
  output: any;
  error?: string;
  stateChange?: string;
}

/**
 * Update a tag's description and/or pipeline
 * @param path Dotted path
 * @param data Tag fields to update; pipeline=[] clears it, pipeline=undefined leaves it unchanged
 */
export async function updateTag(path: string, data: {
  name?: string;
  description?: string;
  units?: string;
  deadband?: number;
  enumValues?: Record<string, string>;
  pipeline?: PipelineBlockEnvelope[];
}): Promise<any> {
  const response = await fetch(`${BASE_URL}/api/v1/tags${toSlashPath(path)}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });

  if (!response.ok) {
    throw new Error(`Failed to update tag: ${response.status}`);
  }

  return response.json();
}

/**
 * Run the tag's pipeline step-by-step on a test input value
 * @param path Dotted path
 * @param input Test input value
 */
export async function debugTagPipeline(path: string, input: any): Promise<{
  steps: DebugStepResult[];
  finalOutput: any;
  blockCount: number;
}> {
  const response = await fetch(`${BASE_URL}/api/v1/debug/tags${toSlashPath(path)}`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({ input }),
  });

  if (!response.ok) {
    throw new Error(`Failed to debug pipeline: ${response.status}`);
  }

  return response.json();
}

/**
 * Update a node's metadata on the server
 * @param path Dotted path (e.g., 'building.floor1')
 * @param data Node metadata to update
 */
export async function updateNode(path: string, data: { name?: string; description?: string; templateName?: string; isDevice?: boolean }): Promise<any> {
  const response = await fetch(`${BASE_URL}/api/v1/nodes${toSlashPath(path)}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });

  if (!response.ok) {
    throw new Error(`Failed to update node: ${response.status}`);
  }

  return response.json();
}

// --- Dashboard API ---

export interface DashboardMeta {
  id: number;
  name: string;
  description: string;
  icon: string;
  variation: string;
  deviceType: string;
  permission: string;
  isCategory: boolean;
  parentId?: number | null;
  sortOrder: number;
}

export interface Dashboard extends DashboardMeta {
  widgets: any[];
}

/**
 * List all dashboards (metadata only, no widgets).
 */
export async function listDashboards(): Promise<DashboardMeta[]> {
  const response = await fetch(`${BASE_URL}/api/v1/dashboards`, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new ApiError(`Failed to list dashboards: ${response.status}`, response.status);
  }

  return response.json();
}

/**
 * Get a single dashboard with full data including widgets.
 */
export async function getDashboard(id: number): Promise<Dashboard> {
  const response = await fetch(`${BASE_URL}/api/v1/dashboards/${id}`, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to get dashboard: ${response.status}`);
  }

  return response.json();
}

/**
 * Create a new dashboard.
 */
export async function createDashboard(dashboard: Omit<Dashboard, 'id'>): Promise<Dashboard> {
  const response = await fetch(`${BASE_URL}/api/v1/dashboards`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(dashboard),
  });

  if (!response.ok) {
    throw new Error(`Failed to create dashboard: ${response.status}`);
  }
  return response.json();
}

/**
 * Update an existing dashboard by id.
 */
export async function updateDashboard(id: number, dashboard: Partial<Dashboard>): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/dashboards/${id}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(dashboard),
  });

  if (!response.ok) {
    throw new Error(`Failed to update dashboard: ${response.status}`);
  }
}

/**
 * Delete a dashboard by id.
 */
export async function deleteDashboard(id: number): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/dashboards/${id}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to delete dashboard: ${response.status}`);
  }
}

// --- Permissions API ---

export interface RolePermissions {
  role: string;
  ui: Record<string, Record<string, boolean>>;
  server: Record<string, Record<string, boolean>>;
}

/**
 * Fetch the current user's merged UI permissions
 */
export async function fetchMyPermissions(): Promise<Record<string, Record<string, boolean>>> {
  const response = await fetch(`${BASE_URL}/api/v1/permissions`, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch permissions: ${response.status}`);
  }

  return response.json();
}

/**
 * Fetch all role permission records (admin widget)
 */
export async function fetchAllRolePermissions(): Promise<RolePermissions[]> {
  const response = await fetch(`${BASE_URL}/api/v1/permissions/roles`, {
    headers: getHeaders(),
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch role permissions: ${response.status}`);
  }

  return response.json();
}

/**
 * Update a role's permissions
 */
export async function updateRolePermissions(role: string, data: { ui: Record<string, Record<string, boolean>>; server: Record<string, Record<string, boolean>> }): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/permissions/roles/${encodeURIComponent(role)}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });

  if (!response.ok) {
    throw new Error(`Failed to update role permissions: ${response.status}`);
  }
}

// --- Users API ---

export interface UserOrg {
  orgId: number;
  orgName: string;
  roles: string[];
}

export interface NotificationOptions {
  emailEnabled: boolean;
  telegramEnabled: boolean;
  telegramId: string;
}

export interface UserRecord {
  id: number;
  firstName: string;
  lastName: string;
  loginName: string;
  email: string;
  notificationOptions?: NotificationOptions;
  active: boolean;
  lastLogin?: string;
  createdAt: string;
  orgs?: UserOrg[];
}

export interface Role {
  id: number;
  name: string;
  description: string;
}

export async function listUsers(): Promise<UserRecord[]> {
  const response = await fetch(`${BASE_URL}/api/v1/users`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to list users: ${response.status}`);
  return response.json();
}

export async function getUser(id: number): Promise<UserRecord> {
  const response = await fetch(`${BASE_URL}/api/v1/users/${id}`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to get user: ${response.status}`);
  return response.json();
}

export async function createUser(data: {
  firstName: string;
  lastName: string;
  loginName: string;
  email: string;
  password: string;
  roles: string[];
}): Promise<UserRecord> {
  const response = await fetch(`${BASE_URL}/api/v1/users`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Failed to create user: ${response.status}`);
  }
  return response.json();
}

export async function updateUser(id: number, data: {
  firstName?: string;
  lastName?: string;
  email?: string;
  active?: boolean;
  orgName?: string;
  roles?: string[];
  notificationOptions?: NotificationOptions;
}): Promise<UserRecord> {
  const response = await fetch(`${BASE_URL}/api/v1/users/${id}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!response.ok) throw new Error(`Failed to update user: ${response.status}`);
  return response.json();
}

export async function resetUserPassword(id: number): Promise<{ password: string }> {
  const response = await fetch(`${BASE_URL}/api/v1/users/${id}/reset-password`, {
    method: 'POST',
    headers: getHeaders(),
  });
  if (!response.ok) throw new Error(`Failed to reset password: ${response.status}`);
  return response.json();
}

export async function listRoles(): Promise<Role[]> {
  const response = await fetch(`${BASE_URL}/api/v1/roles`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to list roles: ${response.status}`);
  return response.json();
}

// --- My Profile API ---

export async function getMyProfile(): Promise<UserRecord> {
  const response = await fetch(`${BASE_URL}/api/v1/me/`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to get profile: ${response.status}`);
  return response.json();
}

export async function updateMyProfile(data: {
  firstName?: string;
  lastName?: string;
  email?: string;
  notificationOptions?: NotificationOptions;
}): Promise<UserRecord> {
  const response = await fetch(`${BASE_URL}/api/v1/me/`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Failed to update profile: ${response.status}`);
  }
  return response.json();
}

// --- Organisations API ---

export interface OrgArea {
  north: number;
  south: number;
  east: number;
  west: number;
}

export interface Organisation {
  id: number;
  /** Immutable slug - becomes the RTDB root node key. */
  name: string;
  /** Human-friendly label for display in reports and emails. */
  displayName: string;
  active: boolean;
  logo?: string;
  favicon?: string;
  area?: OrgArea;
}

export async function listOrganisations(): Promise<Organisation[]> {
  const response = await fetch(`${BASE_URL}/api/v1/organisations`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to list organisations: ${response.status}`);
  return response.json();
}

export async function getOrganisation(name: string): Promise<Organisation> {
  const response = await fetch(`${BASE_URL}/api/v1/organisations/${encodeURIComponent(name)}`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to get organisation: ${response.status}`);
  return response.json();
}

export async function createOrganisation(data: { name: string; displayName?: string; active: boolean; logo?: string; favicon?: string; area?: OrgArea | null }): Promise<Organisation> {
  const response = await fetch(`${BASE_URL}/api/v1/organisations`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || body || `Failed to create organisation: ${response.status}`);
  }
  return response.json();
}

export async function updateOrganisation(name: string, data: { displayName?: string; active: boolean; logo?: string; favicon?: string; area?: OrgArea | null }): Promise<Organisation> {
  const response = await fetch(`${BASE_URL}/api/v1/organisations/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || body || `Failed to update organisation: ${response.status}`);
  }
  return response.json();
}

export async function deleteOrganisation(name: string): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/organisations/${encodeURIComponent(name)}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });
  if (!response.ok) {
    const body = await response.text().catch(() => '');
    throw new Error(body || `Failed to delete organisation: ${response.status}`);
  }
}

// --- API Keys ---

export interface APIKey {
  id: number;
  orgName: string;
  name: string;
  key: string;
  createdAt: string;
}

export async function listAPIKeys(): Promise<APIKey[]> {
  const response = await fetch(`${BASE_URL}/api/v1/api-keys`, { headers: getHeaders() });
  if (!response.ok) throw new Error(`Failed to list API keys: ${response.status}`);
  return response.json();
}

export async function createAPIKey(name: string): Promise<APIKey> {
  const response = await fetch(`${BASE_URL}/api/v1/api-keys`, {
    method: 'POST',
    headers: { ...getHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!response.ok) {
    const body = await response.text().catch(() => '');
    throw new Error(body || `Failed to create API key: ${response.status}`);
  }
  return response.json();
}

export async function deleteAPIKey(id: number): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/api-keys/${id}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });
  if (!response.ok) {
    const body = await response.text().catch(() => '');
    throw new Error(body || `Failed to delete API key: ${response.status}`);
  }
}

// --- Block Schemas API ---

export interface BlockParamDef {
  type: 'number' | 'boolean' | 'string' | 'select' | 'notification-profile';
  label: string;
  options?: string[];
  required?: boolean;
  default?: any;
}

export interface BlockSchema {
  type: string;
  label: string;
  description: string;
  params: Record<string, BlockParamDef>;
}

export async function loadBlockSchemas(): Promise<BlockSchema[]> {
  const response = await fetch(`${BASE_URL}/api/v1/blocks/schemas`, {
    headers: getHeaders(),
  });
  if (!response.ok) throw new Error(`Failed to load block schemas: ${response.status}`);
  return response.json();
}

export async function changeMyPassword(oldPassword: string, newPassword: string): Promise<void> {
  const response = await fetch(`${BASE_URL}/api/v1/me/change-password`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({ oldPassword, newPassword }),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Failed to change password: ${response.status}`);
  }
}

// ─── PDF Report Templates ────────────────────────────────────────────────────

export interface PDFVariable {
  name: string;
  label: string;
  type: 'builtin' | 'rtdb' | 'sql' | 'custom';
  source?: string;        // for builtin: "now" | "org_name" | "org_slug" | "report_name"
  path?: string;          // for rtdb
  query?: string;         // for sql
  format?: string;        // for builtin "now"
  inputType?: string;     // for custom: "text" | "date" | "datetime" | "number"
  defaultValue?: string;  // for custom: fallback value when no runtime override supplied
}

export interface PDFTemplate {
  id: string;
  orgName: string;
  name: string;
  description: string;
  templateJson: any;
  variables: PDFVariable[];
  createdAt: string;
  updatedAt: string;
}

export async function listPDFTemplates(): Promise<PDFTemplate[]> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to list templates: ${r.status}`);
  return r.json();
}

export async function getPDFTemplate(id: string): Promise<PDFTemplate> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates/${id}`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to get template: ${r.status}`);
  return r.json();
}

export async function createPDFTemplate(t: Partial<PDFTemplate>): Promise<PDFTemplate> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(t),
  });
  if (!r.ok) throw new Error(`Failed to create template: ${r.status}`);
  return r.json();
}

export async function updatePDFTemplate(id: string, t: Partial<PDFTemplate>): Promise<PDFTemplate> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates/${id}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(t),
  });
  if (!r.ok) throw new Error(`Failed to update template: ${r.status}`);
  return r.json();
}

export async function deletePDFTemplate(id: string): Promise<void> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates/${id}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });
  if (!r.ok) throw new Error(`Failed to delete template: ${r.status}`);
}

export async function previewPDFTemplate(id: string, variables?: Record<string, string>): Promise<Blob> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/templates/${id}/preview`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({ variables: variables ?? {} }),
  });
  if (!r.ok) throw new Error(`Failed to generate preview: ${r.status}`);
  return r.blob();
}

export async function generatePDF(templateId: string, variables?: Record<string, string>): Promise<Blob> {
  const r = await fetch(`${BASE_URL}/api/v1/reports/generate`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({ templateId, variables: variables ?? {} }),
  });
  if (!r.ok) throw new Error(`Failed to generate PDF: ${r.status}`);
  return r.blob();
}

// --- Events API ---

export interface EventEntry {
  id: number;
  timestamp: string;
  server?: string;
  orgName?: string;
  userId?: number;
  userName?: string;
  severity: string;
  notificationId?: number;
  device: string;
  message: string;
  params?: Record<string, any>;
}

export interface EventFilter {
  severity?: string;
  device?: string;
  search?: string;
  orgName?: string;
  userId?: number;
  notificationId?: number;
  afterId?: number;
  startTime?: string;  // RFC3339
  endTime?: string;    // RFC3339
  limit?: number;
}

const inFlightEventQueries = new Map<string, Promise<EventEntry[]>>();

export async function queryEvents(filter: EventFilter = {}): Promise<EventEntry[]> {
  const params = new URLSearchParams();
  if (filter.severity)       params.set('severity',        filter.severity);
  if (filter.device)         params.set('device',          filter.device);
  if (filter.search)         params.set('search',          filter.search);
  if (filter.orgName)        params.set('org_name',        filter.orgName);
  if (filter.userId)         params.set('user_id',         String(filter.userId));
  if (filter.notificationId) params.set('notification_id', String(filter.notificationId));
  if (filter.afterId)        params.set('after_id',        String(filter.afterId));
  if (filter.startTime)      params.set('startTime',       filter.startTime);
  if (filter.endTime)        params.set('endTime',         filter.endTime);
  if (filter.limit)          params.set('limit',           String(filter.limit));
  const url = `${BASE_URL}/api/v1/logs?${params}`;
  const existing = inFlightEventQueries.get(url);
  if (existing) return existing;

  const request = (async () => {
    const r = await fetch(url, { headers: getHeaders() });
    if (!r.ok) throw new Error(`Failed to query events: ${r.status}`);
    return r.json();
  })();
  inFlightEventQueries.set(url, request);
  request.then(
    () => inFlightEventQueries.delete(url),
    () => inFlightEventQueries.delete(url),
  );
  return request;
}

export interface CreateEventLogEntry {
  severity?: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'CRITICAL';
  orgName?: string;
  notificationId?: number;
  device?: string;
  message: string;
  params?: Record<string, any>;
}

export async function createEventLogEntry(entry: CreateEventLogEntry): Promise<void> {
  const r = await fetch(`${BASE_URL}/api/v1/logs`, {
    method: 'POST',
    headers: { ...getHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(entry),
  });
  if (!r.ok) throw new Error(`Failed to create event: ${r.status}`);
}

// --- Notifications API ---

export interface NotificationProfile {
  id: number;
  orgName: string;
  name: string;
  description: string;
  roles: string[];
  users: number[];
  ackRequired: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ChannelConfig {
  email: {
    host: string;
    port: number;
    username: string;
    password: string;
    from: string;
    useTls: boolean;
  };
  telegram: {
    botToken: string;
  };
}

export async function listNotificationProfiles(): Promise<NotificationProfile[]> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/profiles`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to list notification profiles: ${r.status}`);
  return r.json();
}

export async function getNotificationProfile(id: number): Promise<NotificationProfile> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/profiles/${id}`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to get notification profile: ${r.status}`);
  return r.json();
}

export async function createNotificationProfile(data: Partial<NotificationProfile>): Promise<NotificationProfile> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/profiles`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!r.ok) throw new Error(`Failed to create notification profile: ${r.status}`);
  return r.json();
}

export async function updateNotificationProfile(id: number, data: Partial<NotificationProfile>): Promise<NotificationProfile> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/profiles/${id}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!r.ok) throw new Error(`Failed to update notification profile: ${r.status}`);
  return r.json();
}

export async function deleteNotificationProfile(id: number): Promise<void> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/profiles/${id}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });
  if (!r.ok) throw new Error(`Failed to delete notification profile: ${r.status}`);
}

export async function getChannelConfig(): Promise<ChannelConfig> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/channels`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to get channel config: ${r.status}`);
  return r.json();
}

export async function saveChannelConfig(config: ChannelConfig): Promise<ChannelConfig> {
  const r = await fetch(`${BASE_URL}/api/v1/notifications/channels`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(config),
  });
  if (!r.ok) throw new Error(`Failed to save channel config: ${r.status}`);
  return r.json();
}

export interface TagCalc {
  id: number;
  orgName: string;
  name: string;
  description: string;
  outputTag: string;
  expression: string;
  intervalSeconds: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export async function listTagCalcs(): Promise<TagCalc[]> {
  const r = await fetch(`${BASE_URL}/api/v1/tagcalcs/`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to list tag calcs: ${r.status}`);
  return r.json();
}

export async function createTagCalc(data: Partial<TagCalc>): Promise<TagCalc> {
  const r = await fetch(`${BASE_URL}/api/v1/tagcalcs/`, {
    method: 'POST', headers: getHeaders(), body: JSON.stringify(data),
  });
  if (!r.ok) {
    const msg = await r.text().catch(() => String(r.status));
    throw new Error(msg || `Failed to create tag calc: ${r.status}`);
  }
  return r.json();
}

export async function updateTagCalc(id: number, data: Partial<TagCalc>): Promise<TagCalc> {
  const r = await fetch(`${BASE_URL}/api/v1/tagcalcs/${id}`, {
    method: 'PUT', headers: getHeaders(), body: JSON.stringify(data),
  });
  if (!r.ok) {
    const msg = await r.text().catch(() => String(r.status));
    throw new Error(msg || `Failed to update tag calc: ${r.status}`);
  }
  return r.json();
}

export async function deleteTagCalc(id: number): Promise<void> {
  const r = await fetch(`${BASE_URL}/api/v1/tagcalcs/${id}`, {
    method: 'DELETE', headers: getHeaders(),
  });
  if (!r.ok) throw new Error(`Failed to delete tag calc: ${r.status}`);
}

export async function testTagCalc(expression: string): Promise<{ result?: number; error?: string }> {
  const r = await fetch(`${BASE_URL}/api/v1/tagcalcs/test`, {
    method: 'POST', headers: getHeaders(), body: JSON.stringify({ expression }),
  });
  return r.json();
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

export interface ScheduledTask {
  id: string;
  orgName: string;
  name: string;
  description: string;
  taskType: 'report' | 'backup' | 'shell' | 'yaegi' | 'command';
  taskConfig: Record<string, any>;
  schedule: string;
  enabled: boolean;
  lastRunAt?: string;
  lastRunStatus: string;
  lastRunMessage: string;
  createdAt: string;
  updatedAt: string;
}

export interface ScheduleRunLog {
  id: number;
  scheduleId: string;
  orgName: string;
  firedAt: string;
  completedAt?: string;
  status: string;
  message: string;
  outputPath: string;
}

export async function listScheduledTasks(): Promise<ScheduledTask[]> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to list scheduled tasks: ${r.status}`);
  return r.json();
}

export async function getScheduledTask(id: string): Promise<ScheduledTask> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules/${id}`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to get scheduled task: ${r.status}`);
  return r.json();
}

export async function createScheduledTask(data: Partial<ScheduledTask>): Promise<ScheduledTask> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules`, {
    method: 'POST', headers: getHeaders(), body: JSON.stringify(data),
  });
  if (!r.ok) {
    const msg = await r.text().catch(() => String(r.status));
    throw new Error(msg || `Failed to create scheduled task: ${r.status}`);
  }
  return r.json();
}

export async function updateScheduledTask(id: string, data: Partial<ScheduledTask>): Promise<ScheduledTask> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules/${id}`, {
    method: 'PUT', headers: getHeaders(), body: JSON.stringify(data),
  });
  if (!r.ok) {
    const msg = await r.text().catch(() => String(r.status));
    throw new Error(msg || `Failed to update scheduled task: ${r.status}`);
  }
  return r.json();
}

export async function deleteScheduledTask(id: string): Promise<void> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules/${id}`, {
    method: 'DELETE', headers: getHeaders(),
  });
  if (!r.ok) throw new Error(`Failed to delete scheduled task: ${r.status}`);
}

export async function runScheduledTaskNow(id: string): Promise<{ status?: string; outputPath?: string; error?: string }> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules/${id}/run`, {
    method: 'POST', headers: getHeaders(),
  });
  const body = await r.text().catch(() => '');
  let data: { status?: string; outputPath?: string; error?: string } = {};
  if (body.trim()) {
    try {
      data = JSON.parse(body);
    } catch {
      if (!r.ok) throw new Error(body.trim() || `Failed to run scheduled task: ${r.status}`);
      throw new Error(`Failed to parse run response: ${body.trim()}`);
    }
  }
  if (!r.ok) throw new Error(data.error || body.trim() || `Failed to run scheduled task: ${r.status}`);
  if (data.error) throw new Error(data.error);
  return data;
}

export async function getScheduleRunLog(id: string): Promise<ScheduleRunLog[]> {
  const r = await fetch(`${BASE_URL}/api/v1/schedules/${id}/history`, { headers: getHeaders() });
  if (!r.ok) throw new Error(`Failed to get run log: ${r.status}`);
  return r.json();
}
