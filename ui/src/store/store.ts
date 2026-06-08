import *  as nats from "@nats-io/nats-core";
import { Kvm, KV, KvWatchOptions, KvWatchEntry } from "@nats-io/kv";
import { loadNode, loadTag } from '../api';
import { getCurrentUser } from '../auth';


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

export class MirrorStore {
    private nc: nats.NatsConnection | null = null;
    private kv: KV | null = null;
    private root: Node | null = null;
    private watchers: nats.QueuedIterator<KvWatchEntry>[] = [];
    private watchedTopLevelNodes: Set<string> = new Set();
    private desiredTagValuePaths: Set<string> = new Set();
    private watchedTagValuePaths: Map<string, nats.Subscription> = new Map();
    private tagValueSubscription: nats.Subscription | null = null;
    private hydratedTagValuePaths: Set<string> = new Set();
    private treeSubscriptions: Map<string, Set<TreeChangeCallback>> = new Map();
    private treeSubscriptionsActive: boolean = false;
    private orgName: string = '';


    constructor() {
        this.root = new Node('root', null);
    }

    // Connect to NAT and get the subtree below a root node.
    public async storeConnectNats(url: string, kvBucket: string, username?: string, password?: string): Promise<void> {
        try {
            // Determine the current org from the JWT. Fall back to 'default' if
            // auth is not yet available (should not normally happen).
            this.orgName = getCurrentUser()?.tenant_id ?? 'default';
            const opts: nats.WsConnectionOptions = { servers: url };
            if (username && password) {
                opts.user = username;
                opts.pass = password;
            }
            this.nc = await nats.wsconnect(opts);
            const kvm = new Kvm(this.nc);
            this.kv = await kvm.create(kvBucket);
            for (const path of this.desiredTagValuePaths) {
                this.watchTagValuePath(path);
                this.hydrateTagValuePath(path);
            }
        } catch (err) {
            console.error("Error connecting:", err);
        }
    }

    public async storeDisconnectNats(): Promise<void> {

        for (const w of this.watchers) {
            w.stop();
        }

        for (const sub of this.watchedTagValuePaths.values()) {
            sub.unsubscribe();
        }
        this.watchedTagValuePaths.clear();
        if (this.tagValueSubscription) {
            this.tagValueSubscription.unsubscribe();
            this.tagValueSubscription = null;
        }
        this.hydratedTagValuePaths.clear();

        if (this.nc) {
            await this.nc.close();
        }
        this.nc = null;
        this.kv = null;
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
            tagValueSubscription: Boolean(this.tagValueSubscription),
            hydratedTagValuePaths: Array.from(this.hydratedTagValuePaths),
            watchedTopLevelNodes: Array.from(this.watchedTopLevelNodes),
        };
        console.log('[xact:store:probe] state', state);
        return state;
    }

    // Subscribe to a node by path. Creates nodes if they don't exist.
    // Automatically watches the current org's NATS KV subtree on first call.
    public subscribe(path: Path, callback: subscribeCallback): () => void {
        const pathElements = path.split('.');

        // Get the top-level node name and enforce org sandbox
        const topLevelNode = pathElements[0];
        if (this.orgName && topLevelNode !== this.orgName) {
            console.warn(`MirrorStore: subscribe("${path}") rejected - outside org "${this.orgName}"`);
            return () => {};
        }

        // Watch the current org's KV subtree once (keyed by top-level org name)
        if (!this.watchedTopLevelNodes.has(topLevelNode) && this.kv !== null) {
            this.watchedTopLevelNodes.add(topLevelNode);
            this.watchNatsSubtreeTree(topLevelNode);
        }

        this.desiredTagValuePaths.add(path);
        this.watchTagValuePath(path);
        this.hydrateTagValuePath(path);

        let currentNode = this.root!;
        for (const element of pathElements) {
            currentNode = currentNode.getOrCreateChild(element);
        }

        return currentNode.subscribe(callback);
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

    // Load tree structure and metadata from REST API recursively
    // When depth is specified, fetches that many levels of children in a single request
    // (depth=-1 fetches entire subtree). When depth is undefined, uses recursive per-node fetching.
    public async loadTreeFromAPI(path: Path = '', depth?: number): Promise<void> {
        try {
            const data = await loadNode(path, depth);

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
            if (data.isArray) currentNode.setIsArray(true);

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
                        if (child.isArray) currentNode.setIsArray(true);

                        // If depth was specified, children are already included in response
                        // Process them recursively using the same depth (don't decrement for nested)
                        if (depth !== undefined && child.children) {
                            this.processChildrenRecursive(currentNode, child.children, depth);
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
    private processChildrenRecursive(parentNode: Node, children: any[], maxDepth: number): void {
        for (const child of children) {
            const childNode = parentNode.getOrCreateChild(child.name);

            if (child.type === 'leaf') {
                if (child.config) childNode.setConfig(child.config);
                if (child.shared) childNode.setShared(child.shared);
                if (child.timestamp) childNode.setTimestamp(child.timestamp);
                childNode.setStatus('status' in child ? child.status : '');
                if (child.value !== undefined) childNode.setValue(child.value);
                childNode.setNodeType('leaf');
            } else {
                // It's a node
                if (child.config) childNode.setConfig(child.config);
                const childShared = sharedWithDescription(child);
                if (childShared) childNode.setShared(childShared);
                childNode.setNodeType('node');
                if (child.isArray) childNode.setIsArray(true);

                // Recurse if we haven't hit maxDepth (and there are children to process)
                if (maxDepth !== 0 && child.children && child.children.length > 0) {
                    // For maxDepth=-1, keep going; for positive maxDepth, we pass maxDepth-1
                    const nextDepth = maxDepth === -1 ? -1 : maxDepth - 1;
                    this.processChildrenRecursive(childNode, child.children, nextDepth);
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
        if (data.value !== undefined) currentNode.setValue(data.value);
        currentNode.setNodeType('leaf');
    }

    // Load metadata for a tag (leaf node)
    private async loadTagMetadata(path: Path, skipIfNewerThanTimestamp?: number): Promise<void> {
        try {
            const data = await loadTag(path);

            // Get or create the node for this path
            const pathElements = path.split('.');
            let currentNode = this.root!;
            for (const element of pathElements) {
                currentNode = currentNode.getOrCreateChild(element);
            }

            if (skipIfNewerThanTimestamp !== undefined && currentNode.getTimestamp() > skipIfNewerThanTimestamp) {
                return;
            }

            // Set attributes for this tag
            currentNode.setConfig(data.config || {});
            const shared = sharedWithDescription(data);
            if (shared) currentNode.setShared(shared);
            if (data.timestamp) currentNode.setTimestamp(data.timestamp);
            currentNode.setStatus('status' in data ? data.status : '');
            if (data.value !== undefined) currentNode.setValue(data.value);
            currentNode.setNodeType('leaf');
        } catch (error) {
            console.error(`Failed to load tag metadata for ${path}:`, error);
        }
    }

    private hydrateTagValuePath(path: Path): void {
        if (!this.nc || !path || this.hydratedTagValuePaths.has(path)) {
            return;
        }
        const pathElements = path.split('.');
        if (this.orgName && pathElements[0] !== this.orgName) {
            return;
        }

        this.hydratedTagValuePaths.add(path);
        const timestampBeforeHydrate = this.getNodeTimestamp(path);
        this.loadTagMetadata(path, timestampBeforeHydrate).catch(() => {
            this.hydratedTagValuePaths.delete(path);
        });
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

    // Explicitly start the KV watcher for an org top-level node.
    // Normally the watcher is started lazily on the first subscribe() call; calling
    // this method eagerly lets callers pre-populate the store before reading values.
    public startKvWatch(orgName: string): void {
        if (!this.watchedTopLevelNodes.has(orgName) && this.kv !== null) {
            this.watchedTopLevelNodes.add(orgName);
            this.watchNatsSubtreeTree(orgName);
        }
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

            this.treeSubscriptionsActive = true;

            // Process messages
            (async () => {
                for await (const msg of sub) {
                    this.handleTreeChange(msg);
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
        parent.removeChild(nodeName);
    }

    // Handle incoming tree change message
    private handleTreeChange(msg: nats.Msg): void {
        try {
            // Extract path from subject (rtdb.tree.building.floor1 -> building.floor1)
            const subject = msg.subject;
            const path = subject.replace('rtdb.tree.', '');

            // Parse the message data
            const data = JSON.parse(msg.string());

            // null payload or {deleted:true} = deletion
            if (data === null || data?.deleted === true) {
                this.removeNode(path);
                this.notifyTreeSubscribers(path, null);
                return;
            }

            // Traverse/create nodes as needed so new nodes appear immediately
            const pathElements = path.split('.');
            let currentNode = this.root!;
            for (const element of pathElements) {
                currentNode = currentNode.getOrCreateChild(element);
            }

            // Update payload maps if provided
            if (data.config) currentNode.setConfig(data.config);
            if (data.shared) currentNode.setShared(data.shared);
            if (data.timestamp) currentNode.setTimestamp(data.timestamp);
            if ('status' in data) currentNode.setStatus(data.status);
            if (data.value !== undefined) currentNode.setValue(data.value);
            if (data.type === 'node') currentNode.setNodeType('node');
            else if (data.type === 'leaf') currentNode.setNodeType('leaf');
            if (data.isArray) currentNode.setIsArray(true);

            // Notify subscribers
            this.notifyTreeSubscribers(path, data);
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
        if (!this.nc || !path) {
            return;
        }
        const pathElements = path.split('.');
        if (pathElements[0] !== this.orgName) {
            return;
        }
        if (this.tagValueSubscription) {
            return;
        }

        const subject = `xact.internal.bcast.tagvalue.${this.orgName}.>`;
        const sub = this.nc.subscribe(subject);
        this.tagValueSubscription = sub;

        (async () => {
            try {
                for await (const msg of sub) {
                    this.handleTagValueMessage(msg);
                }
            } catch (err) {
                // ignore subscription close/error; callers can resubscribe on reconnect
            } finally {
                if (this.tagValueSubscription === sub) {
                    this.tagValueSubscription = null;
                }
            }
        })();
    }

    // Parse and apply a live tag value update message.
    private handleTagValueMessage(msg: nats.Msg): void {
        const org = this.orgName;
        // Strip everything up to and including the org segment so the remainder
        // is the relative device+taggroup+tag path (e.g. "NASA.ISS.env.cabin_pressure").
        // Prepend the org to get the full store path ("default.NASA.ISS.env.cabin_pressure").
        const prefix = `xact.internal.bcast.tagvalue.${org}.`;
        const path = org + "." + msg.subject.replace(prefix, "");
        const wanted = this.desiredTagValuePaths.has(path);
        if (!wanted) return;

        let data: Record<string, { type: string; value: any; status?: string; timestamp?: number }>;
        try {
            data = JSON.parse(msg.string());
        } catch (err) {
            return;
        }
        // data = { "leafname": { type: "value", value: ..., status: ... } }
        const tagValue = Object.values(data)[0] as { type: string; value: any; status?: string; timestamp?: number };
        if (!tagValue) return;


        const pathElements = path.split('.');
        let currentNode = this.root!;
        for (const element of pathElements) {
            currentNode = currentNode.getOrCreateChild(element);
        }

        const displayValue = typeof tagValue.value === 'number' && !Number.isInteger(tagValue.value)
            ? parseFloat(tagValue.value.toFixed(2))
            : tagValue.value;
        if (tagValue.timestamp) currentNode.setTimestamp(tagValue.timestamp);
        currentNode.setStatus(tagValue.status ?? '');
        currentNode.setValue(displayValue);
    }

    // Watch NATS node
    private async watchNatsSubtreeTree(topLevel: string) {
        let opts: KvWatchOptions = {
            key: `${topLevel}.>`
        };
        const w = await this.kv!.watch(opts);
        this.watchers.push(w);

        // Start the iterator for this watcher
        (async () => {
            try {
                for await (const e of w) {
                    if (e !== null) {
                        this.processIncomingNats(e);
                    }
                }
            } catch (err) {
                // ignore watcher error
            }
        })();
    }
    private processIncomingNats(e: KvWatchEntry) {
        // Split the key into path elements (e.g., "building.floor1.room2" -> ["building", "floor1", "room2"])
        const pathElements = e.key.split('.');

        // Start at root and traverse/create nodes as needed. Intermediate path
        // elements are containers; the final element's type comes from the KV payload.
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
        let decodedValue: any;
        try {
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

        // Process the payload depending on the type in the packet. Value
        // updates normally arrive via JetStream, but accepting them here keeps
        // KV snapshots/tests and direct replay paths consistent.
        if (decodedValue && typeof decodedValue === 'object') {
            if (decodedValue.type === 'leaf' || decodedValue.type === 'node') {
                currentNode.setNodeType(decodedValue.type);
                if (decodedValue.config) currentNode.setConfig(decodedValue.config);
                const shared = sharedWithDescription(decodedValue);
                if (shared) currentNode.setShared(shared);
                if (decodedValue.timestamp) currentNode.setTimestamp(decodedValue.timestamp);
                if ('status' in decodedValue) currentNode.setStatus(decodedValue.status);
                if (decodedValue.value !== undefined) currentNode.setValue(decodedValue.value);
                if (decodedValue.isArray) currentNode.setIsArray(true);
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

        // Notify tree subscribers so that components using subscribeToTreeChanges
        // (e.g. the map widget) pick up nodes delivered via the KV watch, not just
        // live NATS tree-change messages.
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
