import { shallowRef } from 'vue';
import type { components } from '../api/generated/admin';

export type Principal = components['schemas']['User'] & {role: 'admin' | 'user'};
export const principal = shallowRef<Principal>();
export const loginNotice = shallowRef('');

export function setPrincipal(value: Principal): void {
  if (value.role !== 'admin' && value.role !== 'user') throw new Error('登录身份无效');
  principal.value = value;
  loginNotice.value = '';
}
export function expireSession(notice = ''): void {
  principal.value = undefined;
  loginNotice.value = notice;
}
