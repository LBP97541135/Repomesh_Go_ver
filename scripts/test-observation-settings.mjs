// Uses the opt-in Go browser fixture: real session/CSRF/admin routes and the
// selected workbench. Only model-list requests leave the server; no inference.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const output = process.env.REPOMESH_SETTINGS_TEST_OUTPUT;
const archive = process.env.REPOMESH_SETTINGS_TEST_ARCHIVE;
assert(output && archive, 'explicit private output and workbench archive required');
const manifest = JSON.parse(await fs.readFile(path.join(output, 'manifest.json'), 'utf8'));
const base = manifest.origin;
assert(['127.0.0.1', 'localhost', '[::1]'].includes(new URL(base).hostname));
const original = {
  jev: JSON.parse(await fs.readFile(path.join(archive, 'model.json'), 'utf8')),
  deepseek: JSON.parse(await fs.readFile(path.join(archive, 'assistant.json'), 'utf8')),
};
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ ignoreHTTPSErrors: true, viewport: { width: 1440, height: 1100 } });
const errors = [], outbound = [], settingsResponses = [];
page.on('pageerror', error => errors.push(error.message));
page.on('request', request => {
  if (new URL(request.url()).origin !== base) outbound.push(new URL(request.url()).origin);
});
page.on('response', async response => {
  if (!response.url().includes('/api/settings/observation-models/')) return;
  try {
    const body = await response.text();
    assert(!Object.values(original).some(config => body.includes(config.api_key)), 'settings response exposed a credential');
    settingsResponses.push({ url: new URL(response.url()).pathname, status: response.status() });
  } catch (error) { errors.push(error.message); }
});
try {
  await page.goto(`${base}${manifest.loginPath}?actor=a`);
  const setupResponse = await page.request.get(`${base}/api/setup/status`);
  assert.equal(setupResponse.status(), 404, 'fixture must not claim platform readiness');
  await page.getByRole('heading', { name: '平台启动向导' }).waitFor();
  await page.getByRole('link', { name: '模型与 API 设置', exact: true }).click();
  const results = [];
  for (const [purpose, name] of [['jev', 'Jev'], ['deepseek', 'DeepSeek']]) {
    const form = page.getByRole('form', { name: purpose === 'jev' ? 'Jev · Rubric 评分' : 'DeepSeek · 观测分析' });
    await form.getByRole('button', { name: `保存 ${name} 配置` }).waitFor();
    await form.getByText(`已保存 · ${original[purpose].model}`, { exact: true }).waitFor();
    assert.equal(await form.getByLabel(`${name} API Key`, { exact: true }).inputValue(), '');
    await form.getByLabel(`${name} API Key`, { exact: true }).fill(original[purpose].api_key);
    const save = page.waitForResponse(r => r.url().endsWith(`/api/settings/observation-models/${purpose}`) && r.request().method() === 'POST');
    await form.getByRole('button', { name: `保存 ${name} 配置` }).click();
    assert.equal((await save).status(), 200);
    await form.getByRole('status').getByText('配置已保存，下一次评分或分析使用此模型。连接尚未测试。').waitFor();
    assert.equal(await form.getByLabel(`${name} API Key`, { exact: true }).inputValue(), '');
    const tested = page.waitForResponse(r => r.url().endsWith(`/api/settings/observation-models/${purpose}/test`));
    await form.getByRole('button', { name: '测试连接 / 读取可用模型', exact: true }).click();
    const response = await tested;
    assert.equal(response.status(), 200);
    const result = await response.json();
    assert.equal(result.authentication_ok, true);
    assert(result.models.length > 0);
    await form.getByText(/^供应商返回：/).waitFor();
    results.push({ purpose, model: original[purpose].model, models: result.models, authentication_ok: result.authentication_ok });
  }
  // Exercise model selection and a blank-key update, then restore the exact
  // selected model. This is configuration persistence, not an inference call.
  const jev = page.getByRole('form', { name: 'Jev · Rubric 评分' });
  await jev.getByLabel('Jev 模型', { exact: true }).fill('jev-latest');
  await jev.getByRole('button', { name: '保存 Jev 配置' }).click();
  await jev.getByText('已保存 · jev-latest', { exact: true }).waitFor();
  await page.reload();
  await jev.getByText('已保存 · jev-latest', { exact: true }).waitFor();
  assert.equal(await jev.getByLabel('Jev API Key', { exact: true }).inputValue(), '');
  await jev.getByLabel('Jev 模型', { exact: true }).fill(original.jev.model);
  await jev.getByRole('button', { name: '保存 Jev 配置' }).click();
  await jev.getByText(`已保存 · ${original.jev.model}`, { exact: true }).waitFor();
  await page.reload();
  await jev.getByText(`已保存 · ${original.jev.model}`, { exact: true }).waitFor();
  for (const [purpose, file] of [['jev', 'model.json'], ['deepseek', 'assistant.json']]) {
    const actual = JSON.parse(await fs.readFile(path.join(archive, file), 'utf8'));
    assert.equal(actual.model, original[purpose].model);
    assert.equal(actual.api_key, original[purpose].api_key);
  }
  await page.screenshot({ path: path.join(output, 'model-settings.png'), fullPage: true });
  assert.deepEqual(errors, []);
  assert.deepEqual(outbound, []);
  const summary = { passed: true, setup_endpoint_status: setupResponse.status(), reached_from_unready_wizard: true, scope: 'fixture account; real authenticated settings API, private configuration and provider model lists', results, saved_keys_never_read_back: true, blank_key_preserved: true, selected_model_survives_reload: true, original_configuration_restored: true, errors, external_browser_origins: outbound, settingsResponses };
  await fs.writeFile(path.join(output, 'summary.json'), JSON.stringify(summary, null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify({ passed: true, model_list_providers: results.map(x => x.purpose), key_readback: false, model_reload_and_blank_key: true }));
} finally {
  // Cleanup restores any temporary selection even if a browser assertion fails.
  for (const [purpose, config] of Object.entries(original)) {
    await page.request.post(`${base}/api/settings/observation-models/${purpose}`, {
      headers: { Origin: base, 'X-CSRF-Token': (await page.request.get(`${base}/api/session`).then(r => r.json())).csrfToken },
      data: { model: config.model, api_key: config.api_key },
    });
  }
  await browser.close();
}
