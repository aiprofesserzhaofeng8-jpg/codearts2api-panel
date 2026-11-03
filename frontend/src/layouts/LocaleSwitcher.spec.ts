import { mount } from '@vue/test-utils';
import { afterEach, expect, it, vi } from 'vitest';
import LocaleSwitcher from './LocaleSwitcher.vue';
import { api, configureClient, disposeClient, pendingWrites } from '../api/client';

afterEach(() => { disposeClient(); vi.unstubAllGlobals(); document.body.textContent = ''; });
it('supports active selection, arrow keys, Escape focus return and outside dismissal', async () => {
  const wrapper = mount(LocaleSwitcher, {attachTo: document.body});
  const trigger = wrapper.get('.locale-trigger');
  expect(trigger.attributes('aria-expanded')).toBe('false');
  await trigger.trigger('click');
  expect(wrapper.get('[aria-checked="true"]').text()).toContain('跟随系统');
  expect(document.activeElement).toBe(wrapper.get('[aria-checked="true"]').element);
  await wrapper.get('[aria-checked="true"]').trigger('keydown', {key: 'ArrowDown'});
  expect(document.activeElement?.getAttribute('lang')).toBe('zh');
  await wrapper.get('[lang="zh"]').trigger('keydown', {key: 'End'});
  expect(document.activeElement?.getAttribute('lang')).toBe('en');
  await wrapper.get('[lang="en"]').trigger('keydown', {key: 'Escape'});
  expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  expect(document.activeElement).toBe(trigger.element);
  await trigger.trigger('click');
  document.body.dispatchEvent(new Event('pointerdown', {bubbles: true}));
  await wrapper.vm.$nextTick(); expect(wrapper.find('[role="menu"]').exists()).toBe(false);
  wrapper.unmount();
});

it('prevents a language reload while a submitted write is still pending', async () => {
  configureClient(vi.fn());
  let finish!: (response: Response) => void;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(resolve => { finish = resolve; })));
  const wrapper = mount(LocaleSwitcher, {attachTo: document.body});
  await wrapper.get('.locale-trigger').trigger('click');
  const write = api('/admin/api/groups', {method: 'POST', body: {name: 'fixture'}});
  await wrapper.vm.$nextTick();
  expect(pendingWrites.value).toBe(1);
  expect(wrapper.get('.locale-trigger').attributes('disabled')).toBeDefined();
  for (const option of wrapper.findAll('[role="menuitemradio"]')) expect(option.attributes('disabled')).toBeDefined();
  finish(new Response('{"ok":true}'));
  await write; await wrapper.vm.$nextTick();
  expect(pendingWrites.value).toBe(0);
  expect(wrapper.get('.locale-trigger').attributes('disabled')).toBeUndefined();
  wrapper.unmount();
});
