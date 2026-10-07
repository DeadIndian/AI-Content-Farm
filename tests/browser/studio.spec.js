const { test, expect } = require('@playwright/test');
const { execFileSync } = require('node:child_process');
const { mkdtempSync, rmSync } = require('node:fs');
const { tmpdir } = require('node:os');
const path = require('node:path');

const mediaTests = process.env.STUDIO_RUN_MEDIA_TESTS === '1';
const selectMode = (page, name, value) => page.locator('label').filter({ has: page.locator(`input[name="${name}"][value="${value}"]`) }).click();
const originalPair = {id: 'cog-axiom', name: 'Cog & Axiom', speakers: ['Cog', 'Axiom'], available: true};
const cloudCapabilities = {
  planner: {ready: true, ai_configured: true, gemini_configured: true, engine: 'gemini', langgraph: true},
  renderer: {ready: true, resource_mode: 'gentle', default_voice_provider: 'gemini', allow_local_models: false,
    voice_providers: [
      {id: 'gemini', name: 'Gemini cloud voice', available: true},
      {id: 'espeak', name: 'Lightweight demo voice', available: true},
      {id: 'piper', name: 'Piper', available: true},
    ]},
  pairs: [originalPair], resource_mode: 'gentle',
  shorts: {provider: 'gemini', cloud_ready: true, local_models_allowed: false}, limitations: [],
};
async function mockCapabilities(page, getCaps = () => cloudCapabilities) {
  await page.route('**/api/studio/capabilities', route => route.fulfill({json: getCaps()}));
}
async function manualDraft(page) {
  await page.goto('/');
  await expect(page.locator('#connection-label')).not.toHaveText('Connecting');
  await selectMode(page, 'provider', 'manual');
  await page.locator('#topic').fill('A conversation about careful testing');
  await page.locator('#source-notes').fill('Why do we review the whole workflow?\n\nBecause a beautiful button is only useful when the video actually plays.');
  await page.locator('#generate-draft').click();
  await expect(page.locator('#review-section')).toBeVisible();
}

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
  test(`studio navigation and layout at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport);
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('/');
    await expect(page.locator('#create-heading')).toBeVisible();
    await expect(page.locator('#connection-label')).not.toHaveText('Connecting');
    for (const view of ['shorts', 'projects', 'cast', 'settings', 'create']) {
      await page.locator(`nav [data-view="${view}"], .sidebar-bottom [data-view="${view}"]`).first().click();
      await expect(page.locator(`#view-${view}`)).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBeTruthy();
    }
    expect(errors).toEqual([]);
  });
}

test('cloud-ready workspaces default to Gemini, original robots, and gentle previews', async ({ page }) => {
  await mockCapabilities(page);
  await page.goto('/');
  await expect(page.locator('#connection-label')).toHaveText('Studio ready');
  await expect(page.locator('input[name="provider"][value="ai"]')).toBeChecked();
  await expect(page.locator('#pair')).toHaveValue('cog-axiom');
  await expect(page.locator('#voice-provider')).toHaveValue('gemini');
  await expect(page.locator('#voice-provider option[value="espeak"]')).toHaveCount(1);
  await expect(page.locator('#voice-provider option[value="piper"]')).toHaveCount(0);
  await expect(page.locator('#render-quality')).toHaveValue('preview');
  await page.locator('.sidebar-bottom [data-view="settings"]').click();
  await expect(page.locator('#resource-description')).toContainText('Gentle mode');
  await expect(page.locator('#resource-description')).toContainText('no local AI models');
  await page.locator('nav [data-view="shorts"]').click();
  await expect(page.locator('#shorts-cloud-help')).toContainText('Gemini cloud transcription');
});

test('manual draft edits and voice choice survive polling and reload without encoding', async ({ page }) => {
  await manualDraft(page);
  const dialogue = page.locator('#scene-text-0');
  await dialogue.fill('Does this studio keep the words I write?');
  await dialogue.focus();
  await page.waitForResponse(response => response.url().endsWith('/api/studio/jobs') && response.status() === 200);
  await expect(dialogue).toBeFocused();
  await expect(dialogue).toHaveValue('Does this studio keep the words I write?');
  await page.locator('#voice-provider').selectOption('espeak');
  await page.reload();
  await expect(page.locator('#scene-text-0')).toHaveValue('Does this studio keep the words I write?');
  await expect(page.locator('#voice-provider')).toHaveValue('espeak');
  await expect(page.locator('#draft-trace li')).not.toHaveCount(0);
});

test('cast upload preserves inputs on backend rejection and succeeds on retry', async ({ page }) => {
  const custom = {id: 'browser-cast', name: 'The test bench', speakers: ['Byte', 'Bit'], available: true,
    sprite_urls: ['/api/studio/presenters/cog-idle.png', '/api/studio/presenters/axiom-idle.png'],
    presenter_animation: {Byte: 'audio-reactive-still', Bit: 'audio-reactive-still'}};
  let imported = false, attempts = 0;
  await mockCapabilities(page, () => ({...cloudCapabilities, pairs: imported ? [originalPair, custom] : [originalPair]}));
  await page.route('**/api/studio/cast', async route => {
    const body = route.request().postDataBuffer().toString('latin1');
    expect(body).toContain('name="idle_a"');
    expect(body).toContain('name="idle_b"');
    expect(body).toContain('name="speaker_a"');
    expect(body).not.toContain('name="talk_a"');
    if (++attempts === 1) return route.fulfill({status: 422, json: {error: 'The second portrait needs a valid transparent PNG.'}});
    imported = true;
    return route.fulfill({status: 201, json: custom});
  });
  await page.goto('/#cast');
  await page.locator('#cast-name').fill('The test bench');
  await page.locator('#speaker-a').fill('Byte');
  await page.locator('#speaker-b').fill('Bit');
  await page.locator('#role-a').fill('Curious interviewer');
  await page.locator('#role-b').fill('Patient explainer');
  await page.locator('#idle-a').setInputFiles(path.resolve('assets/presenters/cog-idle.png'));
  await page.locator('#idle-b').setInputFiles(path.resolve('assets/presenters/axiom-idle.png'));
  await expect(page.locator('#cast-image-a')).toBeVisible();
  await page.locator('#import-cast').click();
  await expect(page.locator('#cast-status')).toContainText('valid transparent PNG');
  await expect(page.locator('#cast-name')).toHaveValue('The test bench');
  await expect(page.locator('#import-cast')).toBeEnabled();
  await page.locator('#import-cast').click();
  await expect(page.locator('#cast-status')).toContainText('ready and selected');
  await page.locator('nav [data-view="create"]').click();
  await expect(page.locator('#pair')).toHaveValue('browser-cast');
  await expect(page.locator('#preview-cast')).toHaveText('BYTE + BIT');
});

test('backend planning errors are visible and retryable', async ({ page }) => {
  await page.route('**/api/studio/drafts', route => route.fulfill({status: 502, json: {error: 'Gemini is temporarily unavailable. Please retry.'}}));
  await page.goto('/');
  await expect(page.locator('#connection-label')).not.toHaveText('Connecting');
  await page.locator('#generate-draft').click();
  await expect(page.locator('#draft-status')).toContainText('temporarily unavailable');
  await expect(page.locator('#generate-draft')).toBeEnabled();
});

test('render a playable presenter video (opt-in media)', async ({ page, request }) => {
  test.skip(!mediaTests, 'Set STUDIO_RUN_MEDIA_TESTS=1 on a machine ready for video encoding.');
  await manualDraft(page);
  await page.locator('#voice-provider').selectOption('espeak');
  await page.locator('#render-quality').selectOption('preview');
  await page.locator('#render-draft').click();
  const card = page.locator('#latest-jobs .job-card').first();
  await expect(card.locator('.job-badge')).toHaveText(/completed|ready/i, { timeout: 90_000 });
  const video = card.locator('video');
  await expect(video).toBeVisible();
  await expect.poll(() => video.evaluate(v => v.readyState)).toBeGreaterThanOrEqual(1);
  expect(await video.evaluate(v => v.duration)).toBeGreaterThan(3);
  await video.evaluate(v => v.play());
  await expect.poll(() => video.evaluate(v => v.currentTime)).toBeGreaterThan(0.1);
  const response = await request.get(await video.getAttribute('src'));
  expect(response.ok()).toBeTruthy();
  expect((await response.body()).length).toBeGreaterThan(10_000);
});

test('export multiple Shorts with a ZIP (opt-in media)', async ({ page, request }) => {
  test.skip(!mediaTests, 'Set STUDIO_RUN_MEDIA_TESTS=1 on a machine ready for video encoding.');
  const folder = mkdtempSync(path.join(tmpdir(), 'aicf-browser-'));
  const source = path.join(folder, 'browser-source.mp4');
  try {
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=15', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=22050', '-t', '17', '-c:v', 'libx264', '-threads', '1', '-preset', 'ultrafast', '-c:a', 'aac', source]);
    await page.goto('/#shorts');
    await selectMode(page, 'source-mode', 'upload');
    await page.locator('#video-upload').setInputFiles(source);
    await expect(page.locator('#upload-status')).toContainText('is ready');
    await page.locator('#shorts-duration').selectOption('15');
    await page.locator('label').filter({ has: page.locator('#shorts-captions') }).click();
    await page.locator('#shorts-submit').click();
    const card = page.locator('#shorts-jobs .job-card').first();
    await expect(card.locator('.job-badge')).toHaveText(/completed|ready/i, { timeout: 110_000 });
    await expect(card.locator('video')).toHaveCount(2);
    const zipLink = card.locator('a[href$="/download"]');
    const response = await request.get(await zipLink.getAttribute('href'));
    expect(response.ok()).toBeTruthy();
    const zip = await response.body();
    expect(zip.subarray(0, 2).toString()).toBe('PK');
    expect(zip.length).toBeGreaterThan(10_000);
  } finally { rmSync(folder, { recursive: true, force: true }); }
});
