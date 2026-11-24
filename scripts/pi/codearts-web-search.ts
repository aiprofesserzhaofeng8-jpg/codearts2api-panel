// CodeArts gateway external search extension.
import { Type } from 'typebox';
import { Text } from '@earendil-works/pi-tui';
import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';

export default function (pi: ExtensionAPI) {
  pi.on('session_start', (_event, ctx) => {
    const existing = pi.getAllTools().find(tool => tool.name === 'web_search');
    if (existing) {
      ctx.ui.notify('web_search already registered: ' + (existing.sourceInfo?.path ?? existing.name), 'warning');
      return;
    }
    pi.registerTool({
      name: 'web_search',
      label: 'Web search',
      description: 'Search the web using the CodeArts gateway and return source links and excerpts.',
      parameters: Type.Object({
        query: Type.String({ minLength: 1 }),
        max_results: Type.Optional(Type.Integer({ minimum: 1, maximum: 10 })),
        service_id: Type.Optional(Type.String({ description: 'Authorized service ID. Omit to use the gateway default.' })),
      }),
      renderResult(result, { expanded }, theme, context) {
        const raw = result.content.find(part => part.type === 'text')?.text ?? '';
        let rows: { title: string; url: string; snippet: string }[];
        try {
          rows = JSON.parse(raw);
          if (!Array.isArray(rows)) throw new Error();
        } catch {
          return new Text(theme.fg('error', raw), 0, 0);
        }
        const summary = `找到 ${rows.length} 条搜索结果`;
        if (!rows.length) return new Text(summary, 0, 0);
        if (!expanded) {
          return new Text(summary + '  ' + context.keyHint('app.tools.expand', '展开'), 0, 0);
        }
        const entries = rows.map((row, index) =>
          [theme.bold(`${index + 1}. ${row.title || row.url}`), theme.fg('accent', row.url), row.snippet]
            .filter(Boolean).join('\n'));
        return new Text([summary, ...entries].join('\n\n'), 0, 0);
      },
      async execute(_id, params, signal, _update, context) {
        const model = context.model;
        if (!model || !['codearts', 'codearts-local'].includes(model.provider)) {
          throw new Error('Select a CodeArts gateway model to use web_search.');
        }
        const base = new URL(model.baseUrl);
        if (!['https:', 'http:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) {
          throw new Error('Invalid gateway URL.');
        }
        const key = await context.modelRegistry.getApiKeyForProvider(model.provider);
        if (!key) throw new Error('Gateway API key unavailable.');
        const response = await fetch(base.href.replace(/\/$/, '') + '/search', {
          method: 'POST', headers: { Authorization: 'Bearer ' + key, 'Content-Type': 'application/json' },
          body: JSON.stringify(params), signal, redirect: 'error',
        });
        if (!response.ok) {
          const data = await response.json().catch(() => ({}));
          throw new Error('Web search (HTTP ' + response.status + '): ' + (data.error?.message ?? response.statusText));
        }
        const result = await response.json();
        if (!Array.isArray(result.results)) throw new Error('Invalid search response.');
        return { content: [{ type: 'text', text: JSON.stringify(result.results) }], details: { request_id: result.request_id, service_id: result.service_id } };
      },
    });
  });
}
