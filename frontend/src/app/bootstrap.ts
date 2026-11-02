import { beginLifetime, endLifetime } from '../shared/lifecycle';
import { registerRefresh } from '../shared/refresh';
import { mountDom, disposeToast } from '../shared/dom';
import { mountSession } from './session';
import { refresh } from './snapshot';
import { mountAccounts } from '../features/accounts/controller';
import { mountProviders } from '../features/accounts/providers/controller';
import { mountCodearts } from '../features/accounts/providers/codearts';
import { mountCredits } from '../features/accounts/credits';
import { mountWelfare } from '../features/jobs/welfare';
import { mountUsage } from '../features/usage/controller';
import { mountModels } from '../features/models/controller';
import { mountPricing } from '../features/models/pricing';
import { mountPolicies } from '../features/models/policies';
import { mountKeys } from '../features/keys/controller';
import { mountLogs } from '../features/logs/controller';
import { mountSettings } from '../features/settings/controller';
import { mountGroups } from '../features/groups/controller';
import { mountRoutes } from './routes';
export function startConsole(): () => void {
  beginLifetime(); registerRefresh(refresh);
  [mountDom, mountAccounts, mountProviders, mountCodearts, mountCredits, mountWelfare, mountUsage, mountModels, mountPricing,
    mountPolicies, mountKeys, mountLogs, mountSettings, mountGroups, mountSession, mountRoutes].forEach(mount => mount());
  return () => { registerRefresh(); endLifetime(); disposeToast(); };
}
