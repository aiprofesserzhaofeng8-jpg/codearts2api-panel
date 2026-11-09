// View preferences contain no account data. Scheduling remains server-owned.
export const optionalColumns = ['credential', 'reward', 'lastUsed'] as const;
export const sortKeys = ['default', 'name', 'lastUsed', 'expires'] as const;
export type TablePreferences = { hidden: string[]; sort: typeof sortKeys[number] };
const key = 'codearts2api_account_table_v1';
export function readPreferences(): TablePreferences {
  try {
    const v = JSON.parse(localStorage.getItem(key) || '{}');
    return { hidden: Array.isArray(v.hidden) ? v.hidden.filter((s: string) => optionalColumns.includes(s as typeof optionalColumns[number])) : [], sort: sortKeys.includes(v.sort) ? v.sort : 'default' };
  } catch { return { hidden: [], sort: 'default' }; }
}
export function savePreferences(value: TablePreferences): boolean {
  try { localStorage.setItem(key, JSON.stringify(value)); return true; } catch { return false; }
}
export function sortAccounts<T extends { name?: string; last_used?: number; expires_at?: number }>(accounts: T[], sort: TablePreferences['sort']): T[] {
  return [...accounts].sort((a, b) => sort === 'name' ? (a.name || '').localeCompare(b.name || '') : sort === 'lastUsed' ? (b.last_used || 0) - (a.last_used || 0) : sort === 'expires' ? (a.expires_at || Infinity) - (b.expires_at || Infinity) : 0);
}
export type BulkResult = { id: string; name: string; ok: boolean; message: string };
// Snapshot selected IDs before starting; serialize calls, never retry writes.
export async function runSelected<T extends { id: string; name?: string }>(targets: T[], execute: (target: T) => Promise<string>, report: (result: BulkResult) => void, active: () => boolean = () => true): Promise<void> {
  for (const target of targets) {
    if (!active()) break;
    try { report({ id: target.id, name: target.name || target.id, ok: true, message: await execute(target) }); }
    catch (e) {
      report({ id: target.id, name: target.name || target.id, ok: false, message: e instanceof Error ? e.message : String(e) });
      if (e && typeof e === 'object' && (('status' in e && (e.status === 401 || e.status === 403)) || ('name' in e && e.name === 'AbortError'))) break;
    }
  }
}
