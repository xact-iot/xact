export const NATS_SERVER_CHANGED_EVENT = 'xact:nats-server-changed';

let connectedNatsServer = '';

export function setConnectedNatsServer(serverName: string | undefined): void {
  const next = (serverName ?? '').trim();
  if (next === connectedNatsServer) return;
  connectedNatsServer = next;
  window.dispatchEvent(new CustomEvent(NATS_SERVER_CHANGED_EVENT, { detail: next }));
}

export function getConnectedNatsServer(): string {
  return connectedNatsServer;
}
