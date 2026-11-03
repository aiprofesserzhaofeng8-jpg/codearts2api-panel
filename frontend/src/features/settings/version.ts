import { t as tr, dateLocale } from '../../i18n';
import { api, isAbortError } from '../../api/client';
import { listen } from '../../shared/lifecycle';
interface VersionStatus { current: string; latest?: string; state: string; url: string; checked_at?: string; message: string }
export function mountVersion(): void {
  const button = document.querySelector<HTMLButtonElement>('#checkVersion');
  const current = document.querySelector<HTMLElement>('#runningVersion');
  const message = document.querySelector<HTMLElement>('#versionMessage');
  const link = document.querySelector<HTMLAnchorElement>('#releaseLink');
  if (!button || !current || !message || !link) return;
  const load = async (check: boolean) => {
    button.disabled = true;
    message.textContent = check ? tr("正在检查更新…") : tr("正在读取运行版本…");
    try {
      const info = await api<VersionStatus>('/admin/api/version' + (check ? '?check=1' : ''));
      current.textContent = info.current || tr("未知");
      message.textContent = (info.latest ? (tr("最新正式版 ") + (info.latest) + " · ") : '') + info.message + (info.checked_at ? `（${new Date(info.checked_at).toLocaleString(dateLocale)}）` : '');
      // Only link to this project's release pages, never a server-supplied executable URL.
      const base = 'https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases';
      link.href = info.url === base || new RegExp('^' + base.replaceAll('.', '\\.') + '/tag/v[0-9]+\\.[0-9]+\\.[0-9]+$').test(info.url) ? info.url : base;
    } catch (error) {
      if (!isAbortError(error)) message.textContent = tr("无法读取版本信息，请稍后重试");
    } finally { if (button.isConnected) button.disabled = false; }
  };
  listen(button, 'click', () => { void load(true); });
  void load(false);
}
