export const userNumber = (value: unknown) => Number(value ?? 0).toLocaleString();
export const userUSD = (value: unknown) => '$' + Number(value ?? 0).toFixed(6);
export const userDate = (time: number) => time ? new Date(time * 1000).toLocaleString() : '—';
