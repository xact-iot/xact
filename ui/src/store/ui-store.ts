/**
 * UiStore - client-side session state for global UI variables.
 *
 * Intentionally separate from MirrorStore so that set() can be called freely
 * by widgets and controls without polluting the RTDB mirror or bypassing the
 * org sandbox.  Values are in-memory only; they reset on page reload.
 *
 * Variables
 * ---------
 *   orgName     - The active organisation name. Initialised from the JWT
 *                 tenant_id on login; changeable when switching org context.
 *   deviceType  - The currently selected device type (empty when unset).
 *   deviceName  - The currently selected device name (empty when unset).
 *   timeStart   - Start of the active time range as a Unix ms timestamp
 *                 (null when unset - widgets should fall back to a default
 *                 period such as the previous 24 h).
 *   timeEnd     - End of the active time range as a Unix ms timestamp
 *                 (null when unset - widgets should fall back to Date.now()).
 *
 * Usage
 * -----
 *   import { getUiStore } from './store/ui-store';
 *
 *   // Read current value
 *   const org = getUiStore().get('orgName');
 *
 *   // Write (fires all subscribers synchronously)
 *   getUiStore().set('timeStart', Date.now() - 86_400_000);
 *
 *   // Subscribe - callback fires immediately if value is already set,
 *   // then on every subsequent change.  Returns an unsubscribe function.
 *   const unsub = getUiStore().subscribe('timeStart', (v) => console.log(v));
 *   unsub(); // cancel
 */

// ── Types ──────────────────────────────────────────────────────────────────────

export interface UiVars {
  /** Active organisation name, e.g. "default" */
  orgName: string;
  /** Currently selected device type, e.g. "pump". Empty string = unset. */
  deviceType: string;
  /** Currently selected device name, e.g. "PMP001". Empty string = unset. */
  deviceName: string;
  /**
   * Start of active time range - Unix millisecond timestamp.
   * null = unset; widgets should fall back to (now − their default period).
   */
  timeStart: number | null;
  /**
   * End of active time range - Unix millisecond timestamp.
   * null = unset; widgets should fall back to Date.now().
   */
  timeEnd: number | null;
  /** IANA timezone of the server, e.g. "America/New_York". Fetched from /health on startup. */
  serverTimezone: string;
}

export type UiKey = keyof UiVars;

type Callback<K extends UiKey> = (value: UiVars[K]) => void;

// ── Store class ────────────────────────────────────────────────────────────────

class UiStore {
  private _values: UiVars = {
    orgName:    '',
    deviceType: '',
    deviceName: '',
    timeStart:      null,
    timeEnd:        null,
    serverTimezone: '',
  };

  private _subs = new Map<UiKey, Set<Callback<any>>>();

  // ── Read ───────────────────────────────────────────────────────────────────

  get<K extends UiKey>(key: K): UiVars[K] {
    return this._values[key];
  }

  // ── Write ──────────────────────────────────────────────────────────────────

  set<K extends UiKey>(key: K, value: UiVars[K]): void {
    this._values[key] = value;
    this._notify(key);
  }

  // ── Subscribe ──────────────────────────────────────────────────────────────

  /**
   * Subscribe to a UI variable.  The callback is fired immediately with the
   * current value if it is already set (non-empty string or non-null number),
   * then on every subsequent change.
   *
   * Returns an unsubscribe function.
   */
  subscribe<K extends UiKey>(key: K, callback: Callback<K>): () => void {
    if (!this._subs.has(key)) this._subs.set(key, new Set());
    this._subs.get(key)!.add(callback as Callback<any>);

    // Fire immediately if a meaningful value is already present
    const current = this._values[key];
    if (current !== null && current !== '') {
      callback(current);
    }

    return () => {
      this._subs.get(key)?.delete(callback as Callback<any>);
    };
  }

  // ── Internal ───────────────────────────────────────────────────────────────

  private _notify<K extends UiKey>(key: K): void {
    const value = this._values[key];
    this._subs.get(key)?.forEach(cb => cb(value));
  }
}

// ── Singleton ──────────────────────────────────────────────────────────────────

let _instance: UiStore | null = null;

export function getUiStore(): UiStore {
  if (!_instance) _instance = new UiStore();
  return _instance;
}
