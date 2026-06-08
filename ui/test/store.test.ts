// Store Tests - Tests for the MirrorStore class without a running backend.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const natsMock = vi.hoisted(() => ({
  wsconnect: vi.fn(),
}));

const kvMock = vi.hoisted(() => ({
  create: vi.fn(),
}));

const apiMock = vi.hoisted(() => ({
  loadNode: vi.fn(),
  loadTag: vi.fn(),
}));

const authMock = vi.hoisted(() => ({
  getCurrentUser: vi.fn(),
}));

vi.mock('@nats-io/nats-core', () => ({
  wsconnect: natsMock.wsconnect,
}));

vi.mock('@nats-io/kv', () => ({
  Kvm: vi.fn(function Kvm() {
    return { create: kvMock.create };
  }),
}));

vi.mock('../src/api', () => ({
  loadNode: apiMock.loadNode,
  loadTag: apiMock.loadTag,
}));

vi.mock('../src/auth', () => ({
  getCurrentUser: authMock.getCurrentUser,
}));

import { getMirrorStore, MirrorStore } from '../src/store/store';

interface PushableAsyncIterator<T> extends AsyncIterable<T> {
  push(value: T): void;
  stop(): void;
  stopped: boolean;
}

function createAsyncIterator<T>(): PushableAsyncIterator<T> {
  const values: T[] = [];
  const waiters: Array<(result: IteratorResult<T>) => void> = [];
  const iterator = {
    stopped: false,
    push(value: T) {
      if (this.stopped) return;
      const waiter = waiters.shift();
      if (waiter) waiter({ value, done: false });
      else values.push(value);
    },
    stop() {
      this.stopped = true;
      while (waiters.length) {
        waiters.shift()!({ value: undefined as T, done: true });
      }
    },
    [Symbol.asyncIterator]() {
      return {
        next: () => {
          if (values.length) {
            return Promise.resolve({ value: values.shift()!, done: false });
          }
          if (iterator.stopped) {
            return Promise.resolve({ value: undefined as T, done: true });
          }
          return new Promise<IteratorResult<T>>(resolve => waiters.push(resolve));
        },
      };
    },
  };
  return iterator;
}

function msg(subject: string, payload: unknown): any {
  const text = typeof payload === 'string' ? payload : JSON.stringify(payload);
  return {
    subject,
    data: new TextEncoder().encode(text),
    string: () => text,
  };
}

function kvEntry(key: string, payload: unknown): any {
  const text = typeof payload === 'string' ? payload : JSON.stringify(payload);
  return {
    key,
    value: new TextEncoder().encode(text),
    revision: 1,
    delta: 0,
    length: 1,
    operation: 'PUT',
  };
}

function createSubscription() {
  const iterator = createAsyncIterator<any>();
  return Object.assign(iterator, {
    unsubscribe: vi.fn(() => iterator.stop()),
  });
}

function createNatsConnection() {
  const subscriptions: Record<string, ReturnType<typeof createSubscription>> = {};
  const nc = {
    subscriptions,
    close: vi.fn(async () => undefined),
    closed: vi.fn(() => Promise.resolve(null)),
    status: vi.fn(() => createAsyncIterator<any>()),
    request: vi.fn(),
    subscribe: vi.fn((subject: string) => {
      const sub = createSubscription();
      subscriptions[subject] = sub;
      return sub;
    }),
  };
  return nc;
}

function createKvStore() {
  const watchers: Array<PushableAsyncIterator<any>> = [];
  return {
    watchers,
    watch: vi.fn(async () => {
      const watcher = createAsyncIterator<any>();
      watchers.push(watcher);
      return watcher;
    }),
  };
}

async function flushAsyncWork(): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, 0));
}

function seedLeaf(store: MirrorStore, path: string, payload: Record<string, any> = {}): void {
  store['processIncomingNats'](kvEntry(path, {
    type: 'leaf',
    config: { type: 1, ...payload.config },
    shared: { description: 'Seeded tag', units: 'C', ...payload.shared },
    status: payload.status ?? 'N',
    timestamp: payload.timestamp ?? 100,
    value: payload.value ?? 23.5,
  }));
}

describe('MirrorStore connection and NATS helpers', () => {
  let store: MirrorStore;
  let nc: ReturnType<typeof createNatsConnection>;
  let kv: ReturnType<typeof createKvStore>;

  beforeEach(() => {
    store = new MirrorStore();
    nc = createNatsConnection();
    kv = createKvStore();
    natsMock.wsconnect.mockResolvedValue(nc);
    kvMock.create.mockResolvedValue(kv);
    apiMock.loadTag.mockResolvedValue({ config: {}, shared: {}, status: 'N', value: 7 });
    authMock.getCurrentUser.mockReturnValue({ tenant_id: 'default' });
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.spyOn(console, 'log').mockImplementation(() => undefined);
  });

  afterEach(async () => {
    await store.storeDisconnectNats();
    vi.restoreAllMocks();
    vi.clearAllMocks();
    localStorage.clear();
  });

  it('connects with credentials, creates the KV bucket, and hydrates desired tag paths', async () => {
    const seen: any[] = [];
    store.subscribe('default.Device.temp', value => seen.push(value));

    await store.storeConnectNats('ws://nats.example', 'mirror', 'alice', 'secret');
    await flushAsyncWork();

    expect(natsMock.wsconnect).toHaveBeenCalledWith({
      servers: 'ws://nats.example',
      user: 'alice',
      pass: 'secret',
    });
    expect(kvMock.create).toHaveBeenCalledWith('mirror');
    expect(nc.subscribe).toHaveBeenCalledWith('xact.internal.bcast.tagvalue.default.>');
    expect(apiMock.loadTag).toHaveBeenCalledWith('default.Device.temp');
    expect(store.getOrg()).toBe('default');
    expect(store.getNodeValue('default.Device.temp')).toBe(7);
    expect(seen).toEqual([undefined, undefined, undefined, 7]);
  });

  it('falls back to the default org and logs connection failures', async () => {
    const error = new Error('no socket');
    authMock.getCurrentUser.mockReturnValue(null);
    natsMock.wsconnect.mockRejectedValue(error);

    await store.storeConnectNats('ws://broken', 'mirror');

    expect(store.getOrg()).toBe('default');
    expect(console.error).toHaveBeenCalledWith('Error connecting:', error);
  });

  it('requests JSON payloads through NATS and handles empty responses', async () => {
    await expect(store.request('subject', { a: 1 }, 50)).rejects.toThrow('NATS is not connected');

    await store.storeConnectNats('ws://nats.example', 'mirror');
    nc.request
      .mockResolvedValueOnce({ data: new TextEncoder().encode('{"ok":true}') })
      .mockResolvedValueOnce({ data: new Uint8Array() });

    await expect(store.request('subject', { a: 1 }, 50)).resolves.toEqual({ ok: true });
    await expect(store.request('empty', null, 25)).resolves.toBeNull();

    expect(nc.request).toHaveBeenNthCalledWith(
      1,
      'subject',
      new TextEncoder().encode('{"a":1}'),
      { timeout: 50 },
    );
  });

  it('cleans up KV watchers, tag subscriptions, and the NATS connection on disconnect', async () => {
    await store.storeConnectNats('ws://nats.example', 'mirror');
    store.subscribe('default.Device.temp', () => undefined);
    store.startKvWatch('default');
    await flushAsyncWork();

    await store.storeDisconnectNats();
    const state = store.debugNatsState();

    expect(kv.watch).toHaveBeenCalledWith({ key: 'default.>' });
    expect(kv.watchers[0].stopped).toBe(true);
    expect(nc.subscriptions['xact.internal.bcast.tagvalue.default.>'].unsubscribe).toHaveBeenCalled();
    expect(nc.close).toHaveBeenCalled();
    expect(state.connected).toBe(false);
    expect(state.hydratedTagValuePaths).toEqual([]);
    expect(state.tagValueSubscription).toBe(false);
  });

  it('supports debug subject subscriptions and disconnected debug no-ops', async () => {
    const disconnectedUnsub = store.debugSubscribeSubject('probe');
    disconnectedUnsub();
    expect(console.warn).toHaveBeenCalledWith('[xact:store:probe] NATS is not connected');

    await store.storeConnectNats('ws://nats.example', 'mirror');
    const unsubscribe = store.debugSubscribeSubject('probe');

    nc.subscriptions.probe.push(msg('probe', { hello: 'world' }));
    await flushAsyncWork();
    unsubscribe();

    expect(nc.subscribe).toHaveBeenCalledWith('probe');
    expect(nc.subscriptions.probe.unsubscribe).toHaveBeenCalled();
    expect(console.log).toHaveBeenCalledWith('[xact:store:probe] subscribing', 'probe');
  });

  it('removes failed hydration paths so they can be retried', async () => {
    store['nc'] = nc as any;
    store['orgName'] = 'default';
    store['loadTagMetadata'] = vi.fn().mockRejectedValue(new Error('hydrate failed')) as any;

    store['hydrateTagValuePath']('default.Device.temp');
    await flushAsyncWork();

    expect(store.debugNatsState().hydratedTagValuePaths).toEqual([]);
  });
});

describe('MirrorStore local tree operations and selectors', () => {
  let store: MirrorStore;

  beforeEach(() => {
    store = new MirrorStore();
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.spyOn(console, 'log').mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('creates intermediate nodes, lists children, and reads node metadata from KV snapshots', () => {
    seedLeaf(store, 'building.floor1.room101.temperature', {
      config: { unit: 'C' },
      shared: { description: 'Room Temp', deadband: 0.25 },
      value: 23.5,
    });

    expect(store.nodeExists('building.floor1.room101.temperature')).toBe(true);
    expect(store.nodeExists('building.floor2')).toBe(false);
    expect(store.listChildrenNames('')).toEqual(['building']);
    expect(store.listChildrenNames('building.floor1')).toEqual(['room101']);
    expect(store.listChildrenNames('missing')).toEqual([]);
    expect(store.getNodeType('')).toBe('node');
    expect(store.getNodeType('building.floor1')).toBe('node');
    expect(store.getNodeType('building.floor1.room101.temperature')).toBe('leaf');
    expect(store.getNodeConfig('building.floor1.room101.temperature')).toEqual({ type: 1, unit: 'C' });
    expect(store.getNodeShared('building.floor1.room101.temperature')).toEqual({
      description: 'Room Temp',
      units: 'C',
      deadband: 0.25,
    });
    expect(store.getNodeStatus('building.floor1.room101.temperature')).toBe('N');
    expect(store.getNodeTimestamp('building.floor1.room101.temperature')).toBe(100);
    expect(store.getNodeValue('missing')).toBeUndefined();
    expect(store.getNodeConfig('missing')).toEqual({});
    expect(store.getNodeShared('missing')).toEqual({});
    expect(store.getNodeStatus('missing')).toBe('');
    expect(store.getNodeTimestamp('missing')).toBe(0);
  });

  it('handles node, value, primitive, malformed JSON, and undecodable KV payloads', () => {
    store['processIncomingNats'](kvEntry('default.Templates.AirQualityStandard', {
      type: 'node',
      config: { category: 'template' },
      isArray: true,
    }));
    store['processIncomingNats'](kvEntry('default.Device.temp', {
      type: 'value',
      value: 28,
      status: 'A',
      timestamp: 200,
    }));
    store['processIncomingNats'](kvEntry('default.Device.unknownObject', {
      custom: true,
    }));
    store['processIncomingNats'](kvEntry('default.Device.rawText', 'not-json'));
    store['processIncomingNats']({
      key: 'default.Device.badBytes',
      value: null,
      revision: 1,
      delta: 0,
      length: 1,
      operation: 'PUT',
    } as any);

    expect(store.getNodeType('default.Templates.AirQualityStandard')).toBe('node');
    expect(store.getNodeConfig('default.Templates.AirQualityStandard')).toEqual({ category: 'template' });
    expect(store.getIsArray('default.Templates.AirQualityStandard')).toBe(true);
    expect(store.getNodeValue('default.Device.temp')).toBe(28);
    expect(store.getNodeStatus('default.Device.temp')).toBe('A');
    expect(store.getNodeTimestamp('default.Device.temp')).toBe(200);
    expect(store.getNodeType('default.Device.unknownObject')).toBe('leaf');
    expect(store.getNodeValue('default.Device.rawText')).toBe('not-json');
    expect(store.getNodeType('default.Device.rawText')).toBe('leaf');
    expect(store.nodeExists('default.Device.badBytes')).toBe(true);
    expect(console.error).toHaveBeenCalledWith('Error decoding value:', expect.any(TypeError));
  });

  it('returns enum display values by default while preserving raw values', () => {
    seedLeaf(store, 'building.floor1.room101.mode', {
      config: { type: 4 },
      shared: { enumValues: { '0': 'Off', '1': 'On', '2': 'Auto' } },
      value: 2,
    });

    expect(store.getNodeValue('building.floor1.room101.mode')).toBe('Auto');
    expect(store.getNodeRawValue('building.floor1.room101.mode')).toBe(2);

    store['processIncomingNats'](kvEntry('building.floor1.room101.mode', {
      type: 'value',
      value: 99,
      status: 'N',
    }));

    expect(store.getNodeValue('building.floor1.room101.mode')).toBe(99);
    expect(store.getNodeRawValue('building.floor1.room101.mode')).toBe(99);
  });

  it('subscribes, immediately emits existing values, updates subscribers, and unsubscribes', () => {
    const seen: any[] = [];
    seedLeaf(store, 'building.floor1.room101.temperature', { value: 23.5 });

    const unsubscribe = store.subscribe('building.floor1.room101.temperature', value => seen.push(value));
    store['processIncomingNats'](kvEntry('building.floor1.room101.temperature', {
      type: 'value',
      value: 24,
      status: 'N',
    }));
    unsubscribe();
    store['processIncomingNats'](kvEntry('building.floor1.room101.temperature', {
      type: 'value',
      value: 25,
      status: 'N',
    }));

    expect(seen).toEqual([23.5, 23.5, 24]);
    expect(store.getNodeValue('building.floor1.room101.temperature')).toBe(25);
  });

  it('rejects subscriptions outside the connected org sandbox', () => {
    store['orgName'] = 'default';
    const callback = vi.fn();

    const unsubscribe = store.subscribe('other.Device.temp', callback);
    unsubscribe();

    expect(callback).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalledWith(
      'MirrorStore: subscribe("other.Device.temp") rejected - outside org "default"',
    );
    expect(store.debugNatsState().desiredTagValuePaths).toEqual([]);
  });

  it('parses and resolves tag-reference selectors', () => {
    seedLeaf(store, 'building.floor1.room101.temperature', {
      shared: { description: 'Room Temp', units: 'C', loweronly: 'fallback' },
      status: 'N',
      timestamp: 555,
      value: 23.5,
    });

    expect(store.parseTagReference('building.floor1.room101.temperature:description')).toEqual({
      path: 'building.floor1.room101.temperature',
      selector: 'description',
    });
    expect(store.parseTagReference('building:floor1:temperature:raw')).toEqual({
      path: 'building:floor1:temperature',
      selector: 'raw',
    });
    expect(store.parseTagReference('  path.to.tag  ')).toEqual({ path: 'path.to.tag', selector: 'value' });
    expect(store.baseTagPath('building.floor1.room101.temperature:description')).toBe('building.floor1.room101.temperature');
    expect(store.isValueTagReference('building.floor1.room101.temperature')).toBe(true);
    expect(store.isValueTagReference('building.floor1.room101.temperature:')).toBe(true);
    expect(store.isValueTagReference('building.floor1.room101.temperature:units')).toBe(false);
    expect(store.resolveTagReference('')).toBeUndefined();
    expect(store.resolveTagReference('building.floor1.room101.temperature')).toBe(23.5);
    expect(store.resolveTagReference('building.floor1.room101.temperature:value')).toBe(23.5);
    expect(store.resolveTagReference('building.floor1.room101.temperature:raw-value')).toBe(23.5);
    expect(store.resolveTagReference('building.floor1.room101.temperature:status')).toBe('N');
    expect(store.resolveTagReference('building.floor1.room101.temperature:timestamp')).toBe(555);
    expect(store.resolveTagReference('building.floor1.room101.temperature:N')).toBe(true);
    expect(store.resolveTagReference('building.floor1.room101.temperature:A')).toBe(false);
    expect(store.resolveTagReference('building.floor1.room101.temperature:description')).toBe('Room Temp');
    expect(store.resolveTagReference('building.floor1.room101.temperature:LOWERONLY')).toBe('fallback');
    expect(store.resolveTagReference('building.floor1.room101.temperature:missing')).toBeUndefined();
    expect(store.resolveTagPath('building.floor1.room101.temperature:units')).toBe('C');
  });

  it('subscribes to tag references through the base tag and emits an initial unresolved value', () => {
    const seen: any[] = [];
    const unsubscribe = store.subscribeTagReference('building.floor1.room101.humidity:description', value => {
      seen.push(value);
    });

    seedLeaf(store, 'building.floor1.room101.humidity', {
      shared: { description: 'Room humidity', units: '%' },
      status: 'U',
      value: 55,
    });

    unsubscribe();
    seedLeaf(store, 'building.floor1.room101.humidity', {
      shared: { description: 'Ignored after unsubscribe', units: '%' },
      value: 56,
    });

    expect(seen).toEqual([undefined, undefined, 'Room humidity', 'Room humidity', 'Room humidity']);
    expect(store.subscribeTagReference('', vi.fn())).toEqual(expect.any(Function));
  });

  it('converts absolute and relative paths using the active org', () => {
    expect(store.toAbsolute('Device.temp')).toBe('Device.temp');
    expect(store.toRelative('default.Device.temp')).toBe('default.Device.temp');

    store['orgName'] = 'default';

    expect(store.toAbsolute('')).toBe('');
    expect(store.toAbsolute('Device.temp')).toBe('default.Device.temp');
    expect(store.toAbsolute('default.Device.temp')).toBe('default.Device.temp');
    expect(store.toAbsolute('default')).toBe('default');
    expect(store.toRelative('')).toBe('');
    expect(store.toRelative('default')).toBe('');
    expect(store.toRelative('default.Device.temp')).toBe('Device.temp');
    expect(store.toRelative('Device.temp')).toBe('Device.temp');
  });

  it('removes existing nodes and ignores missing paths', () => {
    seedLeaf(store, 'default.Device.temp');

    store.removeNode('');
    store.removeNode('default.Missing.temp');
    expect(store.nodeExists('default.Device.temp')).toBe(true);

    store.removeNode('default.Device.temp');
    expect(store.nodeExists('default.Device.temp')).toBe(false);
    expect(store.nodeExists('default.Device')).toBe(true);
  });
});

describe('MirrorStore API tree loading', () => {
  let store: MirrorStore;

  beforeEach(() => {
    store = new MirrorStore();
    authMock.getCurrentUser.mockReturnValue({ tenant_id: 'default' });
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it('loads depth-limited API trees using the returned org root name', async () => {
    apiMock.loadNode.mockResolvedValue({
      name: 'default',
      config: { kind: 'org' },
      shared: { owner: 'ops' },
      isArray: true,
      children: [
        {
          name: 'Device',
          type: 'node',
          description: 'Pump device',
          config: { class: 'pump' },
          isArray: true,
          children: [
            {
              name: 'temp',
              type: 'leaf',
              config: { type: 1 },
              shared: { units: 'C' },
              status: '',
              timestamp: 1234,
              value: 21.25,
            },
            {
              name: 'meta',
              type: 'node',
              children: [
                {
                  name: 'serial',
                  type: 'leaf',
                  config: { type: 2 },
                  shared: { description: 'Serial' },
                  value: 'A-1',
                },
              ],
            },
          ],
        },
      ],
    });

    await store.loadTreeFromAPI('', -1);

    expect(apiMock.loadNode).toHaveBeenCalledWith('', -1);
    expect(store.getNodeConfig('default')).toEqual({ kind: 'org' });
    expect(store.getNodeShared('default')).toEqual({ owner: 'ops' });
    expect(store.getIsArray('default')).toBe(true);
    expect(store.getNodeType('default.Device')).toBe('node');
    expect(store.getNodeConfig('default.Device')).toEqual({ class: 'pump' });
    expect(store.getNodeShared('default.Device')).toEqual({ description: 'Pump device' });
    expect(store.resolveTagReference('default.Device:description')).toBe('Pump device');
    expect(store.getIsArray('default.Device')).toBe(true);
    expect(store.getNodeType('default.Device.temp')).toBe('leaf');
    expect(store.getNodeValue('default.Device.temp')).toBe(21.25);
    expect(store.getNodeStatus('default.Device.temp')).toBe('');
    expect(store.getNodeTimestamp('default.Device.temp')).toBe(1234);
    expect(store.getNodeType('default.Device.meta.serial')).toBe('leaf');
    expect(store.getNodeValue('default.Device.meta.serial')).toBe('A-1');
  });

  it('recursively loads nodes and tag metadata when no depth is supplied', async () => {
    apiMock.loadNode
      .mockResolvedValueOnce({
        name: 'default',
        children: [
          { name: 'Device', type: 'node', config: { class: 'pump' } },
          { name: 'rootTag', type: 'leaf' },
        ],
      })
      .mockResolvedValueOnce({
        name: 'Device',
        children: [
          { name: 'temp', type: 'leaf' },
        ],
      });
    apiMock.loadTag.mockImplementation(async (path: string) => {
      if (path === 'default.rootTag') return {
        config: { type: 2 },
        shared: { description: 'Root tag' },
        status: '',
        timestamp: 2345,
        value: 'ready',
      };
      if (path === 'default.Device.temp') return {
        config: { type: 1 },
        shared: { units: 'C' },
        status: 'U',
        value: 19,
      };
      throw new Error(`unexpected tag path ${path}`);
    });

    await store.loadTreeFromAPI('');

    expect(apiMock.loadNode).toHaveBeenNthCalledWith(1, '', undefined);
    expect(apiMock.loadNode).toHaveBeenNthCalledWith(2, 'default.Device', undefined);
    expect(apiMock.loadTag).toHaveBeenCalledWith('default.rootTag');
    expect(apiMock.loadTag).toHaveBeenCalledWith('default.Device.temp');
    expect(store.getNodeValue('default.rootTag')).toBe('ready');
    expect(store.getNodeStatus('default.rootTag')).toBe('');
    expect(store.getNodeTimestamp('default.rootTag')).toBe(2345);
    expect(store.getNodeValue('default.Device.temp')).toBe(19);
    expect(store.getNodeStatus('default.Device.temp')).toBe('U');
  });

  it('logs and swallows API load errors', async () => {
    const error = new Error('api down');
    apiMock.loadNode.mockRejectedValue(error);

    await store.loadTreeFromAPI('default');

    expect(console.error).toHaveBeenCalledWith('Failed to load tree from API at default:', error);
  });

  it('keeps newer live values when late hydration metadata arrives', async () => {
    apiMock.loadTag.mockResolvedValue({
      config: { type: 1 },
      shared: { units: 'C' },
      status: 'N',
      value: 10,
    });
    seedLeaf(store, 'default.Device.temp', { timestamp: 500, value: 30 });

    await store['loadTagMetadata']('default.Device.temp', 100);

    expect(store.getNodeValue('default.Device.temp')).toBe(30);
    expect(apiMock.loadTag).toHaveBeenCalledWith('default.Device.temp');
  });

  it('logs and swallows tag metadata errors', async () => {
    const error = new Error('tag down');
    apiMock.loadTag.mockRejectedValue(error);

    await store['loadTagMetadata']('default.Device.temp');

    expect(console.error).toHaveBeenCalledWith('Failed to load tag metadata for default.Device.temp:', error);
  });
});

describe('MirrorStore tree subscriptions and live tag broadcasts', () => {
  let store: MirrorStore;
  let nc: ReturnType<typeof createNatsConnection>;
  let kv: ReturnType<typeof createKvStore>;

  beforeEach(async () => {
    store = new MirrorStore();
    nc = createNatsConnection();
    kv = createKvStore();
    natsMock.wsconnect.mockResolvedValue(nc);
    kvMock.create.mockResolvedValue(kv);
    authMock.getCurrentUser.mockReturnValue({ tenant_id: 'default' });
    apiMock.loadTag.mockResolvedValue({ config: {}, shared: {}, status: 'N', value: 1 });
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    vi.spyOn(console, 'log').mockImplementation(() => undefined);
    await store.storeConnectNats('ws://nats.example', 'mirror');
  });

  afterEach(async () => {
    await store.storeDisconnectNats();
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it('notifies exact, ancestor, and root tree subscribers for KV watcher updates', async () => {
    const exact = vi.fn();
    const parent = vi.fn();
    const root = vi.fn();
    store.subscribeToTreeChanges('default.Device.temp', exact);
    store.subscribeToTreeChanges('default.Device', parent);
    const unsubscribeRoot = store.subscribeToTreeChanges('', root);

    store.startKvWatch('default');
    await flushAsyncWork();
    kv.watchers[0].push(kvEntry('default.Device.temp', {
      type: 'leaf',
      value: 44,
      status: 'N',
    }));
    await flushAsyncWork();
    unsubscribeRoot();
    kv.watchers[0].push(kvEntry('default.Device.temp', {
      type: 'leaf',
      value: 45,
      status: 'N',
    }));
    await flushAsyncWork();

    expect(exact).toHaveBeenCalledWith('default.Device.temp', expect.objectContaining({ value: 44 }));
    expect(parent).toHaveBeenCalledWith('default.Device.temp', expect.objectContaining({ value: 44 }));
    expect(root).toHaveBeenCalledTimes(1);
    expect(store.getNodeValue('default.Device.temp')).toBe(45);
  });

  it('sets up live tree subscriptions, applies updates, handles deletes, and logs bad messages', async () => {
    const seen = vi.fn();
    store.subscribeToTreeChanges('default.Device', seen);
    await flushAsyncWork();

    nc.subscriptions['rtdb.tree.default.>'].push(msg('rtdb.tree.default.Device.temp', {
      type: 'leaf',
      config: { type: 1 },
      shared: { units: 'C' },
      status: 'A',
      timestamp: 300,
      value: 32,
    }));
    await flushAsyncWork();

    expect(store.getNodeValue('default.Device.temp')).toBe(32);
    expect(store.getNodeStatus('default.Device.temp')).toBe('A');
    expect(store.getNodeTimestamp('default.Device.temp')).toBe(300);
    expect(seen).toHaveBeenCalledWith('default.Device.temp', expect.objectContaining({ value: 32 }));

    nc.subscriptions['rtdb.tree.default.>'].push(msg('rtdb.tree.default.Device.temp', { deleted: true }));
    await flushAsyncWork();
    expect(store.nodeExists('default.Device.temp')).toBe(false);
    expect(seen).toHaveBeenCalledWith('default.Device.temp', null);

    nc.subscriptions['rtdb.tree.default.>'].push({
      subject: 'rtdb.tree.default.Device.bad',
      string: () => '{bad',
    });
    await flushAsyncWork();
    expect(console.error).toHaveBeenCalledWith('Error handling tree change:', expect.any(SyntaxError));
  });

  it('logs tree subscription setup failures', async () => {
    const error = new Error('subscribe failed');
    nc.subscribe.mockImplementationOnce(() => {
      throw error;
    });

    await store['setupTreeSubscription']();

    expect(console.error).toHaveBeenCalledWith('Failed to setup tree subscription:', error);
  });

  it('applies wanted live tag broadcasts, rounds floats, and ignores unwanted or invalid messages', async () => {
    const seen: any[] = [];
    store.subscribe('default.Device.temp', value => seen.push(value));
    await flushAsyncWork();

    nc.subscriptions['xact.internal.bcast.tagvalue.default.>'].push(msg(
      'xact.internal.bcast.tagvalue.default.Device.temp',
      { temp: { type: 'value', value: 12.345, status: 'N', timestamp: 700 } },
    ));
    nc.subscriptions['xact.internal.bcast.tagvalue.default.>'].push(msg(
      'xact.internal.bcast.tagvalue.default.Device.other',
      { other: { type: 'value', value: 99, status: 'A' } },
    ));
    nc.subscriptions['xact.internal.bcast.tagvalue.default.>'].push({
      subject: 'xact.internal.bcast.tagvalue.default.Device.temp',
      string: () => '{bad',
    });
    nc.subscriptions['xact.internal.bcast.tagvalue.default.>'].push(msg(
      'xact.internal.bcast.tagvalue.default.Device.temp',
      {},
    ));
    await flushAsyncWork();

    expect(store.getNodeValue('default.Device.temp')).toBe(12.35);
    expect(store.getNodeStatus('default.Device.temp')).toBe('N');
    expect(store.getNodeTimestamp('default.Device.temp')).toBe(700);
    expect(store.getNodeValue('default.Device.other')).toBeUndefined();
    expect(seen).toContain(12.35);
  });

  it('does not start duplicate tag-value or KV watchers', async () => {
    store.subscribe('default.Device.temp', () => undefined);
    store.subscribe('default.Device.pressure', () => undefined);
    store.startKvWatch('default');
    store.startKvWatch('default');
    await flushAsyncWork();

    expect(nc.subscribe.mock.calls.filter(call => call[0] === 'xact.internal.bcast.tagvalue.default.>')).toHaveLength(1);
    expect(kv.watch).toHaveBeenCalledTimes(1);
  });

  it('skips live tag watches outside the active org', () => {
    store['watchTagValuePath']('other.Device.temp');

    expect(nc.subscribe).not.toHaveBeenCalledWith('xact.internal.bcast.tagvalue.default.>');
  });

  it('clears the live tag subscription after the async subscription loop errors', async () => {
    const error = new Error('subscription failed');
    const failingSub = {
      unsubscribe: vi.fn(),
      [Symbol.asyncIterator]() {
        return {
          next: vi.fn().mockRejectedValueOnce(error),
        };
      },
    };
    nc.subscribe.mockReturnValueOnce(failingSub as any);

    store['watchTagValuePath']('default.Device.temp');
    await flushAsyncWork();

    expect(store.debugNatsState().tagValueSubscription).toBe(false);
  });
});

describe('getMirrorStore singleton', () => {
  it('returns the same MirrorStore instance', () => {
    expect(getMirrorStore()).toBe(getMirrorStore());
  });
});
