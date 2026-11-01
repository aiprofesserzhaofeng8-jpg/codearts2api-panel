// @vitest-environment jsdom
import { it, expect, vi } from 'vitest';
import { btn, cell, emptyRow, on } from './controls';
import { beginLifetime, endLifetime } from '../shared/lifecycle';
it('renders untrusted values as text and preserves table spans', () => {
  const input='<img src=x onerror="window.injected=true">';const element=cell(input);
  expect(element.textContent).toBe(input);expect(element.querySelector('img')).toBeNull();expect(emptyRow(7,input).cells[0]!.colSpan).toBe(7);
});
it('prevents duplicate submissions and re-enables after a failed operation', async () => {
  let reject!: (reason:Error)=>void;const action=vi.fn(()=>new Promise((_resolve,r)=>{reject=r;}));const button=btn('save','primary',action);
  document.body.append(button);button.click();button.click();expect(action).toHaveBeenCalledTimes(1);expect(button.disabled).toBe(true);
  reject(new Error('fixture'));await new Promise(resolve=>setTimeout(resolve,0));expect(button.disabled).toBe(false);button.remove();
});
it('removes static event bindings on unmount and supports one fresh binding on remount', () => {
  document.body.innerHTML='<button id="fixture"></button>';const action=vi.fn();const button=document.querySelector<HTMLButtonElement>('#fixture')!;
  beginLifetime();on('#fixture',action);button.click();endLifetime();button.click();expect(action).toHaveBeenCalledTimes(1);
  beginLifetime();on('#fixture',action);button.click();endLifetime();expect(action).toHaveBeenCalledTimes(2);
});
