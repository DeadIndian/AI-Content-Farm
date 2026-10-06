const byId = (id) => document.getElementById(id);

// Element references
const statusEl = byId("status");
const jobsEl = byId("jobs");
const refreshBtn = byId("refresh");
const clearQueueBtn = byId("clear-queue");

const presetSelect = byId("preset-select");
const presetSettings = byId("preset-settings");

const scriptModeAiBtn = byId("script-mode-ai");
const scriptModeManualBtn = byId("script-mode-manual");
const aiScriptSection = byId("ai-script-section");
const manualScriptSection = byId("manual-script-section");
const generateScriptBtn = byId("generate-script");
const generatedScriptView = byId("generated-script-view");
const generatedTitleEl = byId("generated-title");
const generatedTagsEl = byId("generated-tags");
const copyTitleBtn = byId("copy-title");
const copyTagsBtn = byId("copy-tags");

const scriptOverrideEl = byId("script-override");
const scriptHindiEl = byId("script-hindi");
const scriptTeluguEl = byId("script-telugu");
const manualScriptEl = byId("manual-script");
const renderJobBtn = byId("render-job");
const renderJobManualBtn = byId("render-job-manual");

const topicEl = byId("topic");

const languageEl = byId("language");
const voiceEl = byId("voice");
const orientationEl = byId("orientation");
const customSizeRow = byId("custom-size-row");
const customWidthEl = byId("custom-width");
const customHeightEl = byId("custom-height");
const bgSelect = byId("background-video");

const uploadAreaEl = byId("upload-area");
const uploadInput = byId("video-upload");
const uploadVideoBtn = byId("upload-videos");
const newFolderNameEl = byId("new-folder-name");
const createFolderBtn = byId("create-folder");
const folderSelectEl = byId("folder-select");
const renameFolderBtn = byId("rename-folder");
const youtubeURLInput = byId("youtube-url");
const importYouTubeBtn = byId("import-youtube");

const generatedVideosEl = byId("generated-videos");
const uploadedVideosEl = byId("uploaded-videos");
const refreshVideosBtn = byId("refresh-videos");

const mainTabBtns = document.querySelectorAll(".main-tab-btn");
const mainTabContents = document.querySelectorAll(".main-tab-content");

const inputDirEl = byId("input-dir");
const outputDirEl = byId("output-dir");
const defaultOrientationEl = byId("default-orientation");
const piperToggleEl = byId("piper-toggle");
const piperToggleLabelEl = byId("piper-toggle-label");
const ttsProviderEl = byId("tts-provider");
const defaultLanguageEl = byId("default-language");
const defaultVoiceEl = byId("default-voice");
const voicePreviewTextEl = byId("voice-preview-text");
const previewVoiceBtn = byId("preview-voice");
const voicePreviewPlayer = byId("voice-preview-player");
const saveSettingsBtn = byId("save-settings");
const saveVoiceSettingsBtn = byId("save-voice-settings");
const voicesListEl = byId("voices-list");
const savePresetBtn = byId("save-preset");
const deletePresetBtn = byId("delete-preset");

let availableVoices = [];
let savedPresets = [];
let assetFolders = [];
let previewObjectURL = "";
let generatedScripts = {};

function selectedProvider() {
	const provider = (ttsProviderEl?.value || "").trim().toLowerCase();
	if (provider === "piper" || provider === "auto") {
		return provider;
	}
	return "elevenlabs";
}

function updatePiperToggleLabel() {
	if (!piperToggleLabelEl) return;
	piperToggleLabelEl.textContent = piperToggleEl?.checked ? "On" : "Off";
}

// Setup functions
function setStatus(text, type = "info") {
	statusEl.textContent = text || "";
	statusEl.className = type ? `status-message ${type}` : "status-message";
}

function valueOrEmpty(el) {
	return el && typeof el.value !== "undefined" ? el.value : "";
}

function trimmedValue(el) {
	return valueOrEmpty(el).trim();
}

function setInputValue(el, value) {
	if (el && typeof el.value !== "undefined") {
		el.value = value;
	}
}

function settingsURL() {
	return `/api/settings?t=${Date.now()}`;
}

function encodePathSegments(pathValue) {
	return String(pathValue || "")
		.split("/")
		.filter(Boolean)
		.map((part) => encodeURIComponent(part))
		.join("/");
}

function syncGeneratedScriptsFromEditors() {
	const english = trimmedValue(scriptOverrideEl);
	const hindi = trimmedValue(scriptHindiEl);
	const telugu = trimmedValue(scriptTeluguEl);

	generatedScripts = {};
	if (english) generatedScripts.english = english;
	if (hindi) generatedScripts.hindi = hindi;
	if (telugu) generatedScripts.telugu = telugu;
}

function populateGeneratedScriptEditors() {
	setInputValue(scriptOverrideEl, generatedScripts.english || "");
	setInputValue(scriptHindiEl, generatedScripts.hindi || "");
	setInputValue(scriptTeluguEl, generatedScripts.telugu || "");
}

function setupPresetSelection() {
	presetSelect.addEventListener("change", () => {
		const presetIdx = parseInt(presetSelect.value, 10);
		if (presetSelect.value === "" || isNaN(presetIdx)) {
			presetSettings.style.display = "block";
			clearPresetForm();
			if (deletePresetBtn) deletePresetBtn.disabled = true;
		} else {
			const preset = savedPresets[presetIdx];
			if (!preset) {
				setStatus("Preset not found. Please refresh presets.", "error");
				return;
			}
			loadPresetIntoForm(preset);
			// Keep settings visible so preset actions (save/delete) remain accessible.
			presetSettings.style.display = "block";
			if (deletePresetBtn) deletePresetBtn.disabled = false;
		}
	});
}

function setupPresetSettings() {
	// Keep preset settings open by default for both new and selected presets.
	presetSettings.style.display = "block";
}

function setupScriptMode() {
	scriptModeAiBtn.addEventListener("click", () => {
		scriptModeAiBtn.classList.add("active");
		scriptModeManualBtn.classList.remove("active");
		aiScriptSection.style.display = "block";
		manualScriptSection.style.display = "none";
		generatedScriptView.style.display = "none";
	});

	scriptModeManualBtn.addEventListener("click", () => {
		scriptModeManualBtn.classList.add("active");
		scriptModeAiBtn.classList.remove("active");
		aiScriptSection.style.display = "none";
		manualScriptSection.style.display = "block";
		renderJobBtn.style.display = "none";
		renderJobManualBtn.style.display = "block";
		manualScriptEl?.focus();
	});
}

function setupMainTabs() {
	mainTabBtns.forEach((btn) => {
		btn.addEventListener("click", () => {
			const tabName = btn.getAttribute("data-tab");
			mainTabBtns.forEach((b) => b.classList.remove("active"));
			btn.classList.add("active");
			mainTabContents.forEach((content) => content.classList.remove("active"));
			const targetTab = byId(`tab-${tabName}`);
			if (targetTab) {
				targetTab.classList.add("active");
				// Optionally load tab-specific data on first click
				if (tabName === "jobs") loadJobs().catch(() => {});
				if (tabName === "videos" || tabName === "assets")
					loadVideos().catch(() => {});
			}
		});
	});
}

function setupUploadArea() {
	uploadAreaEl.addEventListener("click", () => uploadInput.click());

	uploadAreaEl.addEventListener("dragover", (e) => {
		e.preventDefault();
		uploadAreaEl.style.borderColor = "#000";
		uploadAreaEl.style.background = "#f0f0f0";
	});

	uploadAreaEl.addEventListener("dragleave", () => {
		uploadAreaEl.style.borderColor = "var(--border)";
		uploadAreaEl.style.background = "var(--bg-secondary)";
	});

	uploadAreaEl.addEventListener("drop", (e) => {
		e.preventDefault();
		uploadAreaEl.style.borderColor = "var(--border)";
		uploadAreaEl.style.background = "var(--bg-secondary)";
		uploadInput.files = e.dataTransfer.files;
	});
}

// Voice functions
function normalizeVoice(v) {
	return {
		key: v.key || v.name || "",
		name: v.name || v.key || "Unnamed",
		language_code: v.language_code || "",
		quality: v.quality || "",
	};
}

async function loadVoices() {
	try {
		const provider = selectedProvider();
		const query = provider ? `?provider=${encodeURIComponent(provider)}` : "";
		const resp = await fetch(`/api/voices${query}`);
		if (!resp.ok) {
			const data = await resp.json().catch(() => ({}));
			throw new Error(data.error || "Failed to load voices");
		}
		const data = await resp.json();
		availableVoices = (data.voices || []).map(normalizeVoice);

		const languageSet = new Set();
		for (const voice of availableVoices) {
			if (voice.language_code) {
				languageSet.add(voice.language_code);
			}
		}
		for (const language of data.languages || []) {
			if (language) {
				languageSet.add(language);
			}
		}

		const languages = Array.from(languageSet).sort();
		renderLanguageDropdown(languageEl, languages, "Auto");
		renderLanguageDropdown(defaultLanguageEl, languages, "Auto");
		refreshVoiceDropdowns();
		renderVoicesList();
	} catch (e) {
		availableVoices = [];
		renderLanguageDropdown(languageEl, [], "Auto");
		renderLanguageDropdown(defaultLanguageEl, [], "Auto");
		refreshVoiceDropdowns();
		const provider = selectedProvider();
		voicesListEl.innerHTML = `<p class="empty-state">No ${provider} voices available</p>`;
		setStatus(`Failed to load ${provider} voices: ${e.message}`, "error");
	}
}

function renderLanguageDropdown(selectEl, languages, defaultLabel) {
	const prev = selectEl.value;
	selectEl.innerHTML = "";
	selectEl.appendChild(new Option(defaultLabel, ""));
	languages.forEach((lang) => selectEl.appendChild(new Option(lang, lang)));
	if (prev && languages.includes(prev)) {
		selectEl.value = prev;
	}
}

function filteredVoicesByLanguage(language) {
	if (!language) {
		return availableVoices;
	}
	const filtered = availableVoices.filter((v) => v.language_code === language);
	if (filtered.length > 0) {
		return filtered;
	}
	const provider = selectedProvider();
	if (provider === "elevenlabs" || provider === "auto") {
		// ElevenLabs multilingual models can synthesize multiple languages per voice.
		return availableVoices;
	}
	return filtered;
}

function refreshVoiceDropdowns() {
	const mainLang = languageEl.value.trim();
	const defaultLang = defaultLanguageEl.value.trim();
	renderVoiceDropdown(voiceEl, mainLang, "Default");
	renderVoiceDropdown(defaultVoiceEl, defaultLang, "None");
	renderVoicesList();
}

function renderVoiceDropdown(selectEl, language, emptyLabel) {
	const prev = selectEl.value;
	const voices = filteredVoicesByLanguage(language);
	selectEl.innerHTML = "";
	selectEl.appendChild(new Option(emptyLabel, ""));
	voices.forEach((voice) => {
		const suffix = voice.quality ? ` (${voice.quality})` : "";
		selectEl.appendChild(new Option(`${voice.name}${suffix}`, voice.key));
	});
	if (prev && voices.some((v) => v.key === prev)) {
		selectEl.value = prev;
	}
}

function renderVoicesList() {
	const voices = filteredVoicesByLanguage(defaultLanguageEl.value.trim());
	if (!voices.length) {
		voicesListEl.innerHTML =
			'<p class="empty-state">No voices for this language</p>';
		return;
	}

	voicesListEl.innerHTML = voices
		.map(
			(voice) => `
		<div class="list-item">
			<div>
				<div class="list-item-name">${voice.name}</div>
				<div class="list-item-meta">${voice.language_code || "n/a"}${voice.quality ? ` | ${voice.quality}` : ""}</div>
			</div>
			<div class="list-actions">
				<button class="list-item-action" data-use="${voice.key}">Use</button>
				<button class="list-item-action" data-preview="${voice.key}">Preview</button>
			</div>
		</div>
	`,
		)
		.join("");

	voicesListEl.querySelectorAll("button[data-use]").forEach((btn) => {
		btn.addEventListener("click", () => {
			const key = btn.getAttribute("data-use") || "";
			defaultVoiceEl.value = key;
			voiceEl.value = key;
			setStatus("Voice selected.", "success");
		});
	});

	voicesListEl.querySelectorAll("button[data-preview]").forEach((btn) => {
		btn.addEventListener("click", () => {
			const key = btn.getAttribute("data-preview") || "";
			previewVoice(key).catch((e) => setStatus(e.message, "error"));
		});
	});
}

async function previewVoice(voiceKey) {
	const text =
		voicePreviewTextEl.value.trim() || "This is a quick Piper voice preview.";
	const language = defaultLanguageEl.value.trim() || languageEl.value.trim();
	previewVoiceBtn.disabled = true;
	setStatus("Generating voice preview...");
	try {
		const resp = await fetch("/api/voices/preview", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				text,
				voice: voiceKey || defaultVoiceEl.value,
				language,
			}),
		});
		if (!resp.ok) {
			const data = await resp.json().catch(() => ({}));
			throw new Error(data.error || "Voice preview failed");
		}
		const blob = await resp.blob();
		if (previewObjectURL) {
			URL.revokeObjectURL(previewObjectURL);
		}
		previewObjectURL = URL.createObjectURL(blob);
		voicePreviewPlayer.src = previewObjectURL;
		await voicePreviewPlayer.play().catch(() => {});
		setStatus("Voice preview ready.", "success");
	} finally {
		previewVoiceBtn.disabled = false;
	}
}

// Preset functions
function clearPresetForm() {
	setInputValue(topicEl, "");
	setInputValue(languageEl, "");
	setInputValue(voiceEl, "");
	setInputValue(orientationEl, "portrait");
	setInputValue(customWidthEl, "");
	setInputValue(customHeightEl, "");
	setInputValue(bgSelect, "");
	setInputValue(scriptOverrideEl, "");
	setInputValue(scriptHindiEl, "");
	setInputValue(scriptTeluguEl, "");
	setInputValue(manualScriptEl, "");
	setInputValue(generatedTitleEl, "");
	setInputValue(generatedTagsEl, "");
	generatedScripts = {};
	scriptModeAiBtn.classList.add("active");
	scriptModeManualBtn.classList.remove("active");
	aiScriptSection.style.display = "block";
	manualScriptSection.style.display = "none";
	generatedScriptView.style.display = "none";
	renderJobBtn.style.display = "block";
	renderJobManualBtn.style.display = "none";
}

function loadPresetIntoForm(preset) {
	setInputValue(topicEl, preset.topic || preset.prompt || "");
	languageEl.value = preset.language || "";
	refreshVoiceDropdowns();
	voiceEl.value = preset.voice || "";
	orientationEl.value = preset.orientation || "portrait";
	customWidthEl.value = preset.custom_width || "";
	customHeightEl.value = preset.custom_height || "";
	bgSelect.value = preset.background_video || "";

	const presetScript = String(preset.script_override || "").trim();
	setInputValue(scriptOverrideEl, presetScript);
	setInputValue(manualScriptEl, presetScript);
	generatedScripts = {};
	if (presetScript) {
		generatedScripts.english = presetScript;
		populateGeneratedScriptEditors();
		generatedScriptView.style.display = "block";
	} else {
		setInputValue(scriptHindiEl, "");
		setInputValue(scriptTeluguEl, "");
		generatedScriptView.style.display = "none";
	}

	updateCustomSizeVisibility();
	setStatus(`Preset "${preset.name}" loaded.`, "success");
}

function renderPresetsList() {
	presetSelect.innerHTML =
		'<option value="">-- Create New / One-Time --</option>';
	savedPresets.forEach((preset, idx) => {
		presetSelect.appendChild(new Option(preset.name, idx));
	});
	if (deletePresetBtn) {
		deletePresetBtn.disabled = true;
	}
}

async function deletePreset() {
	const presetIdx = parseInt(presetSelect.value, 10);
	if (presetSelect.value === "" || Number.isNaN(presetIdx)) {
		setStatus("Select a preset to delete.", "error");
		return;
	}

	if (!savedPresets[presetIdx]) {
		setStatus("Selected preset no longer exists.", "error");
		return;
	}

	const presetName = savedPresets[presetIdx].name || "this preset";
	if (!confirm(`Delete preset \"${presetName}\"?`)) {
		return;
	}

	savedPresets.splice(presetIdx, 1);

	try {
		const resp = await fetch(settingsURL(), {
			cache: "no-store",
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ prompt_presets: savedPresets }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) {
			throw new Error(data.error || "Failed to delete preset");
		}
		savedPresets = Array.isArray(data.prompt_presets)
			? data.prompt_presets
			: [];
		renderPresetsList();
		presetSelect.value = "";
		presetSettings.style.display = "block";
		clearPresetForm();
		setStatus("Preset deleted.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function savePreset() {
	const presetName = prompt("Enter preset name:");
	if (!presetName) return;
	const name = presetName.trim();
	if (!name) return;

	if (!topicEl || !voiceEl || !languageEl || !orientationEl) {
		setStatus(
			"UI fields are not fully loaded. Please refresh and retry.",
			"error",
		);
		return;
	}

	const preset = {
		name,
		prompt: trimmedValue(topicEl),
		topic: trimmedValue(topicEl),
		script_override: trimmedValue(scriptOverrideEl),
		voice: valueOrEmpty(voiceEl),
		language: valueOrEmpty(languageEl),
		orientation: valueOrEmpty(orientationEl),
		custom_width: Number(valueOrEmpty(customWidthEl) || 0),
		custom_height: Number(valueOrEmpty(customHeightEl) || 0),
		background_video: valueOrEmpty(bgSelect),
	};

	savedPresets = savedPresets.filter((p) => p.name !== name);
	savedPresets.unshift(preset);

	try {
		const resp = await fetch(settingsURL(), {
			cache: "no-store",
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ prompt_presets: savedPresets }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) {
			throw new Error(data.error || "Failed to save preset");
		}
		savedPresets = Array.isArray(data.prompt_presets)
			? data.prompt_presets
			: [];
		renderPresetsList();
		if (savedPresets.length > 0) {
			presetSelect.value = "0";
			loadPresetIntoForm(savedPresets[0]);
			if (deletePresetBtn) deletePresetBtn.disabled = false;
		}
		setStatus("Preset saved.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

// Script and rendering
function collectJobPayload() {
	syncGeneratedScriptsFromEditors();

	const scriptOverride =
		trimmedValue(scriptOverrideEl) || trimmedValue(manualScriptEl);
	const scripts = { ...generatedScripts };
	if (scriptOverride) {
		scripts.english = scriptOverride;
	}

	const payload = {
		topic: trimmedValue(topicEl),
		prompt: trimmedValue(topicEl),
		script_override: scriptOverride,
		voice: trimmedValue(voiceEl),
		language: trimmedValue(languageEl),
		orientation: valueOrEmpty(orientationEl),
		custom_width: Number(valueOrEmpty(customWidthEl) || 0),
		custom_height: Number(valueOrEmpty(customHeightEl) || 0),
		background_video: valueOrEmpty(bgSelect),
	};
	if (Object.keys(scripts).length > 0) {
		payload.scripts = scripts;
	}
	return payload;
}

async function generateScriptDraft() {
	setStatus("Generating script draft...");
	generateScriptBtn.disabled = true;
	try {
		const resp = await fetch("/v1/scripts/generate", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(collectJobPayload()),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Script generation failed");

		// Handle JSON response - try multiple parsing paths
		let title = "";
		let tags = "";
		let script = "";
		let scripts = {};

		// Path 1: Direct JSON response with title, script, tags fields
		if (data.title && (data.script || data.english || data.scripts)) {
			title = data.title || data.vid_title || "";
			tags = Array.isArray(data.tags)
				? data.tags.join(", ")
				: Array.isArray(data.vid_tags)
					? data.vid_tags.join(", ")
					: typeof data.tags === "string"
						? data.tags
						: "";
			script = data.english || data.script || "";
			scripts = {
				english: data.english || data.script || "",
				hindi: data.hindi || "",
				telugu: data.telugu || "",
			};
		}
		// Path 2: Script field contains JSON string
		else if (typeof data.script === "string") {
			try {
				// Clean markdown code blocks if present
				let jsonStr = data.script.trim();
				if (jsonStr.startsWith("```json")) {
					jsonStr = jsonStr.replace(/^```json\n?/, "").replace(/\n?```$/, "");
				} else if (jsonStr.startsWith("```")) {
					jsonStr = jsonStr.replace(/^```\n?/, "").replace(/\n?```$/, "");
				}

				const parsed = JSON.parse(jsonStr);
				title = parsed.title || parsed.vid_title || "";
				tags = Array.isArray(parsed.tags)
					? parsed.tags.join(", ")
					: Array.isArray(parsed.vid_tags)
						? parsed.vid_tags.join(", ")
						: typeof parsed.tags === "string"
							? parsed.tags
							: "";
				script = parsed.english || parsed.script || "";
				scripts = {
					english: parsed.english || parsed.script || "",
					hindi: parsed.hindi || "",
					telugu: parsed.telugu || "",
				};
			} catch (e) {
				// If not JSON, treat entire response as script
				script = data.script;
				scripts = { english: script };
			}
		}
		// Path 3: Fallback
		else {
			script = JSON.stringify(data);
			scripts = { english: script };
		}

		if (data.scripts && typeof data.scripts === "object") {
			scripts = {
				...scripts,
				english: data.scripts.english || scripts.english || "",
				hindi: data.scripts.hindi || scripts.hindi || "",
				telugu: data.scripts.telugu || scripts.telugu || "",
			};
		}
		generatedScripts = Object.fromEntries(
			Object.entries(scripts).filter(([, value]) => String(value || "").trim()),
		);

		generatedTitleEl.value = title;
		generatedTagsEl.value = tags;
		if (!generatedScripts.english && script) {
			generatedScripts.english = script;
		}
		populateGeneratedScriptEditors();
		generatedScriptView.style.display = "block";
		setStatus("Draft ready. Review and edit if needed.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		generateScriptBtn.disabled = false;
	}
}

async function renderJobRequest() {
	setStatus("Queueing job...");
	renderJobBtn.disabled = true;
	renderJobManualBtn.disabled = true;
	try {
		const resp = await fetch("/v1/jobs", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(collectJobPayload()),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Failed to queue job");
		setStatus("Job queued successfully.", "success");
		await loadJobs();
		clearPresetForm();
		presetSelect.value = "";
		presetSettings.style.display = "block";
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		renderJobBtn.disabled = false;
		renderJobManualBtn.disabled = false;
	}
}

// Jobs
function badgeClass(status) {
	const statusLower = status?.toLowerCase() || "queued";
	return `badge-${statusLower}`;
}

function renderJob(job) {
	const el = document.createElement("article");
	el.className = "job";

	const title = job.request?.topic || job.request?.prompt || "Untitled";
	const script = job.script || "";
	const err = job.error_message
		? `<div class="job-error">${job.error_message}</div>`
		: "";
	const output = job.output_path
		? `<a href="${job.output_path}" target="_blank" rel="noopener" class="job-link">Open Video</a>`
		: "";
	const rerunBtn =
		String(job.status || "").toLowerCase() === "failed"
			? `<button class="btn btn-secondary btn-sm rerun-job-btn" data-job-id="${job.id}">Re-run</button>`
			: "";

	el.innerHTML = `
		<div class="job-meta">
			<span>${new Date(job.created_at).toLocaleString()}</span>
			<span class="badge ${badgeClass(job.status)}">${job.status?.toUpperCase() || "QUEUED"}</span>
		</div>
		<h3>${title}</h3>
		<pre>${script || "No script"}</pre>
		${err}
		${output}
		${rerunBtn}
	`;
	return el;
}

async function rerunFailedJob(jobId, buttonEl) {
	if (!jobId) return;
	if (buttonEl) buttonEl.disabled = true;
	setStatus("Re-queueing failed job...");
	try {
		const resp = await fetch(`/v1/jobs/${encodeURIComponent(jobId)}/rerun`, {
			method: "POST",
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Failed to re-run job");
		setStatus("Failed job re-queued.", "success");
		await loadJobs();
	} catch (e) {
		setStatus(e.message, "error");
		if (buttonEl) buttonEl.disabled = false;
	}
}

async function loadJobs() {
	try {
		const resp = await fetch("/v1/jobs");
		if (!resp.ok) throw new Error("Failed to load jobs");
		const jobs = await resp.json();
		jobsEl.innerHTML = "";

		if (!jobs || jobs.length === 0) {
			jobsEl.innerHTML = '<p class="empty-state">No jobs yet</p>';
			return;
		}

		jobs.forEach((job) => jobsEl.appendChild(renderJob(job)));
		jobsEl.querySelectorAll(".rerun-job-btn").forEach((btn) => {
			btn.addEventListener("click", () =>
				rerunFailedJob(btn.getAttribute("data-job-id"), btn),
			);
		});
	} catch (e) {
		setStatus(e.message, "error");
	}
}

// Videos
async function loadVideos() {
	try {
		const [uploadedResp, generatedResp] = await Promise.all([
			fetch("/api/videos"),
			fetch("/api/videos/generated"),
		]);
		if (!uploadedResp.ok) throw new Error("Failed to load uploaded videos");
		if (!generatedResp.ok) throw new Error("Failed to load generated videos");

		const [videos, generated] = await Promise.all([
			uploadedResp.json(),
			generatedResp.json(),
		]);
		const foldersResp = await fetch("/api/videos/folders");
		if (!foldersResp.ok) throw new Error("Failed to load folders");
		assetFolders = await foldersResp.json();
		renderFolderSelect(assetFolders || []);

		renderBackgroundOptions(
			Array.isArray(videos) ? videos : [],
			assetFolders || [],
		);
		renderUploadedVideosList(videos || [], assetFolders || []);
		renderGeneratedVideosList(generated || []);
	} catch (e) {
		setStatus(e.message, "error");
	}
}

function renderBackgroundOptions(videos, folders) {
	bgSelect.innerHTML = '<option value="">Auto (Random)</option>';
	if (
		(!Array.isArray(videos) || videos.length === 0) &&
		(!Array.isArray(folders) || folders.length === 0)
	) {
		return;
	}

	const folderSet = new Set((folders || []).map((f) => f.path).filter(Boolean));
	const grouped = new Map();
	const loose = [];

	videos.forEach((v) => {
		const folder = (v.folder || "").trim();
		if (!v.path) return;

		if (!folder || !folderSet.has(folder)) {
			loose.push(v);
			return;
		}
		if (!grouped.has(folder)) {
			grouped.set(folder, []);
		}
		grouped.get(folder).push(v);
	});

	const folderNames = Array.from(grouped.keys()).sort((a, b) =>
		a.localeCompare(b),
	);
	folderNames.forEach((folder) => {
		const group = document.createElement("optgroup");
		group.label = folder;
		grouped.get(folder).forEach((v) => {
			group.appendChild(new Option(v.path, v.path));
		});
		bgSelect.appendChild(group);
	});

	const emptyFolders = (folders || [])
		.filter((folder) => folder.path && !grouped.has(folder.path))
		.sort((a, b) => a.path.localeCompare(b.path));
	if (emptyFolders.length > 0) {
		const emptyGroup = document.createElement("optgroup");
		emptyGroup.label = "Empty Folders";
		emptyFolders.forEach((folder) => {
			emptyGroup.appendChild(new Option(folder.path, folder.path));
		});
		bgSelect.appendChild(emptyGroup);
	}

	if (loose.length > 0) {
		const looseGroup = document.createElement("optgroup");
		looseGroup.label = "Ungrouped";
		loose.forEach((v) => {
			looseGroup.appendChild(new Option(v.path, v.path));
		});
		bgSelect.appendChild(looseGroup);
	}
}

function renderFolderSelect(folders) {
	if (!folderSelectEl) return;
	folderSelectEl.innerHTML = '<option value="">Select folder</option>';
	(folders || []).forEach((folder) => {
		if (!folder.path) return;
		folderSelectEl.appendChild(new Option(folder.path, folder.path));
	});
}

function renderGeneratedVideosList(videos) {
	if (!videos || videos.length === 0) {
		generatedVideosEl.innerHTML =
			'<p class="empty-state">No generated videos yet</p>';
		return;
	}

	const latest = videos.slice(0, 3);
	const remaining = videos.slice(3);

	generatedVideosEl.innerHTML = `
		<div class="asset-folder-heading">Latest</div>
		${latest.map((v) => renderGeneratedVideoCard(v)).join("")}
		${remaining.length > 0 ? `<div class="asset-folder-heading">All Generated</div>${remaining.map((v) => renderGeneratedVideoCard(v)).join("")}` : ""}
	`;

	generatedVideosEl
		.querySelectorAll(".video-action-btn.download-generated")
		.forEach((btn) => {
			btn.addEventListener("click", () => {
				const url = btn.getAttribute("data-url");
				const name = btn.getAttribute("data-video") || "video.mp4";
				if (!url) return;

				const link = document.createElement("a");
				link.href = url;
				link.download = name;
				document.body.appendChild(link);
				link.click();
				document.body.removeChild(link);
			});
		});

	generatedVideosEl
		.querySelectorAll(".video-action-btn.delete-generated")
		.forEach((btn) => {
			btn.addEventListener("click", () =>
				deleteGeneratedVideo(btn.getAttribute("data-video")),
			);
		});
}

function renderGeneratedVideoCard(v) {
	return `
		<div class="video-item preview-only">
			<div class="video-item-main">
				<span class="video-item-name">${v.name}</span>
				<video class="video-preview" controls preload="metadata" src="${v.url}"></video>
			</div>
			<div class="video-item-actions">
				<button class="video-action-btn download-generated" data-url="${v.url}" data-video="${v.name}">Download</button>
				<button class="video-action-btn delete-generated" data-video="${v.name}">Delete</button>
			</div>
		</div>
	`;
}

function renderUploadedVideosList(videos, folders) {
	if (!videos || videos.length === 0) {
		uploadedVideosEl.innerHTML =
			'<p class="empty-state">No uploaded videos</p>';
		return;
	}

	const grouped = new Map();
	const loose = [];
	videos.forEach((v) => {
		const folder = (v.folder || "").trim();
		if (!folder) {
			loose.push(v);
			return;
		}
		if (!grouped.has(folder)) {
			grouped.set(folder, []);
		}
		grouped.get(folder).push(v);
	});
	const folderOptions = (folders || [])
		.map((folder) => `<option value="${folder.path}">${folder.path}</option>`)
		.join("");

	const sections = [];
	Array.from(grouped.keys())
		.sort((a, b) => a.localeCompare(b))
		.forEach((folder) => {
			sections.push(
				`<div class="asset-folder-heading">${folder}</div>` +
					grouped
						.get(folder)
						.map((v) => renderUploadedVideoCard(v, folderOptions))
						.join(""),
			);
		});

	if (loose.length > 0) {
		sections.push(
			'<div class="asset-folder-heading">Ungrouped</div>' +
				loose.map((v) => renderUploadedVideoCard(v, folderOptions)).join(""),
		);
	}

	uploadedVideosEl.innerHTML = sections.join("");

	uploadedVideosEl
		.querySelectorAll(".video-folder-select")
		.forEach((select) => {
			const videoPath = select.getAttribute("data-folder-for") || "";
			const current = videos.find(
				(v) => (v.path || v.name || "") === videoPath,
			);
			if (current && current.folder) {
				select.value = current.folder;
			}
		});

	uploadedVideosEl.querySelectorAll(".video-action-btn").forEach((btn) => {
		const videoPath = btn.getAttribute("data-video");
		if (btn.classList.contains("rename")) {
			btn.addEventListener("click", () => renameVideo(videoPath));
		} else if (btn.classList.contains("download")) {
			btn.addEventListener("click", () => downloadVideo(videoPath));
		} else if (btn.classList.contains("move")) {
			btn.addEventListener("click", () => moveVideo(videoPath, btn));
		} else if (btn.classList.contains("delete")) {
			btn.addEventListener("click", () => deleteVideo(videoPath));
		}
	});
}

function renderUploadedVideoCard(v, folderOptions) {
	const value = v.path || v.name;
	const display = v.folder ? `${v.folder}/${v.name}` : v.name;
	const moveOptions = `<option value="">Root</option>${folderOptions || ""}`;
	return `
		<div class="video-item">
			<div class="video-item-main">
				<span class="video-item-name">${display}</span>
				<video class="video-preview" controls preload="metadata" src="${v.url}"></video>
			</div>
			<div class="video-item-actions video-folder-actions">
				<select class="video-folder-select" data-folder-for="${value}">
					${moveOptions}
				</select>
				<button class="video-action-btn move" data-video="${value}">Move</button>
			</div>
			<div class="video-item-actions">
				<button class="video-action-btn rename" data-video="${value}">Rename</button>
				<button class="video-action-btn download" data-video="${value}">Download</button>
				<button class="video-action-btn delete" data-video="${value}">Delete</button>
			</div>
		</div>
	`;
}

async function renameVideo(oldName) {
	const newName = prompt("Enter new name:", oldName);
	if (!newName || newName === oldName) return;
	setStatus("Renaming video...");
	try {
		const resp = await fetch("/api/videos/rename", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ old_name: oldName, new_name: newName }),
		});
		if (!resp.ok) {
			const data = await resp.json().catch(() => ({}));
			throw new Error(data.error || "Rename failed");
		}
		await loadVideos();
		setStatus("Video renamed.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function downloadVideo(videoName) {
	window.location.href = `/inputs/${encodePathSegments(videoName)}`;
}

async function moveVideo(videoPath, buttonEl) {
	const itemEl = buttonEl?.closest(".video-item");
	const select = itemEl?.querySelector(".video-folder-select");
	if (!select) return;
	const folder = (select.value || "").trim();
	setStatus("Moving video...");
	try {
		const resp = await fetch("/api/videos/move", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ source_path: videoPath, folder }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Move failed");
		await loadVideos();
		setStatus("Video moved.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function deleteVideo(videoName) {
	if (!confirm(`Delete "${videoName}"?`)) return;
	setStatus("Deleting video...");
	try {
		const resp = await fetch("/api/videos/delete", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: videoName }),
		});
		if (!resp.ok) {
			const data = await resp.json().catch(() => ({}));
			throw new Error(data.error || "Delete failed");
		}
		await loadVideos();
		setStatus("Video deleted.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function deleteGeneratedVideo(videoName) {
	if (!confirm(`Delete "${videoName}"?`)) return;
	setStatus("Deleting generated video...");
	try {
		const resp = await fetch("/api/videos/generated/delete", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: videoName }),
		});
		if (!resp.ok) {
			const data = await resp.json().catch(() => ({}));
			throw new Error(data.error || "Delete failed");
		}
		await loadVideos();
		setStatus("Generated video deleted.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function createFolder() {
	const folderName = (newFolderNameEl.value || "").trim();
	if (!folderName) {
		setStatus("Enter a folder name first.", "error");
		return;
	}
	createFolderBtn.disabled = true;
	setStatus("Creating folder...");
	try {
		const resp = await fetch("/api/videos/folders", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ name: folderName }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Create folder failed");
		newFolderNameEl.value = "";
		await loadVideos();
		setStatus("Folder created.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		createFolderBtn.disabled = false;
	}
}

async function renameFolder() {
	const oldName = (folderSelectEl.value || "").trim();
	if (!oldName) {
		setStatus("Select a folder first.", "error");
		return;
	}
	const newName = prompt("Enter new folder name:", oldName);
	if (!newName || !newName.trim() || newName.trim() === oldName) return;
	renameFolderBtn.disabled = true;
	setStatus("Renaming folder...");
	try {
		const resp = await fetch("/api/videos/folders/rename", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ old_name: oldName, new_name: newName.trim() }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Rename folder failed");
		await loadVideos();
		setStatus("Folder renamed.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		renameFolderBtn.disabled = false;
	}
}

async function uploadVideos() {
	if (!uploadInput.files || !uploadInput.files.length) {
		setStatus("Select one or more files first.", "error");
		return;
	}
	setStatus("Uploading videos...");
	uploadVideoBtn.disabled = true;
	try {
		const fd = new FormData();
		for (const f of uploadInput.files) fd.append("videos", f);
		const resp = await fetch("/api/videos/upload", {
			method: "POST",
			body: fd,
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Upload failed");
		setStatus(
			`Uploaded ${data.uploaded?.length || uploadInput.files.length} file(s).`,
			"success",
		);
		uploadInput.value = "";
		await loadVideos();
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		uploadVideoBtn.disabled = false;
	}
}

async function importYouTubeVideo() {
	const url = (youtubeURLInput.value || "").trim();
	if (!url) {
		setStatus("Paste a YouTube URL first.", "error");
		return;
	}

	importYouTubeBtn.disabled = true;
	setStatus("Queuing podcast Shorts...");
	try {
		const resp = await fetch("/api/shorts", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ url }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) {
			throw new Error(data.error || "YouTube import failed");
		}
		youtubeURLInput.value = "";
		document.querySelector('[data-tab="shorts"]').click();
		setStatus("Podcast queued. Follow progress in Podcast Shorts.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		importYouTubeBtn.disabled = false;
	}
}

// Settings
async function loadSettings() {
	try {
		const resp = await fetch(settingsURL(), { cache: "no-store" });
		if (!resp.ok) throw new Error("Failed to load settings");
		const s = await resp.json();

		inputDirEl.value = s.input_videos_dir || "";
		outputDirEl.value = s.output_videos_dir || "";
		defaultOrientationEl.value = s.default_video_orientation || "portrait";
		if (ttsProviderEl) {
			ttsProviderEl.value = (s.tts_provider || "elevenlabs").toLowerCase();
		}
		if (piperToggleEl) {
			piperToggleEl.checked =
				Boolean(s.piper_enabled) || selectedProvider() === "piper";
			updatePiperToggleLabel();
		}
		defaultLanguageEl.value = s.default_language || "";
		languageEl.value = s.default_language || "";
		refreshVoiceDropdowns();
		defaultVoiceEl.value = s.default_voice || "";
		if (!voiceEl.value.trim() && s.default_voice) {
			voiceEl.value = s.default_voice;
		}
		savedPresets = Array.isArray(s.prompt_presets) ? s.prompt_presets : [];
		renderPresetsList();
		updateCustomSizeVisibility();
	} catch (e) {
		setStatus(e.message, "error");
	}
}

async function saveSettings() {
	setStatus("Saving settings...");
	saveSettingsBtn.disabled = true;
	try {
		const payload = {
			input_videos_dir: inputDirEl.value.trim(),
			output_videos_dir: outputDirEl.value.trim(),
			default_video_orientation: defaultOrientationEl.value,
			tts_provider: selectedProvider(),
			piper_enabled: Boolean(piperToggleEl?.checked),
			default_voice: defaultVoiceEl.value.trim(),
			default_language: defaultLanguageEl.value.trim(),
		};
		const resp = await fetch(settingsURL(), {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Failed to save settings");
		setStatus("Settings saved.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		saveSettingsBtn.disabled = false;
	}
}

async function savePiperEnabled() {
	if (!piperToggleEl) return;
	piperToggleEl.disabled = true;
	const enabled = Boolean(piperToggleEl.checked);
	setStatus(enabled ? "Enabling Piper..." : "Disabling Piper...");
	try {
		const resp = await fetch(settingsURL(), {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ piper_enabled: enabled }),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) {
			throw new Error(data.error || "Failed to update Piper state");
		}
		updatePiperToggleLabel();
		setStatus(enabled ? "Piper enabled." : "Piper disabled.", "success");
	} catch (e) {
		piperToggleEl.checked = !piperToggleEl.checked;
		updatePiperToggleLabel();
		setStatus(e.message, "error");
	} finally {
		piperToggleEl.disabled = false;
	}
}

async function saveTTSProviderSelection() {
	if (!ttsProviderEl) return;
	const previousPiperEnabled = Boolean(piperToggleEl?.checked);
	ttsProviderEl.disabled = true;
	const provider = selectedProvider();
	const payload = { tts_provider: provider };
	if (provider === "piper") {
		payload.piper_enabled = true;
		if (piperToggleEl && !piperToggleEl.checked) {
			piperToggleEl.checked = true;
			updatePiperToggleLabel();
		}
	}
	setStatus(`Switching TTS provider to ${provider}...`);
	try {
		const resp = await fetch(settingsURL(), {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) {
			throw new Error(data.error || "Failed to switch TTS provider");
		}
		if (piperToggleEl && typeof data.piper_enabled === "boolean") {
			piperToggleEl.checked = data.piper_enabled;
			updatePiperToggleLabel();
		}
		await loadVoices();
		setStatus("TTS provider updated.", "success");
	} catch (e) {
		if (piperToggleEl) {
			piperToggleEl.checked = previousPiperEnabled;
			updatePiperToggleLabel();
		}
		setStatus(e.message, "error");
	} finally {
		ttsProviderEl.disabled = false;
	}
}

async function saveVoiceSettings() {
	saveVoiceSettingsBtn.disabled = true;
	try {
		const payload = {
			default_voice: defaultVoiceEl.value.trim(),
			default_language: defaultLanguageEl.value.trim(),
		};
		const resp = await fetch(settingsURL(), {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok)
			throw new Error(data.error || "Failed to save voice settings");
		setStatus("Voice settings saved.", "success");
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		saveVoiceSettingsBtn.disabled = false;
	}
}

function updateCustomSizeVisibility() {
	customSizeRow.style.display =
		orientationEl.value === "custom" ? "grid" : "none";
}

async function clearQueue() {
	if (!confirm("Are you sure you want to clear all jobs from the queue?")) {
		return;
	}
	setStatus("Clearing job queue...");
	clearQueueBtn.disabled = true;
	try {
		const resp = await fetch("/v1/jobs", { method: "DELETE" });
		const data = await resp.json().catch(() => ({}));
		if (!resp.ok) throw new Error(data.error || "Failed to clear queue");
		setStatus("Job queue cleared.", "success");
		await loadJobs();
	} catch (e) {
		setStatus(e.message, "error");
	} finally {
		clearQueueBtn.disabled = false;
	}
}

// Event listeners
generateScriptBtn?.addEventListener("click", () => generateScriptDraft());
renderJobBtn?.addEventListener("click", () => renderJobRequest());
renderJobManualBtn?.addEventListener("click", () => renderJobRequest());
saveSettingsBtn?.addEventListener("click", () => saveSettings());
saveVoiceSettingsBtn?.addEventListener("click", () => saveVoiceSettings());
savePresetBtn?.addEventListener("click", () =>
	savePreset().catch((e) => setStatus(e.message, "error")),
);
deletePresetBtn?.addEventListener("click", () =>
	deletePreset().catch((e) => setStatus(e.message, "error")),
);
previewVoiceBtn?.addEventListener("click", () =>
	previewVoice(defaultVoiceEl.value || voiceEl.value).catch((e) =>
		setStatus(e.message, "error"),
	),
);
uploadVideoBtn?.addEventListener("click", () => uploadVideos());
importYouTubeBtn?.addEventListener("click", () => importYouTubeVideo());
createFolderBtn?.addEventListener("click", () =>
	createFolder().catch((e) => setStatus(e.message, "error")),
);
renameFolderBtn?.addEventListener("click", () =>
	renameFolder().catch((e) => setStatus(e.message, "error")),
);
refreshVideosBtn?.addEventListener("click", () =>
	loadVideos().catch((e) => setStatus(e.message, "error")),
);
refreshBtn?.addEventListener("click", () =>
	loadJobs().catch((e) => setStatus(e.message, "error")),
);
clearQueueBtn?.addEventListener("click", () =>
	clearQueue().catch((e) => setStatus(e.message, "error")),
);
orientationEl?.addEventListener("change", updateCustomSizeVisibility);
languageEl?.addEventListener("change", refreshVoiceDropdowns);
piperToggleEl?.addEventListener("change", () =>
	savePiperEnabled().catch((e) => setStatus(e.message, "error")),
);
ttsProviderEl?.addEventListener("change", () =>
	saveTTSProviderSelection().catch((e) => setStatus(e.message, "error")),
);
defaultLanguageEl?.addEventListener("change", () => {
	if (!languageEl.value) {
		languageEl.value = defaultLanguageEl.value;
	}
	refreshVoiceDropdowns();
});

// Copy button event listeners
copyTitleBtn?.addEventListener("click", () => {
	if (generatedTitleEl.value) {
		navigator.clipboard.writeText(generatedTitleEl.value);
		setStatus("Title copied to clipboard!", "success");
	}
});

copyTagsBtn?.addEventListener("click", () => {
	if (generatedTagsEl.value) {
		navigator.clipboard.writeText(generatedTagsEl.value);
		setStatus("Tags copied to clipboard!", "success");
	}
});

scriptOverrideEl?.addEventListener("input", syncGeneratedScriptsFromEditors);
scriptHindiEl?.addEventListener("input", syncGeneratedScriptsFromEditors);
scriptTeluguEl?.addEventListener("input", syncGeneratedScriptsFromEditors);

// Bootstrap
async function boot() {
	setupPresetSelection();
	setupPresetSettings();
	setupScriptMode();
	setupMainTabs();
	setupUploadArea();
	try {
		await loadSettings();
		await Promise.all([loadVideos(), loadJobs()]);
		setStatus("Ready to create content.", "success");
		// Podcast editing does not require a TTS server.
		document.querySelector('[data-tab="studio"]').addEventListener("click", () => loadVoices(), {once: true});
		document.querySelector('[data-tab="settings"]').addEventListener("click", () => loadVoices(), {once: true});
	} catch (e) {
		setStatus(e.message || "Failed to initialize", "error");
	}
}

boot();
setInterval(() => loadJobs().catch(() => {}), 8000);
