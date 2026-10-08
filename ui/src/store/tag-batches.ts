export interface TagValueUpdate {
    type: string;
    value: any;
    status?: string;
    timestamp?: number;
}

export const TAG_BATCH_PREFIX = 'xact.internal.bcast.tagbatch.';

// Subscribe to the group that contains each requested leaf, or to the selected
// branch for descendant subscriptions. Remove overlap to avoid duplicate work.
export function tagBatchPatterns(paths: Iterable<string>): string[] {
    const groups = new Set<string>();
    for (const path of paths) {
        const dot = path.lastIndexOf('.');
        const group = path.endsWith('.>') ? path.slice(0, -2) : dot < 0 ? '' : path.slice(0, dot);
        if (group) groups.add(group);
    }
    const wildcards = [...groups].filter(group => group.includes('*')).map(group => group.split('.'));
    return [...groups].filter(group => {
        for (let parent = group.slice(0, group.lastIndexOf('.')); parent; parent = parent.slice(0, parent.lastIndexOf('.'))) {
            if (groups.has(parent)) return false;
            if (!parent.includes('.')) break;
        }
        const parts = group.split('.');
        return !wildcards.some(pattern => pattern.join('.') !== group && pattern.length <= parts.length &&
            pattern.every((part, index) => part === '*' || part === parts[index]));
    }).map(group => TAG_BATCH_PREFIX + group + '.>');
}

export function decodeTagBatch(subject: string, data: unknown, org: string): Array<[string, TagValueUpdate]> {
    if (!subject.startsWith(TAG_BATCH_PREFIX + org + '.') || !data || typeof data !== 'object') return [];
    const group = subject.slice(TAG_BATCH_PREFIX.length, subject.lastIndexOf('.'));
    const batch = data as { values?: Record<string, TagValueUpdate>; changed?: unknown };
    if (!batch.values || !Array.isArray(batch.changed)) return [];
    const changes = new Map<string, TagValueUpdate>();
    for (const path of batch.changed) {
        if (typeof path !== 'string' || !path.startsWith(group + '.') || path.slice(group.length + 1).includes('.')) return [];
        const value = batch.values[path];
        if (!value || typeof value !== 'object') return [];
        if (value.type === 'value') changes.set(path, value);
    }
    return [...changes];
}
