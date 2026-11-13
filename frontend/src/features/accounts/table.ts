// View preferences contain no account data. Scheduling remains server-owned.
export const optionalColumns = ['platform', 'capacity', 'status', 'quota', 'credential', 'reward', 'lastUsed'] as const;
export const pageSizes = [10, 20, 50, 100] as const;
export const sortKeys = ['default', 'name', 'lastUsed', 'expires'] as const;
export type TablePreferences = { hidden: string[]; sort: typeof sortKeys[number]; pageSize?: number };
const key = 'codearts2api_account_table_v1';
export function readPreferences(): TablePreferences {
  try {
    const v = JSON.parse(localStorage.getItem(key) || '{}');
    return { hidden: Array.isArray(v.hidden) ? v.hidden.filter((s: string) => optionalColumns.includes(s as typeof optionalColumns[number])) : [], sort: sortKeys.includes(v.sort) ? v.sort : 'default', pageSize: pageSizes.includes(v.pageSize) ? v.pageSize : 20 };
  } catch { return { hidden: [], sort: 'default', pageSize: 20 }; }
}
export function savePreferences(value: TablePreferences): boolean {
  try { localStorage.setItem(key, JSON.stringify(value)); return true; } catch { return false; }
}
export function sortAccounts<T extends { name?: string; last_used?: number; expires_at?: number }>(accounts: T[], sort: TablePreferences['sort']): T[] {
  return [...accounts].sort((a, b) => sort === 'name' ? (a.name || '').localeCompare(b.name || '') : sort === 'lastUsed' ? (b.last_used || 0) - (a.last_used || 0) : sort === 'expires' ? (a.expires_at || Infinity) - (b.expires_at || Infinity) : 0);
}
// Windowed page links avoid rendering one button per page for large account pools.
export function paginateAccounts<T>(accounts: T[], requestedPage: number, requestedSize = 20) {
  const size = pageSizes.includes(requestedSize as typeof pageSizes[number]) ? requestedSize : 20;
  const pages = Math.max(1, Math.ceil(accounts.length / size));
  const page = Math.min(pages, Math.max(1, Number.isFinite(requestedPage) ? Math.floor(requestedPage) : 1));
  const start = (page - 1) * size;
  const numbers = pages <= 7 ? Array.from({length: pages}, (_, i) => i + 1) : [...new Set([1, pages, page - 1, page, page + 1, ...(page < 4 ? [2, 3, 4] : []), ...(page > pages - 3 ? [pages - 3, pages - 2, pages - 1] : [])])].filter(n => n >= 1 && n <= pages).sort((a, b) => a - b);
  const links: (number | '…')[] = [];
  numbers.forEach((n, i) => { if (i && n - numbers[i - 1] > 1) links.push('…'); links.push(n); });
  return { page, pages, from: accounts.length ? start + 1 : 0, to: Math.min(start + size, accounts.length), total: accounts.length, items: accounts.slice(start, start + size), links };
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
