(() => {
  const el = id => document.getElementById(id);
  const status = text => { el("shorts-status").textContent = text; };
  const cards = new Map();
  let polling = false;
  async function api(path, options) {
    const response = await fetch(path, {cache: "no-store", ...options});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Request failed");
    return result;
  }
  function node(tag, text, parent) {
    const element = document.createElement(tag);
    if (text) element.textContent = text;
    if (parent) parent.append(element);
    return element;
  }
  async function library() {
    try {
      const videos = await api("/api/videos");
      const select = el("shorts-source"), previous = select.value;
      select.replaceChildren(new Option("Use YouTube link", ""));
      for (const video of videos) select.add(new Option(video.path, video.path));
      select.value = previous;
    } catch (error) { status(error.message); }
  }
  async function refresh() {
    if (polling) return;
    polling = true;
    try {
      const jobs = await api("/api/shorts");
      for (const job of jobs) {
        let card = cards.get(job.id);
        if (!card) {
          const root = node("section", "", el("shorts-jobs"));
          root.className = "card";
          node("h3", job.request.url || job.request.source, root).style.overflowWrap = "anywhere";
          const message = node("p", "", root), progress = node("progress", "", root);
          progress.max = 100;
          progress.style.width = "100%";
          const error = node("pre", "", root);
          error.style.whiteSpace = "pre-wrap";
          error.style.overflowWrap = "anywhere";
          const actions = node("div", "", root);
          const cancel = node("button", "Cancel", actions);
          cancel.className = "btn btn-secondary btn-sm";
          cancel.onclick = async () => {
            try { await api(`/api/shorts/${job.id}/cancel`, {method: "POST"}); await refresh(); }
            catch (error) { status(error.message); }
          };
          const retry = node("button", "Create new attempt", actions);
          retry.className = "btn btn-secondary btn-sm";
          retry.onclick = async () => {
            try { await api("/api/shorts", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(job.request)}); await refresh(); }
            catch (error) { status(error.message); }
          };
          const download = node("a", "Download all clips (.zip)", actions);
          download.className = "btn btn-primary btn-sm";
          download.href = `/api/shorts/${job.id}/download`;
          const clips = node("div", "", root);
          clips.style.cssText = "display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:16px;margin-top:16px";
          card = {root, message, progress, error, cancel, retry, download, clips, seen: new Set()};
          cards.set(job.id, card);
        }
        card.message.textContent = `${job.status} · ${job.progress}% · ${job.message}`;
        card.progress.value = job.progress;
        card.error.textContent = [job.error, ...(job.warnings || [])].filter(Boolean).join("\n");
        card.cancel.hidden = !["queued", "running"].includes(job.status);
        card.retry.hidden = !["failed", "cancelled"].includes(job.status);
        card.download.hidden = !job.clips.length;
        for (const clip of job.clips) {
          if (card.seen.has(clip.filename)) continue;
          card.seen.add(clip.filename);
          const tile = node("article", "", card.clips);
          const video = node("video", "", tile);
          video.controls = true;
          video.preload = "none";
          video.src = clip.url;
          video.style.cssText = "width:100%;aspect-ratio:9/16;background:#111;border-radius:8px";
          node("p", `${clip.duration.toFixed(2)}s · ${clip.title}`, tile);
          const link = node("a", "Download MP4", tile);
          link.href = clip.url;
          link.download = clip.filename;
        }
      }
      // Move existing cards without recreating video players during polling.
      for (const job of jobs) el("shorts-jobs").append(cards.get(job.id).root);
    } catch (error) { status(`Cannot refresh jobs: ${error.message}`); }
    finally { polling = false; }
  }
  el("shorts-form").addEventListener("submit", async event => {
    event.preventDefault();
    const source = el("shorts-source").value;
    const url = el("shorts-url").value.trim();
    if (!source && !url) { status("Paste a YouTube link or choose a library video."); return; }
    el("shorts-submit").disabled = true;
    try {
      await api("/api/shorts", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({
        url: source ? "" : url, source, layout: el("shorts-layout").value,
        language: el("shorts-language").value, max_duration: Number(el("shorts-duration").value),
        no_captions: !el("shorts-captions").checked
      })});
      status("Podcast queued. You can close this page; processing continues while the server is running.");
      await refresh();
    } catch (error) { status(error.message); }
    finally { el("shorts-submit").disabled = false; }
  });
  el("shorts-refresh-library").onclick = library;
  library();
  refresh();
  setInterval(refresh, 3000);
})();
