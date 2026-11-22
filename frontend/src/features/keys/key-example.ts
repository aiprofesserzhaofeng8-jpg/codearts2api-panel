export type Client = 'cURL' | 'Codex' | 'Claude Code';
const quote = (value:string) => "'" + value.replaceAll("'", "'\\''") + "'";

export function keyExample(client:Client, origin:string, model:string):string {
  const base = new URL(origin).origin;
  if (client === 'cURL') {
    const body = JSON.stringify({model,messages:[{role:'user',content:'Hello'}]});
    return ['curl '+quote(base+'/v1/chat/completions'), '-H "Authorization: Bearer $CODEARTS_API_KEY"', "-H 'Content-Type: application/json'", '--data '+quote(body)].join(' \\\n  ');
  }
  if (client === 'Codex') {
    const settings = {model_provider:'codearts_gateway',model,
      'model_providers.codearts_gateway.name':'CodeArts gateway',
      'model_providers.codearts_gateway.base_url':base+'/v1',
      'model_providers.codearts_gateway.env_key':'CODEARTS_API_KEY',
      'model_providers.codearts_gateway.wire_api':'responses',
      'model_providers.codearts_gateway.requires_openai_auth':false,
      'model_providers.codearts_gateway.supports_websockets':false};
    return ['codex', ...Object.entries(settings).map(([k,v])=>'-c '+quote(k+'='+JSON.stringify(v)))].join(' \\\n  ');
  }
  return ['ANTHROPIC_BASE_URL='+quote(base),'ANTHROPIC_AUTH_TOKEN="$CODEARTS_API_KEY"','ANTHROPIC_MODEL='+quote(model),'claude --model '+quote(model)].join(' \\\n  ');
}
