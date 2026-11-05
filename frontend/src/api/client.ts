import type { paths } from './generated/admin';
import { readonly, ref } from 'vue';
const writes = ref(0);
export const pendingWrites = readonly(writes);
export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly reason?: string) { super(message); this.name = 'ApiError'; }
}
export interface RequestOptions { method?: string; body?: unknown; signal?: AbortSignal }
type JSONResponse<T> = T extends { responses: { 200: { content: { 'application/json': infer R } } } } ? R : unknown;
export type GetResponse<P extends keyof paths> = JSONResponse<paths[P] extends { get: infer G } ? G : never>;
export type PostResponse<P extends keyof paths> = JSONResponse<paths[P] extends { post: infer G } ? G : never>;
let expired: (() => void) | undefined;
let disposed = false;
let generation = 0;
const queries = new Map<string, AbortController>();
const reads = new Set<AbortController>();
const scoped = /^\/(?:admin\/api\/(usage|logs|audit|pricing|model-policies|users)|user\/api\/usage)(?:\?|$)/;
export const isAbortError = (error: unknown): boolean => typeof error === 'object' && error !== null && 'name' in error && error.name === 'AbortError';
export function configureClient(onExpired: () => void): void { expired = onExpired; disposed = false; generation++; }
export function cancelPageQueries(): void { for (const controller of queries.values()) controller.abort(); queries.clear(); }
export function disposeClient(): void { disposed = true; cancelPageQueries(); for (const controller of reads) controller.abort(); reads.clear(); expired = undefined; }
export async function api<P extends keyof paths>(path: P, options?: { method?: "GET"; signal?: AbortSignal }): Promise<GetResponse<P>>;
export async function api<P extends keyof paths>(path: P, options: RequestOptions & { method: "POST" }): Promise<PostResponse<P>>;
export async function api<T = unknown>(path: string, options?: RequestOptions): Promise<T>;
export async function api(path: string, options: RequestOptions = {}): Promise<unknown> {
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\')) throw new Error('API path must be same-origin');
  if (disposed) throw new DOMException('Console disposed', 'AbortError');
  const currentGeneration = generation;
  const method = options.method ?? 'GET';
  const writing = method !== 'GET' && method !== 'HEAD';
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  // Only replaceable page reads are cancelled; writes keep their existing semantics.
  const key = path.split('?')[0]!;
  let controller: AbortController | undefined;
  if (method === 'GET' && !options.signal) {
    controller = new AbortController(); reads.add(controller);
    if (scoped.test(path)) {
    queries.get(key)?.abort(); queries.set(key, controller);
    }
  }
  if (writing) writes.value++;
  try {
    const response = await fetch(path, { method, headers, credentials: 'same-origin', signal: options.signal ?? controller?.signal,
      body: options.body === undefined ? undefined : JSON.stringify(options.body) });
    const text = await response.text();
    if (disposed || generation !== currentGeneration) throw new DOMException("Console disposed", "AbortError");
    let data: unknown = null; try { data = JSON.parse(text); } catch { /* Preserve non-JSON error text. */ }
    if (!response.ok) {
      if (response.status === 401 && (path.startsWith('/admin/api/') || path.startsWith('/user/api/'))) expired?.();
      const message = (data as { error?: { message?: unknown } } | null)?.error?.message;
      const reason = (data as { error?: { reason?: unknown } } | null)?.error?.reason;
      throw new ApiError(message ? String(message) : text || `HTTP ${response.status}`, response.status, typeof reason === 'string' ? reason : undefined);
    }
    return data;
  } finally { if (writing) writes.value--; if (controller) reads.delete(controller); if (controller && queries.get(key) === controller) queries.delete(key); }
}
