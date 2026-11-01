let refreshSnapshot: (() => Promise<void>) | undefined;
export function registerRefresh(callback?: () => Promise<void>): void { refreshSnapshot = callback; }
export function refresh(): Promise<void> { return refreshSnapshot?.() ?? Promise.resolve(); }
