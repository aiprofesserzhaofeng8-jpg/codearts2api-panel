import { api, isAbortError } from '../../api/client';
import { t } from '../../i18n';
import { listen, cleanup } from '../../shared/lifecycle';
import { refresh } from '../../shared/refresh';
import type { components } from '../../api/generated/admin';
type Account = components['schemas']['Account'];
const labels: Record<string, string> = {adaptive: '自适应', chat_completions: 'Chat Completions', responses: 'Responses', anthropic: 'Anthropic Messages'};
let account: Account | undefined, busy = false;
const element = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
export function openProtocolSettings(value: Account): void {
  account = value;
  element('accountProtocolName').textContent = value.name;
  const select = element<HTMLSelectElement>('accountProtocol');
  select.replaceChildren();
  if (!value.api_protocol) select.add(new Option(t('保持现有路由'), ''));
  for (const option of value.api_protocol_options ?? []) select.add(new Option(t(labels[option] ?? option), option));
  select.value = value.api_protocol ?? '';
  element('accountProtocolError').textContent = '';
  element<HTMLDialogElement>('accountProtocolDialog').showModal();
}
export function mountProtocolSettings(): void {
  const dialog = element<HTMLDialogElement>('accountProtocolDialog');
  const select = element<HTMLSelectElement>('accountProtocol');
  const save = element<HTMLButtonElement>('accountProtocolSave');
  const cancel = element<HTMLButtonElement>('accountProtocolCancel');
  const errorNode = element('accountProtocolError');
  listen(cancel, 'click', () => { if (!busy) dialog.close(); });
  listen(dialog, 'cancel', e => { if (busy) e.preventDefault(); });
  listen(save, 'click', async () => {
    if (!account || busy) return;
    if (!select.value) { dialog.close(); return; }
    const id = account.id;
    busy = true; select.disabled = save.disabled = cancel.disabled = true;
    errorNode.textContent = '';
    try {
      const result = await api('/admin/api/accounts', {method:'POST', body:{action:'protocol',id,api_protocol:select.value}});
      if (!result.ok || result.id !== id || result.api_protocol !== select.value) throw new Error(t('保存结果不一致，请刷新账号列表'));
      dialog.close(); await refresh();
    } catch (error) {
      if (!isAbortError(error)) errorNode.textContent = error instanceof Error ? error.message : String(error);
    } finally { busy = false; select.disabled = save.disabled = cancel.disabled = false; }
  });
  cleanup(() => { account = undefined; dialog.close(); });
}
