export function formatUnixMillis(value: unknown, timeZone?: string): string | null {
  if (value === undefined || value === null || value === '') return null;

  const ms = typeof value === 'bigint' ? Number(value) : Number(String(value).trim());
  if (!Number.isFinite(ms)) return null;

  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return null;

  try {
    return d.toLocaleString('sv-SE', timeZone ? { timeZone } : undefined).replace('T', ' ');
  } catch {
    return d.toLocaleString('sv-SE').replace('T', ' ');
  }
}
