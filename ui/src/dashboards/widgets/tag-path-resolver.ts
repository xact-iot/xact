import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';

function cleanPart(path: string): string {
  return String(path ?? '').trim().replace(/^\.+|\.+$/g, '');
}

function joinPath(prefix: string, suffix: string): string {
  const p = cleanPart(prefix);
  const s = cleanPart(suffix);
  if (!p) return s;
  if (!s) return p;
  return `${p}.${s}`;
}

function resolvePrefix(prefix: string): string {
  const cleanPrefix = cleanPart(prefix);
  if (!cleanPrefix.includes('*')) return cleanPrefix;

  const deviceName = cleanPart(getUiStore().get('deviceName') || '');
  if (deviceName) return cleanPrefix.replace(/\*/g, deviceName).replace(/\.+/g, '.').replace(/\.+$/g, '');

  const starIdx = cleanPrefix.indexOf('*');
  const parent = cleanPrefix.slice(0, starIdx).replace(/\.+$/g, '');
  const suffix = cleanPrefix.slice(starIdx + 1).replace(/^\.+|\.+$/g, '');
  if (!parent) return suffix;

  const store = getMirrorStore();
  const child = store.listChildrenNames(store.toAbsolute(parent))[0] ?? '';
  return joinPath(joinPath(parent, child), suffix);
}

export function resolveMetricTagPath(tagPrefix: string, tagPath: string): string {
  const cleanPath = cleanPart(tagPath);
  if (!cleanPath) return '';

  const store = getMirrorStore();
  const cleanPrefix = cleanPart(tagPrefix);
  if (!cleanPrefix) return store.toAbsolute(cleanPath);
  const resolvedPrefix = resolvePrefix(cleanPrefix);
  if (cleanPath === resolvedPrefix || cleanPath.startsWith(resolvedPrefix + '.')) {
    return store.toAbsolute(cleanPath);
  }
  return store.toAbsolute(joinPath(resolvedPrefix, cleanPath));
}
