let lifetime = new AbortController();
const cleanups: Array<() => void> = [];
export function beginLifetime(): void { lifetime.abort(); lifetime = new AbortController(); }
export function listen(target: EventTarget, type: string, callback: EventListenerOrEventListenerObject, options?: AddEventListenerOptions | boolean): void {
  target.addEventListener(type, callback, { ...(typeof options === 'boolean' ? { capture: options } : options), signal: lifetime.signal });
}
export function cleanup(callback: () => void): void { cleanups.push(callback); }
export function endLifetime(): void { lifetime.abort(); for (const callback of cleanups.splice(0)) callback(); }
export function every(callback: () => void, ms: number): void { const timer = setInterval(callback, ms); cleanup(() => clearInterval(timer)); }
