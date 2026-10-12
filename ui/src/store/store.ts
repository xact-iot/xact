import *  as nats from "@nats-io/nats-core";
import { loadNode, loadTag } from '../api';
import { getCurrentUser } from '../auth';
import { decodeTagBatch, tagBatchPatterns, TAG_BATCH_PREFIX, type TagValueUpdate } from './tag-batches';


/**
 * -----------------------------
 * Types
 * -----------------------------
 */

export type Path = string;

type subscribeCallback = <T>(arg: T) => void;

export interface TagReference {
    path: string;
    selector: string;
}

const STATUS_SELECTORS = new Set(['U', 'S', 'A', 'D', 'N']);

function enumDisplayValue(value: any, shared: any): any {
    const enumValues = shared?.enumValues;
    if (!enumValues || value === undefined || value === null) return value;

    const display = enumValues[String(value)];
    return display !== undefined ? display : value;
}

function sharedWithDescription(data: any): any | null {
    const hasShared = data && Object.prototype.hasOwnProperty.call(data, 'shared');
    const shared = hasShared && data.shared ? { ...data.shared } : {};
    if (typeof data?.description === 'string') {
        shared.description = data.description;
    }
    return hasShared || Object.keys(shared).length > 0 ? shared : null;
}

type PublicTagValue = { value: any; units?: string; description?: string; status?: string; timestamp?: number };

function publicValuesEqual(a: any, b: any): boolean {
    if (Object.is(a, b)) return true;
    if (Array.isArray(a) && Array.isArray(b)) {
        return a.length === b.length && a.every((value, index) => publicValuesEqual(value, b[index]));
    }
    return a !== null && b !== null && typeof a === 'object' && typeof b === 'object' &&
        JSON.stringify(a) === JSON.stringify(b);
}

class Node {
    protected name: string;
    protected parent: Node | null;
    private children: Map<string, Node> = new Map;
    private subscribers: subscribeCallback[] = [];
    private value: any = undefined;
    private status: string = '';
    private timestamp: number = 0;
    private config: any = {};
    private shared: any = {};
    private nodeType: 'node' | 'leaf' | 'unknown' = 'unknown';
    private isArray: boolean = false;

    constructor(name: string, parent: Node | null) {
        this.name = name;
        this.parent = parent;
    }

    addChild(child: Node) {
        child.parent = this;
        this.children.set(child.name, child);
    }

    removeChild(name: string) {
        this.children.delete(name);
    }

    getOrCreateChild(name: string): Node {
        let child = this.children.get(name);
        if (!child) {
            child = new Node(name, this);
            this.children.set(name, child);
        }
        return child;
    }

    subscribe(cb: subscribeCallback): () => void {
        this.subscribers.push(cb);
        if (this.value !== undefined) {
            cb(this.getValue());
        }
        return () => {
            this.subscribers = this.subscribers.filter(subscriber => subscriber !== cb);
        };
    }

    setValue(value: any) {
        this.value = value;
        this.notifySubscribers();
    }

    getRawValue(): any {
        return this.value;
    }

    getValue(): any {
        return enumDisplayValue(this.value, this.shared);
    }

    private notifySubscribers() {
        for (const callback of this.subscribers) {
            callback(this.getValue());
        }
    }

    getChildren(): Map<string, Node> {
        return this.children;
    }

    getChildrenNames(): string[] {
        return Array.from(this.children.keys());
    }

    setStatus(status: string) {
        this.status = status;
        this.notifySubscribers();
    }

    getStatus(): string {
        return this.status;
    }

    setTimestamp(ts: number) {
        this.timestamp = ts;
    }

    getTimestamp(): number {
        return this.timestamp;
    }

    setConfig(config: any) {
        if (config) {
            this.config = config;
            this.notifySubscribers();
        }
    }

    getConfig(): any {
        return this.config;
    }

    setShared(shared: any) {
        if (shared) {
            this.shared = shared;
            this.notifySubscribers();
        }
    }

    getShared(): any {
        return this.shared;
    }

    setNodeType(type: 'node' | 'leaf') {
        this.nodeType = type;
    }

    getNodeType(): 'node' | 'leaf' | 'unknown' {
        return this.nodeType;
    }

    getName(): string {
        return this.name;
    }

    setIsArray(v: boolean) {
        this.isArray = v;
    }

    getIsArray(): boolean {
        return this.isArray;
    }

};

// class IntLeaf extends Node {
//     private value: int;

//     get value: int {
//         return this.value;
//     }
// }

// Callback type for tree structure changes
type TreeChangeCallback = (path: string, eventData: any) => void;
type TagValueChangeCallback = (path: string) => void;
export type NatsConnectionState = 'unknown' | 'connecting' | 'connected' | 'disconnected';
type NatsConnectionStateCallback = (state: NatsConnectionState) => void;

export class MirrorStore {
    private nc: nats.NatsConnection | null = null;
    private root: Node | null = null;
    private desiredTagValuePaths: Set<string> = new Set();
    private tagValueReferenceCounts = new Map<string, number>();
    private descendantValueReferences = new Map<string, number>();
    private selectedValueReferences = new Map<string, number>();
    private loadedChildrenPaths = new Set<string>();
    private treeLoads = new Map<string, Promise<void>>();
    public searchTruncated = false;
    private tagValuePrefixSubscriptions: Map<string, Set<TagValueChangeCallback>> = new Map();
    private watchedTagValuePaths: Map<string, nats.Subscription> = new Map();
    private watchedTagBatchPaths: Map<string, nats.Subscription> = new Map();
    private tagValueSyncQueued = false;
    private pendingValuePaths = new Set<string>();
    private rebuildValueSubscriptions = true;
    private valueCoverage: string[][] = [];
    private hydratedTagValuePaths: Set<string> = new Set();
    private unwatchedValueTimes = new Map<string, number>();
    private tagMetadataLoads: Map<Path, Promise<boolean>> = new Map();
    private tagMetadataQueue: Array<() => void> = [];
    private activeTagMetadataLoads = 0;
    private readonly maxTagMetadataLoads = 6;
    private treeSubscriptions: Map<string, Set<TreeChangeCallback>> = new Map();
    private treeSubscriptionsActive: boolean = false;
    private treeSubscription: nats.Subscription | null = null;
    private orgName: string = '';
    private publicSnapshotMode = false;
    private publicSnapshotValues = new Map<string, PublicTagValue>();
    private natsConnectionState: NatsConnectionState = 'unknown';
    private natsConnectionStateSubscribers: Set<NatsConnectionStateCallback> = new Set();


    constructor() {
        this.root = new Node('root', null);
    }

    // Connect to NAT and get the subtree below a root node.
    public async storeConnectNats(url: string, username?: string, password?: string, inboxPrefix?: string): Promise<void> {
        this.publicSnapshotMode = false;
        this.setNatsConnectionState('connecting');
        try {
            // Determine the current org from the JWT. Fall back to 'default' if
            // auth is not yet available (should not normally happen).
            this.orgName = getCurrentUser()?.tenant_id ?? 'default';
            const opts: nats.WsConnectionOptions = { servers: url };
            if (username && password) {
                opts.user = username;
                opts.pass = password;
            }
            if (inboxPrefix) opts.inboxPrefix = inboxPrefix;
            this.nc = await nats.wsconnect(opts);
            this.setNatsConnectionState('connected');
            this.monitorNatsConnection(this.nc);
            // Initial values come from REST; live updates need no JetStream access.
            await this.setupTreeSubscription();
            for (const path of this.desiredTagValuePaths) {
                this.watchTagValuePath(path);
                this.hydrateTagValuePath(path);
            }
            this.syncTagValueSubscriptions();
        } catch (err) {
            this.setNatsConnectionState('disconnected');
            console.error("Error connecting:", err);
        }
    }

    public subscribeNatsConnectionState(callback: NatsConnectionStateCallback): () => void {
        this.natsConnectionStateSubscribers.add(callback);
        callback(this.natsConnectionState);
        return () => this.natsConnectionStateSubscribers.delete(callback);
    }

    private setNatsConnectionState(state: NatsConnectionState): void {
        if (this.natsConnectionState === state) return;
        this.natsConnectionState = state;
        for (const callback of this.natsConnectionStateSubscribers) {
            callback(state);
        }
    }

    private monitorNatsConnection(connection: nats.NatsConnection): void {
        (async () => {
            for await (const status of connection.status()) {
                if (this.nc !== connection) return;
                if (status.type === 'reconnect') {
                    this.setNatsConnectionState('connected');
                } else if (
                    status.type === 'disconnect'
                    || status.type === 'reconnecting'
                    || status.type === 'staleConnection'
                    || status.type === 'forceReconnect'
                    || status.type === 'close'
                ) {
                    this.setNatsConnectionState('disconnected');
                }
            }
        })().catch((err) => {
            if (this.nc === connection) {
                this.setNatsConnectionState('disconnected');
                console.error('Error monitoring NATS connection:', err);
            }
        });

        connection.closed().then(() => {
            if (this.nc === connection) {
                this.setNatsConnectionState('disconnected');
            }
        });
    }

    public async storeDisconnectNats(): Promise<void> {

        this.treeSubscription?.unsubscribe();
        this.treeSubscription = null;
        this.treeSubscriptionsActive = false;

        for (const sub of this.watchedTagValuePaths.values()) {
            sub.unsubscribe();
        }
        this.watchedTagValuePaths.clear();
        for (const sub of this.watchedTagBatchPaths.values()) sub.unsubscribe();
        this.watchedTagBatchPaths.clear();
        this.rebuildValueSubscriptions = true;
        this.hydratedTagValuePaths.clear();

        if (this.nc) {
            await this.nc.close();
        }
        this.nc = null;
        this.setNatsConnectionState('disconnected');
    }

    public async request(subject: string, payload: unknown, timeoutMs: number): Promise<any> {
        if (!this.nc) throw new Error("NATS is not connected");
        const data = new TextEncoder().encode(JSON.stringify(payload));
        const msg = await this.nc.request(subject, data, { timeout: timeoutMs });
        const text = new TextDecoder().decode(msg.data);
        if (!text) return null;
        return JSON.parse(text);
    }

    public debugSubscribeSubject(subject: string): () => void {
        if (!this.nc) {
            console.warn('[xact:store:probe] NATS is not connected');
            return () => {};
        }
        console.log('[xact:store:probe] subscribing', subject);
        const sub = this.nc.subscribe(subject);
        (async () => {
            try {
                for await (const msg of sub) {
                    console.log('[xact:store:probe] message', msg.subject, msg.string());
                }
            } catch (err) {
                console.warn('[xact:store:probe] subscription error', err);
            } finally {
                console.log('[xact:store:probe] stopped', subject);
            }
        })();
        return () => {
            console.log('[xact:store:probe] unsubscribe', subject);
            sub.unsubscribe();
        };
    }

    public debugNatsState(): Record<string, any> {
        const state = {
            connected: Boolean(this.nc),
            org: this.orgName,
            desiredTagValuePaths: Array.from(this.desiredTagValuePaths),
            tagValueSubscription: this.watchedTagValuePaths.size > 0,
            hydratedTagValuePaths: Array.from(this.hydratedTagValuePaths),
        };
        console.log('[xact:store:probe] state', state);
        return state;
    }

    // Subscribe to a node by path. Creates nodes if they don't exist.
    // Values are hydrated through REST and updated through tenant-scoped NATS subscriptions.
    public subscribe(path: Path, callback: subscribeCallback): () => void {
        const pathElements = path.split('.');

        // Get the top-level node name and enforce org sandbox
        const topLevelNode = pathElements[0];
        if (this.orgName && topLevelNode !== this.orgName) {
            console.warn(`MirrorStore: subscribe("${path}") rejected - outside org "${this.orgName}"`);
            return () => {};
        }

        this.desiredTagValuePaths.add(path);
        this.watchTagValuePath(path);
        this.hydrateTagValuePath(path);

        let currentNode = this.root!;
        for (const element of pathElements) {
            currentNode = currentNode.getOrCreateChild(element);
        }

        const unsubscribe = currentNode.subscribe(callback);
        this.tagValueReferenceCounts.set(path, (this.tagValueReferenceCounts.get(path) ?? 0) + 1);
        let active = true;
        return () => {
            if (!active) return;
            active = false;
            unsubscribe();
            const count = (this.tagValueReferenceCounts.get(path) ?? 1) - 1;
            if (count) this.tagValueReferenceCounts.set(path, count);
            else { this.tagValueReferenceCounts.delete(path); this.desiredTagValuePaths.delete(path); this.unwatchedValueTimes.set(path, Date.now()); }
            this.scheduleTagValueSubscriptions(path);
        };
    }

    // Observe live values below a path without hydrating every child tag.
    // The initial subtree is already loaded by the application's REST snapshot.
    public subscribeToTagValueChanges(prefix: Path, callback: TagValueChangeCallback, descendants = true, fields: string[] = []): () => void {
        if (!prefix || this.orgName && prefix.split('.')[0] !== this.orgName) return () => {};
        let callbacks = this.tagValuePrefixSubscriptions.get(prefix);
        if (!callbacks) {
            callbacks = new Set();
            this.tagValuePrefixSubscriptions.set(prefix, callbacks);
        }
        callbacks.add(callback);
        for (const field of fields) {
            const path = `${prefix}.${field}`;
            this.selectedValueReferences.set(path, (this.selectedValueReferences.get(path) ?? 0) + 1);
        }
        if (descendants) this.descendantValueReferences.set(prefix, (this.descendantValueReferences.get(prefix) ?? 0) + 1);
        this.scheduleTagValueSubscriptions(prefix, descendants || fields.length > 0);
        let active = true;
        return () => {
            if (!active) return;
            active = false;
            for (const field of fields) {
                const path = `${prefix}.${field}`;
                const count = (this.selectedValueReferences.get(path) ?? 1) - 1;
                if (count) this.selectedValueReferences.set(path, count); else this.selectedValueReferences.delete(path);
            }
            callbacks!.delete(callback);
            if (callbacks!.size === 0) this.tagValuePrefixSubscriptions.delete(prefix);
            if (descendants) {
                const count = (this.descendantValueReferences.get(prefix) ?? 1) - 1;
                if (count) this.descendantValueReferences.set(prefix, count);
                else this.descendantValueReferences.delete(prefix);
            }
            this.scheduleTagValueSubscriptions(prefix, descendants || fields.length > 0);
        };
    }

    private tagValuePrefixListenersFor(path: Path): TagValueChangeCallback[] {
        const listeners: TagValueChangeCallback[] = [];
        for (let prefixPath = path; prefixPath; ) {
            const callbacks = this.tagValuePrefixSubscriptions.get(prefixPath);
            if (callbacks) listeners.push(...callbacks);
            const dot = prefixPath.lastIndexOf('.');
            if (dot < 0) break;
            prefixPath = prefixPath.slice(0, dot);
        }
        return listeners;
    }

    // Get the value at a given path. Enum tags resolve to their display text.
    public getNodeValue(path: Path): any {
        const pathElements = path.split('.');

        let currentNode = this.root!;
        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return undefined;
            }
            currentNode = child;
        }

        return currentNode.getValue();
    }

    // Get the raw stored value at a given path. Enum tags return their numeric ID.
    public getNodeRawValue(path: Path): any {
        const pathElements = path.split('.');

        let currentNode = this.root!;
        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return undefined;
            }
            currentNode = child;
        }

        return currentNode.getRawValue();
    }

    // Check if a node exists at the given path
    public nodeExists(path: Path): boolean {
        const pathElements = path.split('.');

        let currentNode = this.root!;
        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return false;
            }
            currentNode = child;
        }

        return true;
    }

    // List children names at a given path. Returns empty array if path doesn't exist.
    public listChildrenNames(path: Path): string[] {
        if (!path || path === '') {
            // Root level
            return this.root!.getChildrenNames();
        }

        const pathElements = path.split('.');
        let currentNode = this.root!;

        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return [];
            }
            currentNode = child;
        }

        return currentNode.getChildrenNames();
    }

    // Get config for a node at a given path
    public getNodeConfig(path: Path): any {
        if (!path || path === '') {
            return this.root!.getConfig();
        }

        const pathElements = path.split('.');
        let currentNode = this.root!;

        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return {};
            }
            currentNode = child;
        }

        return currentNode.getConfig();
    }

    // Get shared properties for a node at a given path
    public getNodeShared(path: Path): any {
        if (!path || path === '') {
            return this.root!.getShared();
        }

        const pathElements = path.split('.');
        let currentNode = this.root!;

        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return {};
            }
            currentNode = child;
        }

        return currentNode.getShared();
    }

    // Get status for a node at a given path
    public getNodeStatus(path: Path): string {
        if (!path || path === '') {
            return this.root!.getStatus();
        }

        const pathElements = path.split('.');
        let currentNode = this.root!;

        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return '';
            }
            currentNode = child;
        }

        return currentNode.getStatus();
    }

    // Get timestamp (Unix ms) for a node at a given path
    public getNodeTimestamp(path: Path): number {
        if (!path || path === '') return 0;
        const pathElements = path.split('.');
        let currentNode = this.root!;
        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) return 0;
            currentNode = child;
        }
        return currentNode.getTimestamp();
    }

    // Parse a user-supplied tag reference into its base tag path and optional selector.
    // Selectors include status codes (:U, :S, ...), built-ins (:value, :status,
    // :timestamp, :raw), and shared metadata fields (:description, :units, etc).
    public parseTagReference(path: string): TagReference {
        const ref = String(path ?? '').trim();
        const colonIdx = ref.lastIndexOf(':');
        if (colonIdx === -1) return { path: ref, selector: 'value' };
        return {
            path: ref.slice(0, colonIdx),
            selector: ref.slice(colonIdx + 1),
        };
    }

    public baseTagPath(path: string): string {
        return this.parseTagReference(path).path;
    }

    public isValueTagReference(path: string): boolean {
        const selector = this.parseTagReference(path).selector.toLowerCase();
        return selector === '' || selector === 'value';
    }

    // Resolve a user-supplied tag reference.
    // e.g. "meta.online:U" returns true if status is U, while
    // "meta.online:description" returns shared.description.
    public resolveTagReference(path: string): any {
        const { path: basePath, selector } = this.parseTagReference(path);
        if (!basePath) return undefined;
        const key = selector || 'value';
        const lowerKey = key.toLowerCase();

        if (lowerKey === 'value') return this.getNodeValue(basePath);
        if (lowerKey === 'raw' || lowerKey === 'rawvalue' || lowerKey === 'raw-value') {
            return this.getNodeRawValue(basePath);
        }
        if (lowerKey === 'status') return this.getNodeStatus(basePath);
        if (lowerKey === 'timestamp') return this.getNodeTimestamp(basePath);

        const statusCode = key.toUpperCase();
        if (STATUS_SELECTORS.has(statusCode)) {
            return this.getNodeStatus(basePath) === statusCode;
        }

        const shared = this.getNodeShared(basePath) || {};
        if (Object.prototype.hasOwnProperty.call(shared, key)) return shared[key];
        if (Object.prototype.hasOwnProperty.call(shared, lowerKey)) return shared[lowerKey];
        return undefined;
    }

    public subscribeTagReference(path: string, callback: subscribeCallback): () => void {
        const basePath = this.baseTagPath(path);
        if (!basePath) return () => {};
        let called = false;
        const unsubscribe = this.subscribe(basePath, () => {
            called = true;
            callback(this.resolveTagReference(path));
        });
        if (!called) callback(this.resolveTagReference(path));
        return unsubscribe;
    }

    // Backward-compatible alias for older widget code.
    public resolveTagPath(path: string): any {
        return this.resolveTagReference(path);
    }

    // Get node type (node or leaf)
    public getNodeType(path: Path): 'node' | 'leaf' | 'unknown' {
        if (!path || path === '') {
            return 'node'; // Root is always a node
        }

        const pathElements = path.split('.');
        let currentNode = this.root!;

        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) {
                return 'unknown';
            }
            currentNode = child;
        }

        return currentNode.getNodeType();
    }

    public getIsArray(path: Path): boolean {
        if (!path || path === '') return false;
        const pathElements = path.split('.');
        let currentNode = this.root!;
        for (const element of pathElements) {
            const child = currentNode['children'].get(element);
            if (!child) return false;
            currentNode = child;
        }
        return currentNode.getIsArray();
    }

    /** Load only this branch's immediate children, shared across widgets. */
    public ensureChildren(path: Path = ''): Promise<void> {
        if (this.publicSnapshotMode || !getCurrentUser()) return Promise.resolve();
        if (!this.orgName) this.orgName = getCurrentUser()?.tenant_id ?? 'default';
        const absolute = path ? this.toAbsolute(path) : this.orgName;
        if (this.loadedChildrenPaths.has(absolute)) return Promise.resolve();
        const pending = this.treeLoads.get(absolute);
        if (pending) return pending;
        const load = this.loadTreeFromAPI(absolute, 0).finally(() => this.treeLoads.delete(absolute));
        this.treeLoads.set(absolute, load);
        return load;
    }

    public async loadMatchingTags(search: string, status: string | null): Promise<void> {
        await this.loadTreeFromAPI('', -1, undefined, { search, status: status ?? '' });
    }

    /** Project the requested fields without mirroring unrelated device tags. */
    public async loadSelectedPaths(paths: string[]): Promise<void> {
        if (this.publicSnapshotMode || !paths.length || !getCurrentUser()) return;
        if (!this.orgName) this.orgName = getCurrentUser()?.tenant_id ?? 'default';
        const selected = [...new Set(paths.map(path => this.toRelative(path)))];
        for (let index = 0; index < selected.length; index += 128) await this.loadTreeFromAPI('', -1, selected.slice(index, index + 128));
    }

    // Load tree structure and metadata from REST API recursively
    // When depth is specified, fetches that many levels of children in a single request
    // (depth=-1 fetches entire subtree). When depth is undefined, uses recursive per-node fetching.
    public async loadTreeFromAPI(path: Path = '', depth?: number, select?: string[], filter?: { search: string; status: string; limit?: number }): Promise<void> {
        if (this.publicSnapshotMode) return;
        // REST hydration can precede live connection setup. Scope subscriptions
        // and relative widget paths to the authenticated org immediately.
        if (!path) this.orgName = getCurrentUser()?.tenant_id ?? 'default';
        try {
            const data = filter ? await loadNode(path, depth, select, filter) : select ? await loadNode(path, depth, select) : await loadNode(path, depth);
            if (filter) this.searchTruncated = !!data.truncated;

            // When loading the root (''), the server redirects to the user's org
            // root node. Use the response name as the effective path so children
            // are stored at e.g. 'default.NASA' rather than 'NASA' (which would
            // otherwise fail the OrgSandbox check on subsequent requests).
            const effectivePath = (!path && data.name) ? data.name : path;

            // Get or create the node for this path
            let currentNode = this.root!;
            if (effectivePath) {
                const pathElements = effectivePath.split('.');
                for (const element of pathElements) {
                    currentNode = currentNode.getOrCreateChild(element);
                }
            }

            // Set standard attributes for this node
            currentNode.setConfig(data.config || {});
            const rootShared = sharedWithDescription(data);
            if (rootShared) currentNode.setShared(rootShared);
            currentNode.setNodeType('node');
            if (data.isArray) this.markArrayNode(currentNode, effectivePath);

            if (!select && !filter) this.loadedChildrenPaths.add(effectivePath);
            // Process children
            if (data.children) {
                for (const child of data.children) {
                    const childPath = effectivePath ? `${effectivePath}.${child.name}` : child.name;

                    if (child.type === 'leaf') {
                        // If depth was specified, we already have full tag metadata in child
                        if (depth !== undefined) {
                            this.applyTagMetadataToNode(childPath, child);
                        } else {
                            // Load tag metadata separately
                            await this.loadTagMetadata(childPath);
                        }
                    } else {
                        // Create the node and set its attributes
                        const pathElements = childPath.split('.');
                        let currentNode = this.root!;
                        for (const element of pathElements) {
                            currentNode = currentNode.getOrCreateChild(element);
                        }
                        currentNode.setConfig(child.config || {});
                        const childShared = sharedWithDescription(child);
                        if (childShared) currentNode.setShared(childShared);
                        currentNode.setNodeType('node');
                        if (child.isArray) this.markArrayNode(currentNode, childPath);
                        if (child.value !== undefined) { currentNode.setStatus(child.status ?? ''); currentNode.setValue(child.value); }

                        // If depth was specified, children are already included in response
                        // Process them recursively using the same depth (don't decrement for nested)
                        if (depth !== undefined && child.children) {
                            this.processChildrenRecursive(currentNode, child.children, depth, childPath);
                        }

                        // For nodes, recurse if no depth limit was specified
                        if (depth === undefined) {
                            await this.loadTreeFromAPI(childPath);
                        }
                    }
                }
            }
        } catch (error) {
            console.error(`Failed to load tree from API at ${path}:`, error);
        }
    }

    // Process children recursively from an already-fetched response (no more API calls)
    // maxDepth: -1 means infinite (all descendants), 0 means no children, etc.
    private processChildrenRecursive(parentNode: Node, children: any[], maxDepth: number, parentPath: Path): void {
        for (const child of children) {
            const childNode = parentNode.getOrCreateChild(child.name);

            const childPath = `${parentPath}.${child.name}`;
            if (child.type === 'leaf') {
                this.applyTagMetadataToNode(childPath, child);
            } else {
                // It's a node
                if (child.config) childNode.setConfig(child.config);
                const childShared = sharedWithDescription(child);
                if (childShared) childNode.setShared(childShared);
                childNode.setNodeType('node');
                if (child.isArray) this.markArrayNode(childNode, childPath);
                if (child.value !== undefined) { childNode.setStatus(child.status ?? ''); childNode.setValue(child.value); }

                // Recurse if we haven't hit maxDepth (and there are children to process)
                if (maxDepth !== 0 && child.children && child.children.length > 0) {
                    // For maxDepth=-1, keep going; for positive maxDepth, we pass maxDepth-1
                    const nextDepth = maxDepth === -1 ? -1 : maxDepth - 1;
                    this.processChildrenRecursive(childNode, child.children, nextDepth, childPath);
                }
            }
        }
    }

    // Apply tag metadata from a server response directly to a node (no additional request needed)
    private applyTagMetadataToNode(path: Path, data: any): void {
        // Get or create the node for this path
        const pathElements = path.split('.');
        let currentNode = this.root!;
        for (const element of pathElements) {
            currentNode = currentNode.getOrCreateChild(element);
        }

        // Set attributes for this tag
        if (data.config) currentNode.setConfig(data.config);
        const shared = sharedWithDescription(data);
        if (shared) currentNode.setShared(shared);
        if (data.timestamp) currentNode.setTimestamp(data.timestamp);
        currentNode.setStatus('status' in data ? data.status : '');
        if (data.value !== undefined) {
            this.hydratedTagValuePaths.add(path);
            this.unwatchedValueTimes.delete(path);
            currentNode.setValue(data.value);
        }
        currentNode.setNodeType('leaf');
    }

    // Load metadata for a tag (leaf node)
    private loadTagMetadata(path: Path, skipIfNewerThanTimestamp?: number): Promise<boolean> {
        if (this.publicSnapshotMode) return Promise.resolve(false);
        const pending = this.tagMetadataLoads.get(path);
        if (pending) return pending;

        // A layer rebuild can subscribe to thousands of tags in one turn.
        // Share requests for the same tag and bound concurrent HTTP requests.
        const request = new Promise<boolean>(resolve => {
            this.tagMetadataQueue.push(() => {
                this.activeTagMetadataLoads++;
                void this.fetchTagMetadata(path, skipIfNewerThanTimestamp).then(success => {
                    this.tagMetadataLoads.delete(path);
                    this.activeTagMetadataLoads--;
                    resolve(success);
                    this.drainTagMetadataQueue();
                });
            });
        });
        this.tagMetadataLoads.set(path, request);
        this.drainTagMetadataQueue();
        return request;
    }

    private drainTagMetadataQueue(): void {
        while (this.activeTagMetadataLoads < this.maxTagMetadataLoads && this.tagMetadataQueue.length) {
            this.tagMetadataQueue.shift()!();
        }
    }

    private async fetchTagMetadata(path: Path, skipIfNewerThanTimestamp?: number): Promise<boolean> {
        try {
            if (skipIfNewerThanTimestamp !== undefined && !this.desiredTagValuePaths.has(path)) return false;
            const data = await loadTag(path);
            if (skipIfNewerThanTimestamp !== undefined && !this.desiredTagValuePaths.has(path)) return false;

            // Get or create the node for this path
            const pathElements = path.split('.');
            let currentNode = this.root!;
            for (const element of pathElements) {
                currentNode = currentNode.getOrCreateChild(element);
            }

            if (skipIfNewerThanTimestamp !== undefined && currentNode.getTimestamp() > skipIfNewerThanTimestamp) {
                return true;
            }

            // Set attributes for this tag
            currentNode.setConfig(data.config || {});
            const shared = sharedWithDescription(data);
            if (shared) currentNode.setShared(shared);
            if (data.timestamp) currentNode.setTimestamp(data.timestamp);
            currentNode.setStatus('status' in data ? data.status : '');
            if (data.value !== undefined) currentNode.setValue(data.value);
            currentNode.setNodeType('leaf');
            if (data.value !== undefined) this.hydratedTagValuePaths.add(path);
            return true;
        } catch (error) {
            console.error(`Failed to load tag metadata for ${path}:`, error);
            return false;
        }
    }

    private hydrateTagValuePath(path: Path): void {
        const lastUnwatched = this.unwatchedValueTimes.get(path);
        if (lastUnwatched !== undefined && Date.now() - lastUnwatched >= 5000) {
            this.hydratedTagValuePaths.delete(path);
            this.unwatchedValueTimes.delete(path);
        }
        if (this.publicSnapshotMode || !this.nc || !path || this.hydratedTagValuePaths.has(path)) {
            return;
        }
        const pathElements = path.split('.');
        if (this.orgName && pathElements[0] !== this.orgName) {
            return;
        }

        this.hydratedTagValuePaths.add(path);
        const timestampBeforeHydrate = this.getNodeTimestamp(path);
        this.loadTagMetadata(path, timestampBeforeHydrate).then(success => {
            if (!success) this.hydratedTagValuePaths.delete(path);
        }).catch(() => {
            this.hydratedTagValuePaths.delete(path);
        });
    }

    /** Apply only the tag values approved by the public dashboard endpoint. */
    public applyPublicSnapshot(org: string, values: Record<string, PublicTagValue>, replace = false): boolean {
        if (this.orgName && this.orgName !== org) {
            this.root = new Node('root', null);
            this.publicSnapshotValues.clear();
        }
        this.publicSnapshotMode = true;
        this.orgName = org;
        let added = false;
        const structure = new Map<string, { type: string } | null>();
        const changedValues = new Set<string>();
        for (const [path, data] of Object.entries(values)) {
            if (!path.startsWith(org + '.') || path.includes('..') || !data) continue;
            const previous = this.publicSnapshotValues.get(path);
            this.publicSnapshotValues.set(path, data);
            if (previous && publicValuesEqual(previous.value, data.value) &&
                previous.units === data.units && previous.description === data.description &&
                previous.status === data.status && previous.timestamp === data.timestamp) continue;
            const parts = path.split('.');
            let node: Node = this.root!;
            for (const [index, part] of parts.entries()) {
                if (!part) { node = this.root!; break; }
                const existing = node.getChildren().has(part);
                node = node.getOrCreateChild(part);
                if (!existing) {
                    added = true;
                    structure.set(parts.slice(0, index + 1).join('.'), { type: 'snapshot' });
                }
                node.setNodeType(index === parts.length - 1 ? 'leaf' : 'node');
            }
            if (node === this.root) continue;
            const shared = node.getShared();
            if (shared.units !== (data.units ?? '') || shared.description !== (data.description ?? '')) {
                node.setShared({ units: data.units ?? '', description: data.description ?? '' });
            }
            if (node.getStatus() !== (data.status ?? '')) node.setStatus(data.status ?? '');
            if (data.timestamp) node.setTimestamp(data.timestamp);
            if (!publicValuesEqual(node.getRawValue(), data.value)) {
                node.setValue(data.value);
                changedValues.add(path);
            }
        }
        if (replace) {
            const removed = [...this.publicSnapshotValues.keys()].filter(path => !Object.prototype.hasOwnProperty.call(values, path));
            const retainedBranches = new Set<string>(['']);
            if (removed.length) {
                for (const path of this.publicSnapshotValues.keys()) {
                    if (!Object.prototype.hasOwnProperty.call(values, path)) continue;
                    const parts = path.split('.');
                    for (let index = 1; index < parts.length; index++) retainedBranches.add(parts.slice(0, index).join('.'));
                }
            }
            for (const path of removed) {
                this.publicSnapshotValues.delete(path);
                const parts = path.split('.');
                const nodes: Node[] = [this.root!];
                for (const part of parts) {
                    const child = nodes.at(-1)!.getChildren().get(part);
                    if (!child) break;
                    nodes.push(child);
                }
                if (nodes.length !== parts.length + 1) continue;
                nodes.at(-1)!.setValue(undefined);
                for (let index = parts.length - 1; index >= 0; index--) {
                    nodes[index].removeChild(parts[index]);
                    structure.set(parts.slice(0, index + 1).join('.'), null);
                    // Subscriptions may have created placeholders for missing
                    // rule tags. They must not keep a departed bus on the map.
                    if (retainedBranches.has(parts.slice(0, index).join('.'))) break;
                }
                changedValues.add(path);
            }
        }
        // Publish structure after all coordinates have been applied. Existing
        // map subscriptions can add/remove devices without rebuilding layers.
        for (const [path, data] of structure) {
            const parent = path.slice(0, path.lastIndexOf('.'));
            for (const callback of this.treeSubscriptions.get(parent) ?? []) callback(path, data);
        }
        for (const path of changedValues) {
            for (const callback of this.tagValuePrefixListenersFor(path)) callback(path);
        }
        return added;
    }

    public setAuthenticatedOrg(): void {
        this.publicSnapshotMode = false;
        this.publicSnapshotValues.clear();
        this.orgName = getCurrentUser()?.tenant_id ?? 'default';
    }

    /** Returns the current organisation name (e.g. "default"). */
    public getOrg(): string {
        return this.orgName;
    }

    /**
     * Convert an org-relative path to an absolute (org-prefixed) path.
     * Idempotent: paths that already start with the org are returned unchanged.
     * Returns '' for empty input.
     */
    public toAbsolute(relativePath: string): string {
        if (!relativePath) return '';
        if (!this.orgName) return relativePath;
        if (relativePath === this.orgName || relativePath.startsWith(this.orgName + '.')) {
            return relativePath; // already absolute
        }
        return `${this.orgName}.${relativePath}`;
    }

    /**
     * Strip the org prefix from an absolute path, returning the org-relative form.
     * Idempotent: paths that don't start with the org are returned unchanged.
     * Returns '' if the path is exactly the org name.
     */
    public toRelative(absolutePath: string): string {
        if (!absolutePath || !this.orgName) return absolutePath;
        if (absolutePath === this.orgName) return '';
        if (absolutePath.startsWith(this.orgName + '.')) {
            return absolutePath.slice(this.orgName.length + 1);
        }
        return absolutePath; // already relative
    }

    // Compatibility helper: tree snapshots now come from REST rather than KV.
    public startKvWatch(orgName: string): void {
        if (orgName === this.orgName) void this.setupTreeSubscription();
    }

    // Subscribe to tree structural changes for a specific path
    public subscribeToTreeChanges(path: Path, callback: TreeChangeCallback): () => void {
        // Ensure tree subscription is active
        if (!this.treeSubscriptionsActive && this.nc) {
            this.setupTreeSubscription();
        }

        // Add callback to subscriptions
        if (!this.treeSubscriptions.has(path)) {
            this.treeSubscriptions.set(path, new Set());
        }
        this.treeSubscriptions.get(path)!.add(callback);
        if (path && this.getNodeType(path) !== 'leaf' && !this.getIsArray(path)) {
            void this.ensureChildren(path).then(() => {
                if (!this.treeSubscriptions.get(path)?.has(callback)) return;
                for (const name of this.listChildrenNames(path)) callback(`${path}.${name}`, { type: 'snapshot' });
            });
        }

        // Return unsubscribe function
        return () => {
            const callbacks = this.treeSubscriptions.get(path);
            if (callbacks) {
                callbacks.delete(callback);
                if (callbacks.size === 0) {
                    this.treeSubscriptions.delete(path);
                }
            }
        };
    }

    // Set up NATS subscription for tree changes - scoped to current org
    private async setupTreeSubscription(): Promise<void> {
        if (!this.nc || this.treeSubscriptionsActive) return;

        try {
            // Only receive structural updates for the current org's subtree.
            const subject = this.orgName
                ? `rtdb.tree.${this.orgName}.>`
                : 'rtdb.tree.>';
            const sub = this.nc.subscribe(subject);

            this.treeSubscription = sub;
            this.treeSubscriptionsActive = true;

            // Process messages
            (async () => {
                let sliceStarted = performance.now();
                for await (const msg of sub) {
                    this.handleTreeChange(msg);
                    // Large cascade deletes can queue hundreds of thousands of
                    // messages. Yield the main thread so timers, paint and user
                    // input are serviced while draining the subscription.
                    if (performance.now() - sliceStarted >= 8) {
                        await new Promise<void>(resolve => setTimeout(resolve, 0));
                        sliceStarted = performance.now();
                    }
                }
            })();
        } catch (err) {
            console.error('Failed to setup tree subscription:', err);
        }
    }

    // Remove a node (or leaf) from the store's mirror tree
    public removeNode(path: Path): void {
        if (!path) return;
        const pathElements = path.split('.');
        const nodeName = pathElements[pathElements.length - 1];

        let parent = this.root!;
        for (let i = 0; i < pathElements.length - 1; i++) {
            const child = parent['children'].get(pathElements[i]);
            if (!child) return;
            parent = child;
        }
        const removed = parent.getChildren().get(nodeName);
        if (!removed) return;
        const prune = (node: Node, nodePath: string) => {
            this.hydratedTagValuePaths.delete(nodePath);
            this.unwatchedValueTimes.delete(nodePath);
            this.loadedChildrenPaths.delete(nodePath);
            for (const [name, child] of node.getChildren()) prune(child, `${nodePath}.${name}`);
        };
        prune(removed, path);
        parent.removeChild(nodeName);
    }

    // Handle incoming tree change message
    private handleTreeChange(msg: nats.Msg): void {
        try {
            // Extract path from subject (rtdb.tree.building.floor1 -> building.floor1)
            const subject = msg.subject;
            const path = subject.replace('rtdb.tree.', '');
            const parent = path.slice(0, path.lastIndexOf('.'));
            // An unopened branch cannot affect the displayed tree. Discard its
            // metadata before JSON parsing instead of hydrating the tenant feed.
            if (!this.nodeExists(path) && !this.loadedChildrenPaths.has(parent) && !this.treeSubscriptions.has(path) && !this.treeSubscriptions.has(parent)) return;

            // Parse the message data
            const data = JSON.parse(msg.string());

            // null payload or {deleted:true} = deletion
            if (data === null || data?.deleted === true) {
                this.removeNode(path);
                this.notifyTreeSubscribers(path, null);
                return;
            }

            this.processIncomingNats({ key: path, value: msg.data }, data);
        } catch (err) {
            console.error('Error handling tree change:', err);
        }
    }

    // Notify all subscribers for a path and its ancestors
    private notifyTreeSubscribers(path: string, data: any): void {
        // Notify exact path subscribers
        const exactCallbacks = this.treeSubscriptions.get(path);
        if (exactCallbacks) {
            exactCallbacks.forEach(cb => cb(path, data));
        }

        // Notify parent path subscribers (for ancestors)
        const pathElements = path.split('.');
        for (let i = 1; i < pathElements.length; i++) {
            const parentPath = pathElements.slice(0, i).join('.');
            const parentCallbacks = this.treeSubscriptions.get(parentPath);
            if (parentCallbacks) {
                parentCallbacks.forEach(cb => cb(path, data));
            }
        }

        // Always notify root ('') subscribers
        const rootCallbacks = this.treeSubscriptions.get('');
        if (rootCallbacks) {
            rootCallbacks.forEach(cb => cb(path, data));
        }
    }

    // Subscribe to live updates for one concrete tag path.
    private watchTagValuePath(path: Path): void {
        this.scheduleTagValueSubscriptions(path);
    }

    private scheduleTagValueSubscriptions(path: string, rebuild = false): void {
        this.pendingValuePaths.add(path);
        this.rebuildValueSubscriptions ||= rebuild;
        if (this.tagValueSyncQueued) return;
        this.tagValueSyncQueued = true;
        queueMicrotask(() => { this.tagValueSyncQueued = false; this.syncTagValueSubscriptions(); });
    }

    private syncTagValueSubscriptions(): void {
        if (!this.nc || !this.orgName) return;
        const base = 'xact.internal.bcast.tagvalue.';
        const subscribe = (subject: string, subscriptions = this.watchedTagValuePaths) => {
            if (subscriptions.has(subject)) return;
            const sub = this.nc!.subscribe(subject);
            subscriptions.set(subject, sub);
            (async () => {
                try { for await (const msg of sub) this.handleTagValueMessage(msg); }
                catch { /* subscription closed */ }
                finally { if (subscriptions.get(subject) === sub) subscriptions.delete(subject); }
            })();
        };
        const covered = (path: string) => {
            const parts = path.split('.');
            return this.valueCoverage.some(pattern => pattern.every((part, index) =>
                part === '>' ? parts.length > index : part === '*' ? parts[index] !== undefined : part === parts[index]) &&
                (pattern.at(-1) === '>' || pattern.length === parts.length));
        };
        const wantsExact = (path: string) => path.split('.')[0] === this.orgName &&
            (this.desiredTagValuePaths.has(path) || this.tagValuePrefixSubscriptions.has(path)) && !covered(path);
        if (this.rebuildValueSubscriptions) {
            const prefixes = [...this.descendantValueReferences.keys()].filter(path => path.split('.')[0] === this.orgName);
            const wildcardPaths = new Set([...this.selectedValueReferences.keys()].filter(path => path.split('.')[0] === this.orgName));
            for (const prefix of prefixes) {
                if (!prefixes.some(other => other !== prefix && prefix.startsWith(other + '.'))) wildcardPaths.add(prefix + '.>');
            }
            this.valueCoverage = [...wildcardPaths].map(path => path.split('.'));
            const wanted = new Set([...wildcardPaths].map(path => base + path));
            for (const path of new Set([...this.desiredTagValuePaths, ...this.tagValuePrefixSubscriptions.keys()])) if (wantsExact(path)) wanted.add(base + path);
            for (const [subject, sub] of this.watchedTagValuePaths) if (!wanted.has(subject)) { sub.unsubscribe(); this.watchedTagValuePaths.delete(subject); }
            for (const subject of wanted) subscribe(subject);
            this.rebuildValueSubscriptions = false;
        } else {
            // Adding/removing one marker should not rescan every subscription.
            for (const path of this.pendingValuePaths) {
                const subject = base + path;
                if (wantsExact(path)) subscribe(subject);
                else { this.watchedTagValuePaths.get(subject)?.unsubscribe(); this.watchedTagValuePaths.delete(subject); }
            }
        }
        this.pendingValuePaths.clear();
        const containers = new Set([...this.tagValuePrefixSubscriptions.keys()].filter(prefix =>
            this.descendantValueReferences.has(prefix) || [...this.selectedValueReferences.keys()].some(path => path.startsWith(prefix + '.'))));
        const paths = [...this.watchedTagValuePaths.keys()].map(subject => subject.slice(base.length)).filter(path =>
            !containers.has(path) || this.desiredTagValuePaths.has(path) || this.getIsArray(path));
        const batches = new Set(tagBatchPatterns(paths));
        for (const [subject, sub] of this.watchedTagBatchPaths) {
            if (!batches.has(subject)) { sub.unsubscribe(); this.watchedTagBatchPaths.delete(subject); }
        }
        for (const subject of batches) subscribe(subject, this.watchedTagBatchPaths);
    }

    // Parse and apply a live tag value update message.
    private handleTagValueMessage(msg: nats.Msg): void {
        const org = this.orgName;
        if (msg.subject.startsWith(TAG_BATCH_PREFIX)) {
            try {
                for (const [path, value] of decodeTagBatch(msg.subject, JSON.parse(msg.string()), org)) {
                    const parts = path.split('.');
                    const selected = this.desiredTagValuePaths.has(path) || this.tagValuePrefixSubscriptions.has(path) ||
                        this.valueCoverage.some(pattern => pattern.every((part, index) =>
                            part === '>' ? parts.length > index : part === '*' ? parts[index] !== undefined : part === parts[index]) &&
                            (pattern.at(-1) === '>' || pattern.length === parts.length));
                    if (selected) this.applyTagValue(path, value);
                }
            } catch { /* Ignore malformed updates. */ }
            return;
        }
        // Strip everything up to and including the org segment so the remainder
        // is the relative device+taggroup+tag path (e.g. "NASA.ISS.env.cabin_pressure").
        // Prepend the org to get the full store path ("default.NASA.ISS.env.cabin_pressure").
        const prefix = `xact.internal.bcast.tagvalue.${org}.`;
        const path = org + "." + msg.subject.replace(prefix, "");
        const listeners = this.tagValuePrefixListenersFor(path);
        if (!this.desiredTagValuePaths.has(path) && listeners.length === 0) return;

        let data: Record<string, { type: string; value: any; status?: string; timestamp?: number }>;
        try {
            data = JSON.parse(msg.string());
        } catch (err) {
            return;
        }
        // data = { "leafname": { type: "value", value: ..., status: ... } }
        const tagValue = Object.values(data)[0] as { type: string; value: any; status?: string; timestamp?: number };
        if (!tagValue) return;
        this.applyTagValue(path, tagValue);
    }

    private applyTagValue(path: string, tagValue: TagValueUpdate): void {
        const listeners = this.tagValuePrefixListenersFor(path);
        if (!this.desiredTagValuePaths.has(path) && listeners.length === 0) return;
        const pathElements = path.split('.');
        let currentNode = this.root!;
        let parentNode: Node | null = null;
        for (const element of pathElements) {
            parentNode = currentNode;
            currentNode = currentNode.getOrCreateChild(element);
        }

        if (tagValue.type === 'array-start') {
            currentNode.setStatus('array-updating');
            for (const callback of listeners) callback(path);
            return; // Retain the previous complete array value.
        }

        // Array elements can be route coordinates, including custom tag paths.
        // Preserve their full precision rather than quantising map geometry.
        const isCoordinate = path.endsWith('.meta.lat') || path.endsWith('.meta.lon') || parentNode?.getIsArray();
        const displayValue = !isCoordinate && typeof tagValue.value === 'number' && !Number.isInteger(tagValue.value)
            ? parseFloat(tagValue.value.toFixed(2))
            : tagValue.value;
        if (tagValue.timestamp) currentNode.setTimestamp(tagValue.timestamp);
        currentNode.setStatus(tagValue.status ?? '');
        currentNode.setValue(displayValue);
        for (const callback of listeners) callback(path);
    }

    private markArrayNode(node: Node, path: string): void {
        const wasArray = node.getIsArray();
        node.setIsArray(true);
        if (!wasArray && this.tagValuePrefixSubscriptions.has(path)) this.scheduleTagValueSubscriptions(path, true);
    }

    private processIncomingNats(e: { key: string; value: Uint8Array }, parsed?: any) {
        // Split the key into path elements (e.g., "building.floor1.room2" -> ["building", "floor1", "room2"])
        const pathElements = e.key.split('.');

        // Start at root and traverse/create nodes as needed. Intermediate path
        // elements are containers; the final element's type comes from the payload.
        let currentNode = this.root!;
        for (let i = 0; i < pathElements.length; i++) {
            currentNode = currentNode.getOrCreateChild(pathElements[i]);
            if (i < pathElements.length - 1) {
                if (currentNode.getNodeType() === 'unknown') {
                    currentNode.setNodeType('node');
                }
            }
        }

        // Decode the value from Uint8Array to string, then try to parse as JSON
        let decodedValue: any = parsed;
        if (parsed === undefined) try {
            const textDecoder = new TextDecoder();
            const valueStr = textDecoder.decode(e.value);

            // Try to parse as JSON, fallback to raw string if parsing fails
            try {
                decodedValue = JSON.parse(valueStr);
            } catch {
                decodedValue = valueStr;
            }
        } catch (err) {
            console.error("Error decoding value:", err);
            decodedValue = e.value; // Use raw value as fallback
        }

        // Apply tree metadata and values from the tenant's live subscription.
        if (decodedValue && typeof decodedValue === 'object') {
            if (decodedValue.type === 'leaf' || decodedValue.type === 'node') {
                currentNode.setNodeType(decodedValue.type);
                if (decodedValue.config) currentNode.setConfig(decodedValue.config);
                const shared = sharedWithDescription(decodedValue);
                if (shared) currentNode.setShared(shared);
                if (decodedValue.timestamp) currentNode.setTimestamp(decodedValue.timestamp);
                if ('status' in decodedValue) currentNode.setStatus(decodedValue.status);
                if (decodedValue.value !== undefined) currentNode.setValue(decodedValue.value);
                if (decodedValue.isArray) this.markArrayNode(currentNode, e.key);
            } else if (decodedValue.type === 'value') {
                if (currentNode.getNodeType() === 'unknown') currentNode.setNodeType('leaf');
                if (decodedValue.timestamp) currentNode.setTimestamp(decodedValue.timestamp);
                if ('status' in decodedValue) currentNode.setStatus(decodedValue.status);
                if (decodedValue.value !== undefined) currentNode.setValue(decodedValue.value);
            } else if (currentNode.getNodeType() === 'unknown') {
                currentNode.setNodeType('leaf');
            }
        } else {
            // Primitive value payload fallback
            if (currentNode.getNodeType() === 'unknown') currentNode.setNodeType('leaf');
            currentNode.setValue(decodedValue);
        }

        // Notify components watching this node or its ancestors.
        this.notifyTreeSubscribers(e.key, decodedValue);
    }

    // Find a node, creating nodes as necessary
    // public findNode(path: Path): Node {
    //     let elements = path.split('/')
    //     let nextNode = this.root
    //     for (let i = 0; i < elements.length; i++) {
    //         elmName = elements[i]
    //         child = node.children[elmName]
    //         if node === null {
    //             child = new Node(elmName, parent)
    //             node.children[elmNode] = child
    //             node = child
    //         }
    //     }
    // }
}

// Singleton instance
let mirrorStoreInstance: MirrorStore | null = null;

export function getMirrorStore(): MirrorStore {
    if (!mirrorStoreInstance) {
        mirrorStoreInstance = new MirrorStore();
    }
    return mirrorStoreInstance;
}
