// User-visible navigation shared by provider smoke tests.
async function selectPlatform(page, provider) {
  const brand = {bedrock: 'claude', vertex: 'gemini'}[provider] || provider;
  const button = page.locator(`[data-account-platform="${brand}"]`);
  if (!await button.isVisible()) await page.locator('#accountPlatformsMore').click();
  await button.click();
  if (brand !== provider) await page.locator(`[data-account-type="${provider}"]`).click();
}
module.exports = {selectPlatform};
