// Shared prompt API fixture for provider UI smoke tests; never calls an upstream.
const assert = require('node:assert/strict');
const ids = ['codearts','workbuddy','trae','qoder','kiro','chatgpt','claude','teleagent'];
async function installPromptFixture(context, settings) {
  let config = {common: structuredClone(settings.common_prompt), platforms: Object.fromEntries(ids.map(id => {
    const p = settings[id] || {};
    return [id, {prompt_mode:p.prompt_mode || 'passthrough', prompt_text:p.prompt_text || '', prompt_fallback:p.prompt_fallback ?? false, sanitize:p.sanitize ?? false, overrides:p.prompt_overrides ?? null}];
  }))};
  await context.route('**/admin/api/prompts', route => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON();
      assert.deepEqual(body.expected, config);
      config = body.configuration;
    }
    return route.fulfill({json:config});
  });
  return () => config;
}
async function checkPromptOverride(page, getConfig, provider, text) {
  await page.locator('#nav-prompts').click();
  await page.locator('#promptText').waitFor();
  await page.locator('.prompt-scope').nth(ids.indexOf(provider)+1).click();
  const inherit = page.locator('.prompt-editor > fieldset > label.check input').nth(1);
  assert.equal(await inherit.isChecked(), true);
  assert.equal(await page.locator('#promptText').inputValue(), getConfig().common.prompt_text);
  await inherit.uncheck();
  await page.locator('#promptText').fill(text);
  await Promise.all([page.waitForResponse(r=>r.url().endsWith('/prompts')&&r.request().method()==='POST'), page.locator('#savePrompts').click()]);
  await page.waitForFunction(()=>document.querySelector('#savePrompts').disabled);
  assert.equal(getConfig().platforms[provider].overrides.prompt_text, text);
  assert.equal(getConfig().platforms[provider].search_enabled, undefined);
}
module.exports = {installPromptFixture, checkPromptOverride};
