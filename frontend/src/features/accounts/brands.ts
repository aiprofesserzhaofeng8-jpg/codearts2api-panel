// Display metadata only. Provider IDs and credential interpretation remain server-owned.
const names: Record<string, string> = {
  chatgpt: 'OpenAI', claude: 'Claude', gemini: 'Gemini', deepseek: 'DeepSeek',
  zhipu: 'Zhipu', kimi: 'Kimi', minimax: 'MiniMax', grok: 'Grok',
  codearts: 'CodeArts', workbuddy: 'WorkBuddy', trae: 'Trae', antigravity: 'Antigravity',
  qoder: 'Qoder', kiro: 'Kiro', mobileclaw: 'MobileClaw', teleagent: 'TeleAgent',
  opencode: 'OpenCode', commandcode: 'Command Code', cline: 'Cline', typesafe: 'TypeSafe',
  bedrock: 'AWS Bedrock', vertex: 'Vertex AI',
};
export const brands = Object.entries(names).map(([id, name]) => ({id, name}));
export const commonPlatforms = brands.slice(0, 11);
export const extraPlatforms = brands.slice(11, 20);
export const brandName = (id: string) => names[id] || id;
const icons = import.meta.glob('../../assets/providers/*.{svg,png}', {eager: true, query: '?url', import: 'default'}) as Record<string, string>;
export function brandIcon(id: string) {
  const name = ({chatgpt: 'openai'} as Record<string, string>)[id] || id;
  return icons[`../../assets/providers/${name}.svg`] || icons[`../../assets/providers/${name}.png`] || icons['../../assets/providers/product.svg'];
}
export interface AccountType {
  id: string; label: string; provider: string; methods: string[];
  key?: boolean; field?: string; value?: string;
}
const type = (provider: string, id: string, label: string, methods: string[], extra: Partial<AccountType> = {}): AccountType => ({provider, id, label, methods, ...extra});
export function accountTypes(platform: string): AccountType[] {
  if (platform === 'chatgpt' || platform === 'claude') return [
    type(platform, 'oauth', platform === 'chatgpt' ? 'ChatGPT / Codex' : 'Claude 订阅', ['browser', 'import']),
    type(platform, 'api', platform === 'chatgpt' ? 'OpenAI API' : 'Claude API', ['import'], {key: true}),
    ...(platform === 'claude' ? [type('bedrock', 'bedrock', 'AWS Bedrock', ['import'])] : []),
  ];
  if (platform === 'gemini') return [
    type(platform, 'api', 'AI Studio API', ['import'], {key: true, field: 'geminiImportMode', value: 'api'}),
    type(platform, 'cli', 'CLI / Code Assist', ['browser', 'import'], {field: 'geminiImportMode', value: 'cli'}),
    type('vertex', 'vertex', 'Vertex AI', ['import']),
  ];
  if (['zhipu', 'kimi', 'minimax'].includes(platform)) return [
    type(platform, 'payg', '按量 API', ['import'], {key: true, field: 'codingImportMode', value: 'payg'}),
    type(platform, 'coding', platform === 'minimax' ? 'M Plan' : 'Coding Plan', ['import'], {key: true, field: 'codingImportMode', value: 'coding'}),
  ];
  if (platform === 'opencode') return ['zen', 'go'].map(value => type(platform, value, value === 'zen' ? 'Zen' : 'Go', ['import'], {key: true, field: 'opencodeImportScope', value}));
  if (platform === 'commandcode') return [type(platform, 'subscription', 'GOAT / Pro / Max / Team', ['import'], {key: true, field: 'commandcodeImportMode', value: 'subscription'}), type(platform, 'payg', 'Provider / Pay as you go', ['import'], {key: true, field: 'commandcodeImportMode', value: 'payg'})];
  if (['deepseek', 'cline', 'typesafe'].includes(platform)) return [type(platform, 'api', platform === 'typesafe' ? '决策接口' : 'API', ['import'], {key: true})];
  return [type(platform, 'default', brandName(platform), ['browser', 'import'])];
}

// Soft presentation backgrounds complement original brand artwork in both themes.
const iconTints: Record<string, string> = {
  chatgpt: '#e5f4ed', claude: '#f8e8df', gemini: '#edf0ff', deepseek: '#e7efff',
  zhipu: '#e9efff', kimi: '#e8f6ed', minimax: '#ffe8ef', grok: '#eef0f3',
  codearts: '#f0edff', workbuddy: '#fff0dd', trae: '#dcf8ed', antigravity: '#eef0ff',
  qoder: '#e3f5ed', kiro: '#f0e7ff', mobileclaw: '#eee8ff', teleagent: '#e5efff',
  opencode: '#edf0f4', commandcode: '#f4eee6', cline: '#e7f1f5', typesafe: '#fce6f4',
};
export const brandIconStyle = (id: string) => ({'--brand-tint': iconTints[id] || '#eef0f3'});
