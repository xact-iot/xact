import { getAuthHeaders, getAuthToken, getCurrentUser } from '../auth';
import { getMirrorStore } from '../store/store';
import { registerPermissions } from '../permissions/registry';
export interface ApplicationHTTP { prefix: string; max_body_mb?: number; websocket_paths?: string[] }
export interface ApplicationManifest { id: string; name: string; widgets: string[]; operations: string[]; resource: { id: string; description: string; permissions: Array<{name: string; description: string}> }; http?: ApplicationHTTP }
const manifests = new Map<string,ApplicationManifest>();
const token = /^[A-Za-z0-9_-]{1,128}$/;
const BASE_URL = '/xact';
// Resolves a path below the application's proxy prefix; the result must stay inside it.
function proxyLocation(applicationId: string, path: string): {url: URL; websocket: boolean} {
 const prefix=manifests.get(applicationId)?.http?.prefix;
 if(!prefix) throw new Error('Application has no HTTP proxy');
 if(typeof path!=='string'||!path.startsWith('/')) throw new Error('Application path must start with /');
 const root=`${BASE_URL}/${prefix}`, url=new URL(root+path,window.location.origin);
 if(url.origin!==window.location.origin||(url.pathname!==root&&!url.pathname.startsWith(root+'/'))) throw new Error('Application path leaves the application prefix');
 const relative=url.pathname.slice(root.length);
 return {url,websocket:!!manifests.get(applicationId)?.http?.websocket_paths?.includes(relative)};
}
export async function loadApplications(): Promise<void> {
 manifests.clear();
 const r=await fetch('/xact/api/v1/applications',{headers:getAuthHeaders(),cache:'no-store'});
 if(!r.ok) throw new Error('Application registration unavailable');
 const list: ApplicationManifest[]=await r.json();
 for(const m of list) { manifests.set(m.id,m); registerPermissions(m.resource.id,m.resource.description,m.resource.permissions); }
}
export const applications = {
 list(): ApplicationManifest[] { return Array.from(manifests.values()); },
 async session(applicationId: string): Promise<any> {
  if(!manifests.has(applicationId)) throw new Error('Application is not registered');
  const r=await fetch(`/xact/api/v1/applications/${encodeURIComponent(applicationId)}/session`,{headers:getAuthHeaders(),cache:'no-store'});
  if(!r.ok) throw new Error(r.status===401?'Session expired':'Application authorization unavailable');
  return r.json();
 },
 async upload(applicationId: string, file: File, options: {scope?: string}={}): Promise<{archive: string; filename: string}> {
  const scope=options.scope||'default';
  if(applicationId!=='public_bus'||!manifests.has(applicationId)||!token.test(scope)||!file.name.toLowerCase().endsWith('.zip')) throw new Error('Choose a GTFS ZIP file');
  if(file.size>25*1024*1024) throw new Error('GTFS ZIP exceeds 25 MB');
  const params=new URLSearchParams({scope,filename:file.name});
  const r=await fetch(`/xact/api/v1/applications/public_bus/upload?${params}`,{method:'POST',headers:{...getAuthHeaders(),'Content-Type':'application/zip'},body:file,cache:'no-store'});
  const response=await r.json().catch(()=>({}));
  if(!r.ok) throw new Error(response?.error?.message||`GTFS upload failed (${r.status})`);
  if(typeof response.archive!=='string') throw new Error('Invalid GTFS upload response');
  return response;
 },
 /** Absolute URL for a proxied path; ws(s) for the application's WebSocket paths, http(s) otherwise. */
 proxyUrl(applicationId: string, path: string): string {
  const {url,websocket}=proxyLocation(applicationId,path);
  if(websocket) url.protocol=url.protocol==='https:'?'wss:':'ws:';
  return url.href;
 },
 /** fetch() through the application proxy with the current XACT session. Resolves with the Response, as fetch does. */
 async fetch(applicationId: string, path: string, init: RequestInit={}): Promise<Response> {
  const {url}=proxyLocation(applicationId,path), bearer=getAuthToken();
  if(!bearer) throw new Error('Session expired');
  const headers=new Headers(init.headers);
  headers.set('Authorization',`Bearer ${bearer}`);
  return globalThis.fetch(url.href,{cache:'no-store',...init,headers,credentials:'omit'});
 },
 async request(applicationId: string, operation: string, payload: unknown={}, options: {scope?: string; requestId?: string; revision?: number; timeoutMs?: number}={}): Promise<any> {
  const m=manifests.get(applicationId), bearer=getAuthToken(), user=getCurrentUser();
  const scope=options.scope||'default';
  if(!m||!m.operations.includes(operation)||!bearer||!user||!token.test(user.tenant_id)||!token.test(scope)) throw new Error('Invalid application request or session');
  const subject=`xact.app.v1.${user.tenant_id}.${applicationId}.${scope}.request.${operation}`;
  const result=await getMirrorStore().request(subject,{version:1,request_id:options.requestId||crypto.randomUUID(),session:bearer,revision:options.revision,payload},Math.min(options.timeoutMs||10000,30000));
  if(getAuthToken()!==bearer||getCurrentUser()?.tenant_id!==user.tenant_id) throw new Error('Session changed while request was in flight');
  if(!result?.ok) throw new Error(result?.error?.message||'Application request failed');
  return result;
 }
};
