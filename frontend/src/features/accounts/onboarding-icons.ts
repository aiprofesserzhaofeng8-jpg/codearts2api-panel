import browser from '../../assets/onboarding/browser.svg';
import apiKey from '../../assets/onboarding/api-key.svg';
import credentialImport from '../../assets/onboarding/import.svg';
export type OnboardingIcon = 'browser' | 'api' | 'import';
export const onboardingIcons: Record<OnboardingIcon, string> = {browser, api: apiKey, import: credentialImport};
export function onboardingIcon(kind: OnboardingIcon): HTMLSpanElement {
  const tile = document.createElement('span');
  tile.className = `method-symbol method-symbol-${kind}`;
  tile.setAttribute('aria-hidden', 'true');
  const image = document.createElement('img'); image.src = onboardingIcons[kind]; image.alt = '';
  tile.append(image); return tile;
}
