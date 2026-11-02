export const credits = (units: number) => (units / 100000000).toFixed(8).replace(/\.?0+$/, '') || '0';
export const consumerDate = (value: number) => value ? new Date(value * 1000).toLocaleString() : '—';
