import { t } from '../../i18n';
const labels: Record<string,string> = {model:'模型定价',reference:'参考定价',default:'全站默认价',mixed:'多种定价'};
export function priceKind(kind: unknown) {
  return typeof kind === 'string' && labels[kind] ? t(labels[kind]!) : '';
}
