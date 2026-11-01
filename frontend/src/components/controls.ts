import { listen } from '../shared/lifecycle';
export function mk<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string | null, text?: unknown): HTMLElementTagNameMap[K];
export function mk(tag: string, cls?: string | null, text?: unknown): HTMLElement;
export function mk(tag: string, cls?: string | null, text?: unknown): HTMLElement {
  const element = document.createElement(tag);
  if (cls) element.className = cls;
  if (text !== undefined && text !== null) element.textContent = String(text);
  return element;
}
export const cell = (text?: unknown, cls?: string): HTMLTableCellElement => mk('td', cls, text);
export const row = (cells: Node[]): HTMLTableRowElement => { const element = mk('tr'); element.append(...cells); return element; };
export const pill = (text: unknown, cls?: string): HTMLSpanElement => mk('span', 'badge '+(cls ?? ''), text);
export function emptyRow(columns: number, text: unknown): HTMLTableRowElement { const element = cell(text, 'empty'); element.colSpan = columns; return row([element]); }
type BusyElement = HTMLElement & { disabled?: boolean };
type Action = (this: HTMLElement, event: Event) => unknown;
export function busyable(element: BusyElement, action: Action): (this: HTMLElement, event: Event) => unknown {
  return function(event) {
    if (element.disabled) return;
    const result = action.call(this, event);
    if (result && typeof (result as PromiseLike<unknown>).then === 'function') {
      element.disabled = true; element.setAttribute('aria-busy', 'true');
      Promise.resolve(result).catch(() => {}).then(() => { element.disabled = false; element.removeAttribute('aria-busy'); });
    }
    return result;
  };
}
export function btn(label: unknown, cls: string | null, action: Action): HTMLButtonElement {
  const element = mk('button', 'btn '+(cls ?? ''), label); element.type = 'button'; const guarded = busyable(element, action); element.onclick = event => guarded.call(element, event); return element;
}
export function on(selector: string, action: Action): HTMLElement {
  const element = document.querySelector<BusyElement>(selector);
  if (!element) throw new Error('Missing control: '+selector);
  listen(element, 'click', busyable(element, action)); return element;
}
