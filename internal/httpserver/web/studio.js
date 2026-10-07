/* AI Content Farm Studio: native, accessible UI over the durable Go job APIs. */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  const $$ = selector => Array.from(document.querySelectorAll(selector));
  const STORE_KEY = 'content-farm.studio.v1';
  const ACTIVE = new Set(['queued', 'running']);
  const names = {create: 'Create a video', shorts: 'Make Shorts', projects: 'Projects', cast: 'Your cast', settings: 'Settings'};
  const state = {draft: null, capabilities: null, studioJobs: [], shortsJobs: [], latestId: '', filter: 'all', view: 'create', uploadedSource: '', polling: false, uploading: false, pollTimer: null, providerChosen: false, voiceChosen: false, savedVoice: '', notesExpanded: false};
  const cardStores = new Map();
  let toastTimer;

  function element(tag, className, text, parent) {
    const el = document.createElement(tag);
    if (className) el.className = className;
    if (text !== undefined && text !== null) el.textContent = text;
    if (parent) parent.append(el);
    return el;
  }
  function icon(name) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
    use.setAttribute('href', `#i-${name}`); svg.append(use); return svg;
  }
  function makeButton(label, style, symbol, action) {
    const button = element('button', `button ${style}`, '');
    button.type = 'button';
    if (symbol) button.append(icon(symbol));
    button.append(document.createTextNode(label));
    if (action) button.addEventListener('click', action);
    return button;
  }
  function safeURL(raw, localOnly = false) {
    if (typeof raw !== 'string' || !raw) return '';
    try {
      const url = new URL(raw, location.origin);
      if (!['http:', 'https:'].includes(url.protocol) || (localOnly && url.origin !== location.origin)) return '';
      return url.href;
    } catch { return ''; }
  }
  function setLink(link, raw, download) {
    const url = safeURL(raw, true);
    link.hidden = !url;
    if (url) {
      link.href = url;
      if (download) link.setAttribute('download', download);
    }
  }
  function humanError(value) {
    if (typeof value === 'string') return value;
    if (value && typeof value === 'object') return value.message || value.detail || value.error || JSON.stringify(value);
    return 'Something went wrong. Please try again.';
  }
  async function api(path, options = {}, timeout = 180000) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout);
    try {
      const response = await fetch(path, {cache: 'no-store', ...options, signal: controller.signal});
      const raw = await response.text();
      let data;
      try { data = raw ? JSON.parse(raw) : null; }
      catch { throw new Error(response.ok ? 'The server returned an unexpected response.' : `Request failed (${response.status}). Check that the studio server is running.`); }
      if (!response.ok) throw new Error(humanError(data?.error || data?.detail || data?.message || `Request failed (${response.status}).`));
      return data;
    } catch (error) {
      if (error.name === 'AbortError') throw new Error('The request timed out. Check Projects before retrying; the server may still be processing it.');
      if (error instanceof TypeError) throw new Error('Could not reach the studio. Check your connection and that the server is running.');
      throw error;
    } finally { clearTimeout(timer); }
  }
  const post = (path, body) => api(path, {method: 'POST', ...(body === undefined ? {} : {headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)})});
  function status(id, message = '', kind = '') {
    const el = $(id); el.textContent = message; el.hidden = !message;
    el.className = `inline-status ${kind}`.trim();
  }
  function toast(message, error = false) {
    clearTimeout(toastTimer); $('toast').textContent = message;
    $('toast').className = `toast${error ? ' error' : ''}`; $('toast').hidden = false;
    toastTimer = setTimeout(() => { $('toast').hidden = true; }, error ? 9000 : 4500);
  }
  function busy(button, loading, text) {
    if (!button.dataset.restText) button.dataset.restText = button.querySelector('span')?.textContent || button.textContent;
    button.disabled = loading; button.classList.toggle('busy', loading);
    button.setAttribute('aria-busy', String(loading));
    const label = button.querySelector('span');
    if (label) label.textContent = loading ? text : button.dataset.restText;
  }
  function chosen(name) { return document.querySelector(`input[name="${name}"]:checked`)?.value || ''; }
  function choose(name, value) {
    const input = $$(`input[name="${name}"]`).find(node => node.value === value);
    if (input) input.checked = true;
  }
  function changeView(view, updateHash = true, focus = false) {
    if (!(view in names)) view = 'create';
    state.view = view;
    Object.keys(names).forEach(key => { $(`view-${key}`).hidden = key !== view; });
    $$('.nav-item[data-view]').forEach(button => {
      const active = button.dataset.view === view;
      button.classList.toggle('active', active);
      if (active) button.setAttribute('aria-current', 'page'); else button.removeAttribute('aria-current');
    });
    $('page-breadcrumb').textContent = names[view];
    document.title = `${names[view]} — AI Content Farm`;
    if (updateHash && location.hash !== `#${view}`) history.replaceState(null, '', `#${view}`);
    if (focus) { $('main').focus({preventScroll: true}); window.scrollTo({top: 0, behavior: 'instant'}); }
  }
  document.addEventListener('click', event => {
    const button = event.target.closest('[data-view]');
    if (button) { event.preventDefault(); changeView(button.dataset.view, true, true); }
  });
  window.addEventListener('hashchange', () => changeView(location.hash.slice(1), false, true));

  function save() {
    try {
      localStorage.setItem(STORE_KEY, JSON.stringify({draft: state.draft, latestId: state.latestId, topic: $('topic').value, source_notes: $('source-notes').value, provider: chosen('provider'), pair: $('pair').value, format: chosen('format'), target_seconds: $('target-seconds').value, voice_provider: $('voice-provider').value}));
    } catch { /* Private browsing and full local storage must not block creation. */ }
  }
  function restore() {
    try {
      const data = JSON.parse(localStorage.getItem(STORE_KEY) || 'null');
      if (!data || typeof data !== 'object') return;
      if (typeof data.topic === 'string') $('topic').value = data.topic;
      if (typeof data.source_notes === 'string') { $('source-notes').value = data.source_notes; state.notesExpanded = !!data.source_notes.trim(); }
      if (data.provider) { choose('provider', data.provider); state.providerChosen = true; }
      if (data.voice_provider) { state.savedVoice = data.voice_provider; state.voiceChosen = true; }
      if (data.format) choose('format', data.format);
      if (data.target_seconds) $('target-seconds').value = data.target_seconds;
      if (data.pair) $('pair').dataset.saved = data.pair;
      if (typeof data.latestId === 'string') state.latestId = data.latestId;
      if (data.draft && Array.isArray(data.draft.scenes) && data.draft.scenes.length <= 24) {
        state.draft = data.draft; renderDraft();
      }
    } catch { /* Ignore incomplete data from previous studio versions. */ }
  }
  function modeChanged() {
    const mode = chosen('provider');
    if (mode === 'ai' && $('source-notes').value.trim()) state.notesExpanded = true;
    $('source-notes-field').hidden = mode === 'demo' || (mode === 'ai' && !state.notesExpanded);
    $('toggle-source-notes').hidden = mode !== 'ai';
    $('toggle-source-notes').setAttribute('aria-expanded', String(state.notesExpanded));
    $('toggle-source-notes').querySelector('span').textContent = state.notesExpanded ? 'Source notes & direction' : 'Add source notes & direction';
    document.querySelector('.composer .button-footnote').textContent = mode === 'demo' ? 'Your final say before rendering. Demo scripts have a fixed length.' : mode === 'manual' ? 'Your words, two voices. Review the scene split before export.' : 'Review every scene before rendering. You stay in control.';
    $('source-notes').required = mode === 'manual';
    $('topic-presets').hidden = mode !== 'demo';
    $('source-notes-label').textContent = mode === 'manual' ? 'Your script' : 'Source notes & direction (optional)';
    $('source-notes').placeholder = mode === 'manual' ? 'Paste your dialogue. Separate each speaker’s turn with a new paragraph.' : 'Facts to include, source links, your audience, and the angle you have in mind…';
    $('source-notes-help').textContent = mode === 'manual' ? 'Your words become alternating scenes. Review every line before rendering.' : 'The planner uses these notes. Check claims and sources before you render.';
    const aiReady = state.capabilities?.planner?.ai_configured;
    $('mode-explanation').textContent = mode === 'demo'
      ? 'Curated scripts. Use the demo voice for a no-key export.'
      : mode === 'manual' ? 'Bring your own words. We’ll arrange the dialogue into scenes for your presenters.'
      : aiReady ? 'Your cloud model plans the dialogue. Every scene stays editable before rendering.'
      : 'AI scripting needs a model configured on the server. Check Settings for your workspace’s readiness.';
    const label = mode === 'manual' ? 'Build my scenes' : 'Generate a draft';
    const button = $('generate-draft'); button.dataset.restText = label;
    if (!button.disabled) button.querySelector('span').textContent = label;
    save();
  }
  const builtins = {
    'cog-axiom': {id: 'cog-axiom', name: 'Cog & Axiom', speakers: ['Cog', 'Axiom'], available: true},
    'nova-atlas': {id: 'nova-atlas', name: 'Nova & Atlas', speakers: ['Nova', 'Atlas'], available: true},
    'ryusui-senku': {id: 'ryusui-senku', name: 'Ryusui & Senku', speakers: ['Ryusui', 'Senku'], available: false},
    'ryusui-sai': {id: 'ryusui-sai', name: 'Ryusui & Sai', speakers: ['Ryusui', 'Sai'], available: false}
  };
  function pairInfo(id) {
    return (state.capabilities?.pairs || []).find(pair => pair.id === id) || builtins[id] || builtins['cog-axiom'];
  }
  function speakerNames(pair) {
    const speakers = pairInfo(pair).speakers;
    return Array.isArray(speakers) && speakers.length >= 2 ? speakers.map(s => typeof s === 'string' ? s : s.name || 'Presenter') : ['Presenter 1', 'Presenter 2'];
  }
  function spriteURLs(pairId) {
    const pair = pairInfo(pairId);
    if (Array.isArray(pair.sprite_urls) && pair.sprite_urls.length >= 2) return pair.sprite_urls.slice(0, 2);
    if (pairId === 'cog-axiom') return ['/api/studio/presenters/cog-idle.png', '/api/studio/presenters/axiom-talk.png'];
    if (pairId === 'nova-atlas') return ['/api/studio/presenters/nova-idle.png', '/api/studio/presenters/atlas-talk.png'];
    if (pairId.startsWith('ryusui-')) return [];
    return [`/api/studio/cast/${encodeURIComponent(pairId)}/idle_a.png`, `/api/studio/cast/${encodeURIComponent(pairId)}/idle_b.png`];
  }
  function addPresenters(container, pairId = 'cog-axiom') {
    container.replaceChildren();
    container.dataset.pair = pairId;
    for (const raw of spriteURLs(pairId)) {
      const url = safeURL(raw, true);
      if (!url) continue;
      const img = element('img', '', null, container);
      img.src = url; img.alt = ''; img.width = 600; img.height = 740; img.decoding = 'sync';
      // Decode concept art eagerly, including when the mobile preview starts below the fold.
      if (img.decode) img.decode().catch(() => {});
      img.addEventListener('load', () => { img.style.visibility = ''; });
      img.addEventListener('error', () => { img.style.visibility = 'hidden'; });
    }
  }
  function updatePreview() {
    const topic = $('topic').value.trim();
    const title = (state.draft?.topic === topic ? state.draft.title : '') || topic || 'Your next big idea';
    const el = $('preview-title');
    el.classList.toggle('long-title', title.length > 30 && !/sky.*blue/i.test(title));
    if (/sky.*blue/i.test(title)) {
      el.replaceChildren(document.createTextNode('Why is the'), document.createElement('br'), document.createTextNode('sky blue'), element('span', '', '?'));
    } else {
      el.textContent = title.length > 63 ? `${title.slice(0, 60)}…` : title;
    }
    const format = chosen('format');
    $('composition').classList.toggle('landscape', format === 'landscape');
    $('composition').classList.toggle('square', format === 'square');
    $('preview-format').textContent = format === 'landscape' ? '16:9 · LANDSCAPE' : format === 'square' ? '1:1 · SQUARE' : '9:16 · PORTRAIT';
    const pair = $('pair').value || 'cog-axiom';
    $('preview-cast').textContent = speakerNames(pair).join(' + ').toUpperCase();
    const hasArt = spriteURLs(pair).length > 0;
    $('preview-presenters').hidden = !hasArt;
    if (hasArt && $('preview-presenters').dataset.pair !== pair) addPresenters($('preview-presenters'), pair);
    $('preview-personal').hidden = hasArt;
    $$('.tiny-avatars i').forEach((avatar, index) => { avatar.textContent = speakerNames(pair)[index]?.charAt(0) || ''; });
    $$('#preview-personal>span').forEach((span, index) => { span.textContent = speakerNames(pair)[index]?.charAt(0) || ''; });
    const caption = state.draft?.scenes?.[0]?.text;
    $('preview-caption-text').textContent = caption ? (caption.length > 68 ? `${caption.slice(0, 65)}…` : caption) : 'A little curiosity goes a long way.';
  }
  $('toggle-source-notes').addEventListener('click', () => {
    state.notesExpanded = !state.notesExpanded;
    $('source-notes-field').hidden = !state.notesExpanded;
    $('toggle-source-notes').setAttribute('aria-expanded', String(state.notesExpanded));
    $('toggle-source-notes').querySelector('span').textContent = state.notesExpanded ? 'Source notes & direction' : 'Add source notes & direction';
    if (state.notesExpanded) $('source-notes').focus();
  });
  $$('.mode-control input').forEach(input => input.addEventListener('change', () => { state.providerChosen = true; modeChanged(); }));
  $('draft-form').addEventListener('input', event => { if (event.target.id !== 'source-notes') updatePreview(); save(); });
  $('draft-form').addEventListener('change', event => {
    if (state.draft && (event.target.name === 'format' || event.target.id === 'pair')) {
      state.draft.format = chosen('format'); state.draft.pair = $('pair').value;
      if (event.target.id === 'pair') renderScenes();
    }
    updatePreview(); save();
  });
  $$('[data-topic]').forEach(button => button.addEventListener('click', () => {
    $('topic').value = button.dataset.topic; state.providerChosen = true; choose('provider', 'demo'); modeChanged();
    updatePreview(); $('topic').focus(); save();
  }));

  function showTrace(container, trace) {
    container.replaceChildren();
    for (const step of trace || []) {
      const li = element('li', '', null, container);
      element('strong', '', String(step.node || step.stage || 'Step').replaceAll('_', ' '), li);
      element('small', '', `${step.status || 'complete'}${Number.isFinite(step.duration_ms) ? ` · ${step.duration_ms} ms` : ''}`, li);
      if (step.detail) element('p', '', step.detail, li);
    }
    if (!trace?.length) element('li', '', 'No planning steps were recorded for this draft.', container);
  }
  function updateEstimate() {
    if (!state.draft) return;
    const words = state.draft.scenes.reduce((n, scene) => n + scene.text.trim().split(/\s+/).filter(Boolean).length, 0);
    const seconds = Math.round(words / 2.35 + state.draft.scenes.length * .3);
    state.draft.estimated_seconds = seconds;
    $('draft-summary').textContent = `${state.draft.scenes.length} SCENES · ~${seconds} SEC`;
    $('add-scene').disabled = state.draft.scenes.length >= 24;
    $$('.remove-scene').forEach(button => { button.disabled = state.draft.scenes.length <= 2; });
  }
  function renderScenes() {
    const list = $('scene-list'); list.replaceChildren();
    const speakers = speakerNames(state.draft.pair);
    state.draft.scenes.forEach((scene, index) => {
      const row = element('article', 'scene-card', null, list);
      element('span', 'scene-number', String(index + 1).padStart(2, '0'), row);
      const content = element('div', 'scene-content', null, row);
      const top = element('div', 'scene-topline', null, content);
      const speaker = element('select', '', null, top);
      speaker.setAttribute('aria-label', `Speaker for scene ${index + 1}`);
      speakers.forEach((name, value) => speaker.add(new Option(name, String(value))));
      speaker.value = String(scene.speaker);
      speaker.addEventListener('change', () => { scene.speaker = Number(speaker.value); save(); });
      const remove = element('button', 'icon-button remove-scene', null, top); remove.type = 'button';
      remove.setAttribute('aria-label', `Remove scene ${index + 1}`); remove.title = `Remove scene ${index + 1}`; remove.append(icon('close'));
      remove.addEventListener('click', () => {
        if (state.draft.scenes.length <= 2) return;
        state.draft.scenes.splice(index, 1); renderScenes(); updateEstimate(); save(); updatePreview();
        const next = $('scene-list').children[Math.min(index, state.draft.scenes.length - 1)];
        next?.querySelector('textarea')?.focus();
      });
      const text = element('textarea', 'scene-text', null, content);
      text.value = scene.text; text.rows = 3; text.maxLength = 600; text.required = true;
      text.id = `scene-text-${index}`; text.setAttribute('aria-label', `Dialogue for scene ${index + 1}`);
      text.addEventListener('input', () => { scene.text = text.value; updateEstimate(); save(); updatePreview(); });
      const visualRow = element('div', 'scene-visual', null, content);
      const label = element('label', '', 'ON-SCREEN CARD', visualRow); label.htmlFor = `scene-visual-${index}`;
      const visual = element('input', '', null, visualRow); visual.id = label.htmlFor; visual.value = scene.visual; visual.maxLength = 140; visual.required = true;
      visual.placeholder = 'The key idea shown on screen';
      visual.addEventListener('input', () => { scene.visual = visual.value; save(); });
    });
    updateEstimate();
  }
  function renderDraft() {
    if (!state.draft) return;
    $('review-section').hidden = false; $('review-dot').classList.add('selected');
    $('draft-title').value = state.draft.title || '';
    renderScenes();
    $('draft-warnings').replaceChildren();
    for (const warning of state.draft.warnings || []) element('p', 'inline-status', humanError(warning), $('draft-warnings'));
    const sources = state.draft.sources || [];
    $('sources-details').hidden = !sources.length; $('draft-sources').replaceChildren();
    for (const source of sources) {
      const li = element('li', '', null, $('draft-sources'));
      const url = safeURL(source.url);
      if (url) { const a = element('a', '', source.title || source.url, li); a.href = url; a.target = '_blank'; a.rel = 'noopener noreferrer'; }
      else li.textContent = source.title || 'Reference';
    }
    showTrace($('draft-trace'), state.draft.trace);
    $('trace-count').textContent = `(${state.draft.trace?.length || 0})`;
    updatePreview();
  }
  $('draft-title').addEventListener('input', () => { if (state.draft) { state.draft.title = $('draft-title').value; save(); updatePreview(); } });
  $('add-scene').addEventListener('click', () => {
    if (!state.draft || state.draft.scenes.length >= 24) return;
    const previous = state.draft.scenes[state.draft.scenes.length - 1];
    state.draft.scenes.push({speaker: previous?.speaker === 0 ? 1 : 0, text: '', visual: ''});
    renderScenes(); save(); $('scene-list').lastElementChild.querySelector('textarea').focus();
  });
  $('draft-form').addEventListener('submit', async event => {
    event.preventDefault();
    const button = $('generate-draft');
    busy(button, true, 'Planning your story…');
    status('draft-status', 'Building your scenes. You can edit every line in the next step.', 'loading');
    const request = {topic: $('topic').value.trim(), source_notes: chosen('provider') === 'demo' ? '' : $('source-notes').value.trim(), pair: $('pair').value, format: chosen('format'), target_seconds: Number($('target-seconds').value), provider: chosen('provider')};
    try {
      const draft = await post('/api/studio/drafts', request);
      if (!draft || !Array.isArray(draft.scenes) || !draft.scenes.length) throw new Error('The planner returned no scenes. Please try again.');
      state.draft = draft; renderDraft(); save();
      status('draft-status', 'Draft ready. Review your scenes below, then render when it feels right.');
      status('render-status');
      $('review-heading').focus({preventScroll: true}); $('review-section').scrollIntoView({behavior: 'smooth', block: 'start'});
    } catch (error) { status('draft-status', error.message, 'error'); }
    finally { busy(button, false); }
  });
  $('render-draft').addEventListener('click', async () => {
    if (!state.draft) return;
    if (!$('voice-provider').value) { status('render-status', 'No voice provider is ready. Check Settings to configure Gemini or the lightweight demo voice.', 'error'); return; }
    for (const input of [ $('draft-title'), ...$$('#scene-list textarea, #scene-list input') ]) {
      if (!input.reportValidity()) { input.focus(); return; }
    }
    if (new Set(state.draft.scenes.map(scene => scene.speaker)).size < 2) {
      status('render-status', 'Give both presenters at least one scene before rendering.', 'error'); return;
    }
    const button = $('render-draft'); busy(button, true);
    status('render-status', 'Sending your reviewed draft to the render queue…', 'loading');
    try {
      const job = await post('/api/studio/jobs', {draft: state.draft, quality: $('render-quality').value, voice_provider: $('voice-provider').value});
      state.latestId = job.id; state.studioJobs = [job, ...state.studioJobs.filter(item => item.id !== job.id)];
      $('render-dot').classList.add('selected'); save(); updateJobs();
      status('render-status', 'Your video is queued. Follow the render below; processing continues while the server is running.');
      $('latest-section').scrollIntoView({behavior: 'smooth', block: 'start'});
      await refreshJobs();
    } catch (error) { status('render-status', error.message, 'error'); }
    finally { busy(button, false); }
  });

  function makeJobCard(type) {
    const root = element('article', 'job-card');
    const header = element('div', 'job-header', null, root);
    const heading = element('div', 'job-heading', null, header);
    const kicker = element('p', 'job-kicker', null, heading);
    const title = element('h3', 'job-title', null, heading);
    const badge = element('span', 'job-badge', null, header);
    const message = element('p', 'job-message', null, root);
    const progress = element('progress', 'job-progress', null, root); progress.max = 100;
    progress.setAttribute('aria-label', 'Rendering progress');
    const error = element('pre', 'job-error', null, root); error.hidden = true;
    const actions = element('div', 'job-actions', null, root);
    const card = {root, kicker, title, badge, message, progress, error, actions, type, job: null, clipMap: new Map()};
    const action = async (button, kind) => {
      button.disabled = true;
      try {
        const id = encodeURIComponent(card.job.id);
        let result;
        if (kind === 'cancel') result = await post(type === 'studio' ? `/api/studio/jobs/${id}/cancel` : `/api/shorts/${id}/cancel`);
        else if (type === 'studio') result = await post(`/api/studio/jobs/${id}/retry`);
        else result = await post('/api/shorts', card.job.request);
        if (kind === 'retry' && type === 'studio' && result?.id) { state.latestId = result.id; save(); }
        await refreshJobs(); toast(kind === 'cancel' ? 'Cancellation requested.' : 'A new attempt is queued.');
      } catch (err) { toast(err.message, true); }
      finally { button.disabled = false; }
    };
    card.cancel = makeButton('Cancel job', 'secondary small', 'close');
    card.retry = makeButton('Retry', 'secondary small', 'refresh');
    card.cancel.addEventListener('click', () => action(card.cancel, 'cancel'));
    card.retry.addEventListener('click', () => action(card.retry, 'retry'));
    actions.append(card.cancel, card.retry);
    card.download = element('a', 'button primary small', null, actions); card.download.append(icon('download'), document.createTextNode(type === 'studio' ? 'Download MP4' : 'Download all · ZIP'));
    if (type === 'studio') {
      card.captions = element('a', 'button secondary small', 'Captions · SRT', actions);
      card.edit = makeButton('Edit this draft', 'secondary small', 'create', () => {
        state.draft = JSON.parse(JSON.stringify(card.job.request.draft));
        if (card.job.request.voice_provider && [...$('voice-provider').options].some(option => option.value === card.job.request.voice_provider && !option.disabled)) { $('voice-provider').value = card.job.request.voice_provider; state.voiceChosen = true; voiceChanged(); }
        $('topic').value = state.draft.topic; choose('provider', state.draft.provider); choose('format', state.draft.format);
        if ([...$('pair').options].some(option => option.value === state.draft.pair)) $('pair').value = state.draft.pair;
        modeChanged(); renderDraft(); save(); changeView('create');
        $('review-section').scrollIntoView({behavior: 'smooth', block: 'start'}); $('review-heading').focus({preventScroll: true});
      });
      actions.append(card.edit);
      card.video = element('video', 'job-player', null, root); card.video.controls = true; card.video.preload = 'metadata'; card.video.playsInline = true; card.video.hidden = true;
      card.video.addEventListener('error', () => { if (card.video.getAttribute('src')) { card.error.textContent = 'This video could not be loaded. Try the download link, or retry the render if the output file is missing.'; card.error.hidden = false; } });
    } else card.clips = element('div', 'clip-grid', null, root);
    card.details = element('details', 'job-trace', null, root);
    element('summary', '', 'Production details', card.details);
    card.trace = element('ol', 'trace-list', null, card.details);
    card.traceVersion = '';
    return card;
  }
  function updateJobCard(card, job, type) {
    card.job = job;
    const date = new Date(job.created_at);
    const when = Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString(undefined, {month: 'short', day: 'numeric'});
    card.kicker.textContent = `${type === 'studio' ? 'PRESENTER VIDEO' : 'SHORTS BATCH'}${when ? ` / ${when}` : ''}`;
    card.title.textContent = type === 'studio' ? job.request?.draft?.title || 'Presenter video' : job.request?.source?.split('/').pop() || job.request?.url || 'Shorts batch';
    card.badge.className = `job-badge ${job.status}`; card.badge.textContent = job.status === 'cancelled' ? 'canceled' : job.status;
    const percent = Math.min(100, Math.max(0, Number(job.progress) || 0));
    card.message.textContent = `${ACTIVE.has(job.status) ? `${percent}% · ` : ''}${job.message || job.stage || job.status}`;
    card.progress.value = percent; card.progress.hidden = !ACTIVE.has(job.status);
    const errors = [job.error, ...(job.warnings || [])].filter(Boolean).map(humanError).join('\n');
    card.error.hidden = !errors; if (errors) card.error.textContent = errors;
    card.cancel.hidden = !ACTIVE.has(job.status); card.retry.hidden = !['failed', 'canceled', 'cancelled'].includes(job.status);
    if (type === 'studio') {
      setLink(card.download, job.output_url ? `${job.output_url}${job.output_url.includes('?') ? '&' : '?'}download=1` : '', 'video.mp4');
      setLink(card.captions, job.caption_url ? `${job.caption_url}${job.caption_url.includes('?') ? '&' : '?'}download=1` : '', 'captions.srt');
      card.edit.hidden = !job.request?.draft;
      const output = safeURL(job.output_url, true);
      card.video.hidden = !output;
      if (output && card.video.getAttribute('src') !== output) {
        card.video.src = output; const poster = safeURL(job.poster_url, true); if (poster) card.video.poster = poster;
        card.video.setAttribute('aria-label', card.title.textContent);
      }
    } else {
      const clips = job.clips || [];
      setLink(card.download, clips.length ? `/api/shorts/${encodeURIComponent(job.id)}/download` : '', `shorts-${job.id}.zip`);
      for (const clip of clips) {
        const key = clip.filename || clip.url;
        if (card.clipMap.has(key)) continue;
        const tile = element('article', 'clip-card', null, card.clips);
        const video = element('video', '', null, tile); video.controls = true; video.preload = 'none'; video.playsInline = true;
        const url = safeURL(clip.url, true); if (url) video.src = url;
        video.setAttribute('aria-label', clip.title || clip.filename || 'Short clip');
        element('p', '', clip.title || clip.filename || 'Short clip', tile);
        element('small', '', Number.isFinite(Number(clip.duration)) ? `${Number(clip.duration).toFixed(1)} SEC · 9:16` : '9:16', tile);
        const link = element('a', '', 'Download MP4', tile); setLink(link, clip.url, clip.filename || 'short.mp4');
        card.clipMap.set(key, tile);
      }
    }
    const trace = job.trace || [];
    const traceVersion = JSON.stringify(trace);
    card.details.hidden = !trace.length;
    if (traceVersion !== card.traceVersion) { showTrace(card.trace, trace); card.traceVersion = traceVersion; }
  }
  function renderJobList(containerId, jobs) {
    const container = $(containerId);
    if (!cardStores.has(containerId)) cardStores.set(containerId, new Map());
    const cards = cardStores.get(containerId); const current = new Set();
    jobs.forEach(({job, type}, index) => {
      const key = `${type}:${job.id}`; current.add(key);
      let card = cards.get(key);
      if (!card) { card = makeJobCard(type); cards.set(key, card); }
      updateJobCard(card, job, type);
      const existing = container.children[index];
      // Keep existing video elements and focused controls attached during polling.
      if (existing !== card.root) container.insertBefore(card.root, existing || null);
    });
    for (const [key, card] of cards) if (!current.has(key)) { card.root.remove(); cards.delete(key); }
  }
  function updateJobs() {
    const all = [...state.studioJobs.map(job => ({job, type: 'studio'})), ...state.shortsJobs.map(job => ({job, type: 'shorts'}))]
      .sort((a, b) => new Date(b.job.created_at) - new Date(a.job.created_at));
    const shown = all.filter(({job}) => state.filter === 'all' || (state.filter === 'active' ? ACTIVE.has(job.status) : job.status === 'completed'));
    renderJobList('project-jobs', shown);
    $('projects-empty').hidden = shown.length > 0;
    const noProjects = all.length === 0;
    $('projects-empty').querySelector('h2').textContent = noProjects ? 'The first one is always special.' : state.filter === 'active' ? 'Nothing in the queue.' : 'Still a work in progress.';
    $('projects-empty').querySelector('p').textContent = noProjects ? 'Create a presenter video or cut your first batch of Shorts. Your work will have a home right here.' : state.filter === 'active' ? 'Your studio is ready for the next idea. All your existing projects are in All projects.' : 'Completed videos and clip batches will appear here as they finish rendering.';
    $('project-count').textContent = String(all.length); $('project-count').hidden = !all.length;
    renderJobList('shorts-jobs', state.shortsJobs.map(job => ({job, type: 'shorts'})));
    $('shorts-empty').hidden = state.shortsJobs.length > 0;
    const latest = state.studioJobs.find(job => job.id === state.latestId);
    $('latest-section').hidden = !latest;
    if (latest) renderJobList('latest-jobs', [{job: latest, type: 'studio'}]);
  }
  async function refreshJobs() {
    if (state.polling) return;
    state.polling = true;
    try {
      const results = await Promise.allSettled([api('/api/studio/jobs', {}, 20000), api('/api/shorts', {}, 20000)]);
      const errors = [];
      if (results[0].status === 'fulfilled') state.studioJobs = Array.isArray(results[0].value) ? results[0].value : results[0].value?.jobs || [];
      else errors.push(`Presenter projects: ${results[0].reason.message}`);
      if (results[1].status === 'fulfilled') state.shortsJobs = Array.isArray(results[1].value) ? results[1].value : results[1].value?.jobs || [];
      else errors.push(`Shorts projects: ${results[1].reason.message}`);
      status('projects-status', errors.join('\n'), errors.length ? 'error' : '');
      if (errors.length) {
        $('connection-label').textContent = 'Connection issue';
        $('connection').querySelector('.status-dot').className = 'status-dot unavailable';
      } else if (state.capabilities) {
        const ready = state.capabilities.planner?.ready && state.capabilities.renderer?.ready;
        $('connection-label').textContent = ready ? 'Studio ready' : 'Setup needed';
        $('connection').querySelector('.status-dot').className = `status-dot${ready ? '' : ' unavailable'}`;
      }
      updateJobs();
    } finally { state.polling = false; }
  }
  $$('.refresh-jobs').forEach(button => button.addEventListener('click', async () => { busy(button, true); await refreshJobs(); busy(button, false); }));
  $$('[data-filter]').forEach(button => button.addEventListener('click', () => {
    state.filter = button.dataset.filter;
    $$('[data-filter]').forEach(item => { item.classList.toggle('selected', item === button); item.setAttribute('aria-pressed', String(item === button)); });
    updateJobs();
  }));

  function sourceModeChanged() {
    const mode = chosen('source-mode');
    $('url-source').hidden = mode !== 'url'; $('upload-source').hidden = mode !== 'upload'; $('library-source').hidden = mode !== 'library';
    $('shorts-url').required = mode === 'url'; $('shorts-source').required = mode === 'library';
    $('shorts-submit').disabled = state.uploading;
  }
  $$('input[name="source-mode"]').forEach(input => input.addEventListener('change', sourceModeChanged));
  async function loadLibrary(selectSource) {
    try {
      const result = await api('/api/videos');
      const videos = Array.isArray(result) ? result : result?.videos || [];
      const select = $('shorts-source'); const previous = selectSource || select.value;
      select.replaceChildren(new Option(videos.length ? 'Choose an uploaded video' : 'No videos yet — upload one to begin', ''));
      for (const video of videos) {
        const path = typeof video === 'string' ? video : video.path || video.name;
        if (path) select.add(new Option(path, path));
      }
      if ([...select.options].some(option => option.value === previous)) select.value = previous;
      return videos;
    } catch (error) { status('shorts-status', `Cannot load your library: ${error.message}`, 'error'); return []; }
  }
  $('refresh-library').addEventListener('click', async () => { $('refresh-library').disabled = true; await loadLibrary(); $('refresh-library').disabled = false; });
  async function uploadVideo(file) {
    if (!file || state.uploading) return;
    if (!/\.(mp4|mov|mkv|webm)$/i.test(file.name)) { status('upload-status', 'Choose an MP4, MOV, MKV, or WebM video.', 'error'); return; }
    state.uploading = true; state.uploadedSource = ''; $('shorts-submit').disabled = true; $('video-upload').disabled = true;
    status('upload-status', `Uploading ${file.name}… Keep this page open until the upload finishes.`, 'loading');
    try {
      const data = new FormData(); data.append('videos', file);
      const result = await api('/api/videos/upload', {method: 'POST', body: data}, 600000);
      if (!result?.uploaded?.length) throw new Error('The server did not accept the video. Use an MP4, MOV, MKV, or WebM file.');
      state.uploadedSource = result.uploaded[0];
      const videos = await loadLibrary(state.uploadedSource);
      const match = videos.find(video => (video.path || video.name || video) === state.uploadedSource || (video.path || video.name || '').endsWith(`/${state.uploadedSource}`));
      if (match) state.uploadedSource = match.path || match.name || match;
      $('shorts-source').value = state.uploadedSource;
      status('upload-status', `${file.name} is ready. Choose a layout and make the cuts.`);
    } catch (error) { status('upload-status', error.message, 'error'); }
    finally { state.uploading = false; $('video-upload').disabled = false; $('shorts-submit').disabled = false; }
  }
  $('video-upload').accept = '.mp4,.mov,.mkv,.webm,video/mp4,video/quicktime,video/webm';
  $('video-upload').addEventListener('change', () => uploadVideo($('video-upload').files[0]));
  $('upload-zone').addEventListener('dragover', event => { event.preventDefault(); $('upload-zone').classList.add('dragging'); });
  $('upload-zone').addEventListener('dragleave', () => $('upload-zone').classList.remove('dragging'));
  $('upload-zone').addEventListener('drop', event => { event.preventDefault(); $('upload-zone').classList.remove('dragging'); uploadVideo(event.dataTransfer.files[0]); });
  $('shorts-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (state.uploading) return;
    const mode = chosen('source-mode');
    const source = mode === 'library' ? $('shorts-source').value : mode === 'upload' ? state.uploadedSource : '';
    const url = mode === 'url' ? $('shorts-url').value.trim() : '';
    if (!source && !url) { status('shorts-status', mode === 'upload' ? 'Upload a video first, then make your cuts.' : 'Choose a source video or paste a YouTube link.', 'error'); return; }
    const request = {source, url, layout: chosen('shorts-layout'), language: $('shorts-language').value, max_duration: Number($('shorts-duration').value), no_captions: !$('shorts-captions').checked};
    busy($('shorts-submit'), true, 'Queuing your footage…');
    status('shorts-status', 'Sending your video to the cutting room…', 'loading');
    try {
      const job = await post('/api/shorts', request);
      if (job?.id) state.shortsJobs = [job, ...state.shortsJobs.filter(item => item.id !== job.id)];
      updateJobs(); await refreshJobs();
      status('shorts-status', 'Your batch is queued. Processing continues while the server is running. Clips appear below as they finish.');
      $('shorts-jobs').scrollIntoView({behavior: 'smooth', block: 'start'});
    } catch (error) { status('shorts-status', error.message, 'error'); }
    finally { busy($('shorts-submit'), false); }
  });

  function renderCast() {
    const grid = $('cast-grid'); grid.replaceChildren();
    const pairs = state.capabilities?.pairs || [builtins['cog-axiom'], builtins['nova-atlas']];
    for (const pair of pairs) {
      const card = element('article', 'cast-card', null, grid);
      const original = ['cog-axiom', 'nova-atlas'].includes(pair.id);
      const hasArt = spriteURLs(pair.id).length > 0;
      const art = element('div', `cast-art${hasArt ? '' : ' personal'}`, null, card); art.setAttribute('aria-hidden', 'true');
      if (hasArt) addPresenters(art, pair.id);
      else speakerNames(pair.id).forEach(name => element('span', '', name.charAt(0), art));
      const info = element('div', 'cast-info', null, card);
      element('p', 'job-kicker', original ? 'THE ORIGINALS / INCLUDED' : 'YOUR CAST / LOCAL SPRITES', info);
      element('h2', '', pair.name || speakerNames(pair.id).join(' & '), info);
      const descriptions = {
        'cog-axiom': 'Cog follows the spark. Axiom finds the pattern. An original robot duo built for curious questions and clear explanations.',
        'nova-atlas': 'Nova asks the questions. Atlas connects the dots. Original mouth poses and motion follow the conversation.'
      };
      const animations = Object.values(pair.presenter_animation || {});
      const hasMouthPoses = animations.some(value => String(value).includes('mouth'));
      const customDescription = pair.available ? `Your own presenters, brought to life with ${hasMouthPoses ? 'talking poses and audio-driven motion.' : 'audio-driven motion and a speaking waveform. Add talking poses for mouth animation.'}` : pair.reason || 'Add local presenter images to use this pair.';
      element('p', '', descriptions[pair.id] || customDescription, info);
      const button = makeButton(pair.available ? 'Cast this pair' : 'Assets needed', pair.available ? 'primary' : 'secondary', pair.available ? 'arrow' : 'cast', () => {
        $('pair').value = pair.id;
        if (state.draft) { state.draft.pair = pair.id; renderScenes(); }
        updatePreview(); save(); changeView('create', true, true); toast(`${pair.name || 'Presenter pair'} selected.`);
      });
      button.disabled = !pair.available; info.append(button);
    }
  }
  const uploadedPreviews = new Map();
  ['a', 'b'].forEach(side => {
    $(`idle-${side}`).addEventListener('change', () => {
      const image = $(`cast-image-${side}`), file = $(`idle-${side}`).files[0];
      if (uploadedPreviews.has(side)) URL.revokeObjectURL(uploadedPreviews.get(side));
      image.hidden = !file;
      if (file) { const url = URL.createObjectURL(file); uploadedPreviews.set(side, url); image.src = url; }
    });
  });
  $('cast-form').addEventListener('submit', async event => {
    event.preventDefault();
    const button = $('import-cast');
    const files = $$('#cast-form input[type=file]').flatMap(input => [...input.files]);
    if (files.some(file => !/\.png$/i.test(file.name) || (file.type && file.type !== 'image/png'))) { status('cast-status', 'Use PNG files for presenter portraits and poses.', 'error'); return; }
    if (files.some(file => file.size > 4 * 1024 * 1024)) { status('cast-status', 'Each presenter PNG must be under 4 MiB.', 'error'); return; }
    if (files.reduce((sum, file) => sum + file.size, 0) > 16 * 1024 * 1024 - 8192) { status('cast-status', 'Keep the complete pair under 16 MiB. Try smaller PNGs.', 'error'); return; }
    const form = new FormData($('cast-form'));
    // Empty optional file inputs must be omitted from the multipart request.
    for (const field of ['talk_a', 'talk_b', 'blink_a', 'blink_b']) if (!form.get(field)?.size) form.delete(field);
    const name = $('cast-name').value.trim();
    const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 35) || 'my-cast';
    form.set('id', `${slug}-${Date.now().toString(36).slice(-6)}`);
    form.set('name', name);
    busy(button, true, 'Adding your cast…'); status('cast-status', 'Uploading your sprites and checking the pair…', 'loading');
    try {
      const pair = await api('/api/studio/cast', {method: 'POST', body: form}, 180000);
      await loadCapabilities();
      const id = pair?.id || pair?.pair?.id || form.get('id');
      const listed = [...$('pair').options].some(option => option.value === id);
      if (listed) $('pair').value = id;
      if (state.draft && $('pair').value === id) { state.draft.pair = id; renderScenes(); }
      updatePreview(); save();
      status('cast-status', listed ? `${pair?.name || name} is ready and selected for your next video.` : `${pair?.name || name} was saved. Refresh your workspace in Settings to load the new pair.`);
      $('cast-form').reset();
      for (const [side, url] of uploadedPreviews) { URL.revokeObjectURL(url); $(`cast-image-${side}`).hidden = true; $(`cast-image-${side}`).removeAttribute('src'); }
      uploadedPreviews.clear();
      toast('Your cast is ready. Head to Create a video to use it.');
    } catch (error) { status('cast-status', error.message, 'error'); }
    finally { busy(button, false); }
  });
  function voiceChanged() {
    const provider = $('voice-provider').value;
    $('voice-help').textContent = provider === 'gemini' ? 'Neural narration runs in Gemini’s cloud. Your configured account handles API usage.' : provider === 'piper' ? 'Piper uses the explicitly enabled local voice service.' : 'A simple synthetic demo voice. No voice API calls and no neural model to download.';
    save();
  }
  $('voice-provider').addEventListener('change', () => { state.voiceChosen = true; state.savedVoice = $('voice-provider').value; voiceChanged(); });
  function renderCapabilities(caps) {
    const planner = caps.planner || {}, renderer = caps.renderer || {}, shorts = caps.shorts || {};
    const cloudReady = planner.gemini_configured === true || renderer.voice_providers?.some(provider => provider.id === 'gemini' && provider.available) || shorts.cloud_ready === true;
    const localAllowed = renderer.allow_local_models === true || shorts.local_models_allowed === true;
    const ready = renderer.ready === true && planner.ready === true;
    $('connection-label').textContent = ready ? 'Studio ready' : 'Setup needed';
    $('connection').querySelector('.status-dot').className = `status-dot${ready ? '' : ' unavailable'}`;
    const pairs = caps.pairs || [];
    const selected = $('pair').dataset.saved || $('pair').value;
    if (pairs.length) {
      $('pair').replaceChildren();
      for (const pair of pairs) { const option = new Option(`${pair.name}${pair.available ? '' : ' · assets needed'}`, pair.id); option.disabled = !pair.available; $('pair').add(option); }
      if (pairs.some(pair => pair.id === selected && pair.available)) $('pair').value = selected;
      else if (pairs.some(pair => pair.id === 'cog-axiom' && pair.available)) $('pair').value = 'cog-axiom';
      delete $('pair').dataset.saved;
    }
    const voices = renderer.voice_providers || [
      {id: 'gemini', name: 'Gemini cloud voice', available: cloudReady},
      {id: 'espeak', name: 'Lightweight demo voice', available: renderer.ready}
    ];
    const currentVoice = state.savedVoice || (state.voiceChosen ? $('voice-provider').value : '');
    const availableVoices = voices.filter(provider => provider.id !== 'piper' || localAllowed);
    $('voice-provider').replaceChildren();
    for (const provider of availableVoices) {
      const labels = {gemini: 'Gemini cloud voice', espeak: 'Lightweight demo voice', piper: 'Piper · enabled local service'};
      const option = new Option(`${labels[provider.id] || provider.name}${provider.available ? '' : ' · setup needed'}`, provider.id);
      option.disabled = !provider.available; $('voice-provider').add(option);
    }
    const preferred = currentVoice || (availableVoices.some(provider => provider.id === 'gemini' && provider.available) ? 'gemini' : renderer.default_voice_provider || 'espeak');
    if (availableVoices.some(provider => provider.id === preferred && provider.available)) $('voice-provider').value = preferred;
    else $('voice-provider').value = availableVoices.find(provider => provider.available)?.id || '';
    if (cloudReady && planner.ai_configured && !state.providerChosen && !state.draft) choose('provider', 'ai');
    const resourceMode = caps.resource_mode || renderer.resource_mode || 'gentle';
    $('resource-description').textContent = `${resourceMode === 'gentle' ? 'Gentle mode' : 'Workspace resources'} · one render worker · lightweight previews · ${localAllowed ? 'local models explicitly enabled' : 'no local AI models'}`;
    const captionReady = shorts.cloud_ready === undefined ? cloudReady : shorts.cloud_ready;
    $('shorts-cloud-help').textContent = shorts.provider && shorts.provider !== 'gemini' && localAllowed ? 'This workspace explicitly enables local transcription. Captioned exports use the configured local service.' : captionReady ? 'Captions use Gemini cloud transcription. Turn them off for simple cuts without transcription.' : 'Cloud captions need Gemini configured on the server. Turn captions off for simple cuts without transcription.';
    $('settings-grid').replaceChildren();
    const blocks = [
      {ready: planner.ready, title: 'Story planning', description: planner.reason || (planner.ai_configured ? 'Your cloud model plans the dialogue through LangGraph. Each scene is editable, with the planning steps kept alongside your draft.' : 'Manual scripts and curated demos are ready. Configure Gemini to create scripts for open-ended topics.'), code: planner.ai_configured ? `${planner.engine === 'gemini' || cloudReady ? 'Gemini' : 'Cloud model'} + LangGraph` : 'Manual + curated demo scripts'},
      {ready: renderer.ready, title: 'Video assembly', description: renderer.reason || (renderer.ready ? 'PNG presenters, captions, and video exports run locally. A single render worker keeps jobs from competing for your computer.' : 'Install FFmpeg and the studio Python dependencies to enable video assembly.'), code: 'Preview: 360p · 12 fps · one worker'},
      {ready: cloudReady, title: 'Gemini cloud', description: cloudReady ? 'Cloud scripts, natural voices, and transcription use your configured Gemini account. No local neural model is needed.' : 'Configure Gemini in the server environment for cloud scripting, voice, and captions. The lightweight demo voice remains an option.', code: cloudReady ? 'Voice + transcription run in the cloud' : 'GEMINI_API_KEY'}
    ];
    for (const block of blocks) {
      const card = element('section', 'capability-card', null, $('settings-grid'));
      const label = element('p', `status-label${block.ready ? '' : ' unavailable'}`, null, card);
      element('span', 'status-dot', null, label); label.append(document.createTextNode(block.ready ? 'Ready to use' : 'Setup needed'));
      element('h2', '', block.title, card); element('p', '', block.description, card); element('code', '', block.code, card);
    }
    $('capability-limitations').replaceChildren();
    if (caps.limitations?.length) { const ul = element('ul', '', null, $('capability-limitations')); caps.limitations.forEach(item => element('li', '', humanError(item), ul)); }
    renderCast(); modeChanged(); voiceChanged(); updatePreview();
  }
  async function loadCapabilities() {
    $('refresh-capabilities').disabled = true;
    try {
      state.capabilities = await api('/api/studio/capabilities', {}, 25000);
      renderCapabilities(state.capabilities); status('settings-status');
    } catch (error) {
      $('connection-label').textContent = 'Connection issue'; $('connection').querySelector('.status-dot').className = 'status-dot unavailable';
      status('settings-status', error.message, 'error');
      if (!state.capabilities) {
        $('settings-grid').replaceChildren();
        const card = element('section', 'capability-card', null, $('settings-grid'));
        element('h2', '', 'Workspace unavailable', card); element('p', '', 'Check that the studio server is running, then use Check again to reload its capabilities.', card);
      }
    } finally { $('refresh-capabilities').disabled = false; }
  }
  $('refresh-capabilities').addEventListener('click', loadCapabilities);
  async function poll() {
    clearTimeout(state.pollTimer);
    if (!document.hidden) await refreshJobs();
    state.pollTimer = setTimeout(poll, 4000);
  }
  document.addEventListener('visibilitychange', () => { if (!document.hidden) { clearTimeout(state.pollTimer); poll(); } });
  $('topic').maxLength = 240; $('draft-title').maxLength = 160;
  // SVG symbols are decorative; text labels provide accessible control names.
  $$('svg').forEach(svg => svg.setAttribute('aria-hidden', 'true'));
  $$('.nav-item').forEach(button => { button.title = names[button.dataset.view]; button.setAttribute('aria-label', names[button.dataset.view]); });
  addPresenters($('preview-presenters'));
  restore(); modeChanged(); sourceModeChanged(); updatePreview(); renderCast();
  changeView(location.hash.slice(1), false);
  Promise.allSettled([loadCapabilities(), loadLibrary(), poll()]);
})();
