import { getAuthHeaders, getAuthToken, getCurrentUser } from '../auth';
import { getMirrorStore } from '../store/store';
import { registerPermissions } from '../permissions/registry';
export interface ApplicationManifest { id: string; name: string; widgets: string[]; operations: string[]; resource: { id: string; description: string; permissions: Array<{name: string; description: string}> } }
const manifests = new Map<string,ApplicationManifest>();
const token = /^[A-Za-z0-9_-]{1,128}$/;
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
