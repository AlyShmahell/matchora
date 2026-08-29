const $ = (id) => document.getElementById(id);

const skipEpisodePostersKey = "matchora.skip_episode_posters";
const sessionKey = "matchora.session";

let pendingTimer = null;
let waitTick = null;
let waitRows = [];
let session = localStorage.getItem(sessionKey) || "";

function skipEpisodePosters() {
  const el = $("skip-episode-posters");
  return !!(el && el.checked);
}

function withSkipFlag(url) {
  const u = new URL(url, location.origin);
  if (session) {
    u.searchParams.set("session", session);
  }
  if (skipEpisodePosters()) {
    u.searchParams.set("skip_episode_posters", "true");
  }
  return u.pathname + u.search;
}

function setSession(id) {
  session = id || "";
  if (session) {
    localStorage.setItem(sessionKey, session);
  } else {
    localStorage.removeItem(sessionKey);
  }
}

async function loadSession() {
  try {
    const r = await fetch("/v1/sessions");
    const ids = r.ok ? await r.json() : [];
    const list = Array.isArray(ids) ? ids : [];
    if (session && list.includes(session)) {
      return;
    }
    if (list.length) {
      setSession(list[0]);
    } else {
      setSession("");
    }
  } catch {
    /* keep cached session */
  }
}

function skipPayload(extra) {
  const body = extra || {};
  if (skipEpisodePosters()) {
    body.skip_episode_posters = true;
  }
  return body;
}

function initSkipCheckbox() {
  const el = $("skip-episode-posters");
  if (!el) {
    return;
  }
  el.checked = localStorage.getItem(skipEpisodePostersKey) === "true";
  el.addEventListener("change", () => {
    localStorage.setItem(skipEpisodePostersKey, el.checked ? "true" : "false");
  });
}

async function loadSecrets() {
  const status = $("secrets-status");
  try {
    const r = await fetch("/v1/secrets");
    const j = await r.json();
    if (!r.ok) {
      if (status) {
        status.hidden = false;
        status.textContent = r.status + (j && j.error ? " " + j.error : "");
      }
      return;
    }
    renderSecrets(j);
  } catch (err) {
    if (status) {
      status.hidden = false;
      status.textContent = String(err);
    }
  }
}

function renderSecrets(status) {
  const box = $("secret-fields");
  if (!box) {
    return;
  }
  box.replaceChildren();
  const keys = Object.keys(status || {});
  for (const key of keys) {
    const row = document.createElement("label");
    row.className = "secret-row";
    const name = document.createElement("span");
    name.textContent = key;
    const input = document.createElement("input");
    input.name = key;
    input.autocomplete = "off";
    input.type = "password";
    input.placeholder = status[key] ? "set" : "unset";
    const hint = document.createElement("span");
    hint.className = "hint";
    hint.textContent = status[key] ? "set" : "unset";
    row.append(name, input, hint);
    if (status[key]) {
      const clear = document.createElement("button");
      clear.type = "button";
      clear.textContent = "clear";
      clear.addEventListener("click", (e) => {
        e.preventDefault();
        clearSecret(key);
      });
      row.appendChild(clear);
    }
    box.appendChild(row);
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitUntilHealthy(statusEl) {
  if (statusEl) {
    statusEl.hidden = false;
    statusEl.textContent = "restarting…";
  }
  for (let i = 0; i < 90; i++) {
    await sleep(1000);
    try {
      const r = await fetch("/health");
      const j = await r.json();
      if (r.ok && j.healthy === true) {
        await pollHealth();
        return true;
      }
    } catch {
      /* bouncing */
    }
  }
  if (statusEl) {
    statusEl.textContent = "restart timed out";
  }
  return false;
}

async function postSecrets(body) {
  const status = $("secrets-status");
  status.hidden = false;
  try {
    const r = await fetch("/v1/secrets", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    let payload = null;
    try {
      payload = await r.json();
    } catch {
      payload = null;
    }
    if (!r.ok) {
      status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
      return;
    }
  } catch {
    /* connection drop on restart */
  }
  if (!(await waitUntilHealthy(status))) {
    return;
  }
  await loadSecrets();
  status.textContent = "saved";
}

async function loadLlama() {
  const host = $("llama-host");
  const port = $("llama-port");
  if (!host || !port) {
    return;
  }
  try {
    const r = await fetch("/v1/config");
    const j = await r.json();
    const llama = j && j.llama ? j.llama : {};
    host.value = llama.host || "127.0.0.1";
    port.value = llama.port || 8080;
  } catch {
    host.value = host.value || "127.0.0.1";
    port.value = port.value || 8080;
  }
}

async function clearSecret(key) {
  const body = {};
  body[key] = "";
  await postSecrets(body);
}

async function pollHealth() {
  const el = $("status");
  try {
    const r = await fetch("/health");
    const j = await r.json();
    const ok = r.ok && j.healthy === true;
    el.textContent = ok ? "healthy" : "down";
    el.className = "status " + (ok ? "ok" : "down");
    renderModelChips(j.models);
  } catch {
    el.textContent = "down";
    el.className = "status down";
    renderModelChips([]);
  }
}

function renderModelChips(models) {
  const box = $("model-chips");
  box.replaceChildren();
  if (!Array.isArray(models)) {
    return;
  }
  for (const m of models) {
    if (!m || !m.name) {
      continue;
    }
    const chip = document.createElement("span");
    chip.className = "chip chip-model";
    const tok = Number(m.tok_s);
    const rate = Number.isFinite(tok) ? tok.toFixed(1) : "—";
    const device = m.device === "cpu" || m.device === "gpu" ? m.device : "";
    chip.textContent = [m.role, m.name, rate + " tok/s", device].filter(Boolean).join(" ");
    box.appendChild(chip);
  }
}

async function loadFS(path) {
  const q = path ? "?path=" + encodeURIComponent(path) : "";
  const r = await fetch("/v1/fs" + q);
  if (!r.ok) {
    throw new Error(await r.text());
  }
  return r.json();
}

function renderFS(listing) {
  $("cwd").textContent = listing.path;
  $("cwd").dataset.path = listing.path;
  $("cwd").dataset.parent = listing.parent || "";
  $("cwd").dataset.root = listing.root;
  $("up").disabled = !listing.parent;
  const ul = $("dirs");
  ul.replaceChildren();
  for (const e of listing.entries || []) {
    const li = document.createElement("li");
    const b = document.createElement("button");
    b.type = "button";
    b.className = "link";
    b.textContent = e.name + "/";
    b.addEventListener("click", () => browse(e.path));
    li.appendChild(b);
    ul.appendChild(li);
  }
}

async function browse(path) {
  try {
    $("scan-status").hidden = true;
    renderFS(await loadFS(path));
  } catch (err) {
    $("scan-status").hidden = false;
    $("scan-status").textContent = String(err);
  }
}

function heatClass(score) {
  const s = Number(score) || 0;
  if (s >= 1) {
    return "heat-6";
  }
  return "heat-" + Math.min(6, Math.max(0, Math.floor(s * 7)));
}

function candCard(c, job) {
  const wrap = document.createElement("div");
  wrap.className = "cand " + heatClass(c.score);
  const key = (c.provider || "") + ":" + (c.id || "");
  if (job.catalog_for && job.catalog_for === key) {
    wrap.classList.add("catalog-on");
  }
  if (c.poster) {
    const img = document.createElement("img");
    img.src = c.poster;
    img.alt = "";
    img.className = "poster";
    wrap.appendChild(img);
  }
  const body = document.createElement("div");
  body.className = "cand-body";
  const interactive = job.status === "manual" || job.status === "multiple";
  if (interactive) {
    body.classList.add("pick");
    body.addEventListener("click", () => selectCandidate(job.id, c));
  }
  const id = document.createElement("div");
  id.className = "cand-id";
  id.textContent = key;
  const title = document.createElement("div");
  title.className = "cand-title";
  let name = c.title || "";
  if (c.year) name += " (" + c.year + ")";
  title.textContent = name;
  const score = document.createElement("div");
  score.className = "cand-score " + heatClass(c.score);
  score.textContent = "score: " + Number(c.score || 0).toFixed(3);
  body.append(id, title, score);
  if (c.synopsis) {
    const syn = document.createElement("div");
    syn.className = "cand-syn";
    syn.textContent = c.synopsis;
    body.appendChild(syn);
  }
  wrap.appendChild(body);
  const seasons = document.createElement("button");
  seasons.type = "button";
  seasons.className = "seasons";
  seasons.textContent = "seasons";
  seasons.addEventListener("click", (ev) => {
    ev.stopPropagation();
    loadCatalog(job.id, c);
  });
  wrap.appendChild(seasons);
  return wrap;
}

function renderChips(jobs) {
  const list = Array.isArray(jobs) ? jobs : [];
  let success = 0, manual = 0, pending = 0, failure = 0;
  for (const j of list) {
    if (j.status === "matched") success++;
    else if (j.status === "manual" || j.status === "multiple") manual++;
    else if (j.status === "pending") pending++;
    else if (j.status === "error" || j.status === "unmatched") failure++;
  }
  $("chip-total").textContent = "total " + list.length;
  $("chip-success").textContent = "success " + success;
  $("chip-manual").textContent = "manual " + manual;
  $("chip-pending").textContent = "pending " + pending;
  $("chip-failure").textContent = "failure " + failure;
}

function jobOrder(j) {
  const s = j.status || "";
  if (s === "error" || s === "unmatched") {
    return 0;
  }
  if (s === "manual" || s === "multiple") {
    return 1;
  }
  if (s === "pending") {
    return 2;
  }
  return 3;
}

function renderJobs(jobs) {
  const box = $("jobs");
  const empty = $("empty");
  renderChips(jobs);
  box.replaceChildren();
  if (!jobs || jobs.length === 0) {
    empty.hidden = false;
    return;
  }
  empty.hidden = true;
  const list = jobs.slice().sort((a, b) => jobOrder(a) - jobOrder(b));
  for (const job of list) {
    const card = document.createElement("article");
    card.className = "job";
    const title = document.createElement("div");
    const bits = [job.title];
    if (job.year) bits.push("(" + job.year + ")");
    title.textContent = bits.join(" ");
    const meta = document.createElement("div");
    meta.className = "meta";
    if (job.match && job.match.score) {
      meta.classList.add(heatClass(job.match.score));
    }
    let line = (job.source || "unknown") + " → " + (job.status || "pending");
    if (job.type) line += " · " + job.type;
    if (job.ranker) line += " [" + job.ranker + "]";
    if (job.match) {
      line += " · " + job.match.provider + ":" + job.match.id + " " + job.match.title;
      if (job.match.score) line += " (" + Number(job.match.score).toFixed(3) + ")";
    }
    if (job.sub) line += " · ep " + job.sub.title;
    if (job.error) line += " · " + job.error;
    meta.textContent = line;
    card.append(title, meta);
    if (job.candidates && job.candidates.length) {
      const boxCands = document.createElement("div");
      boxCands.className = "cands";
      const ranked = job.candidates.slice().sort((a, b) => (Number(b.score) || 0) - (Number(a.score) || 0));
      for (const c of ranked.slice(0, 5)) {
        boxCands.appendChild(candCard(c, job));
      }
      card.appendChild(boxCands);
    }
    const catalog = renderCatalog(job);
    if (catalog) {
      card.appendChild(catalog);
    }
    box.appendChild(card);
  }
}

async function selectCandidate(jobId, c) {
  await fetch(withSkipFlag("/v1/jobs/" + encodeURIComponent(jobId) + "/select"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(skipPayload({ provider: c.provider, id: c.id })),
  });
  await loadJobs();
}

async function loadCatalog(jobId, c) {
  await fetch(withSkipFlag("/v1/jobs/" + encodeURIComponent(jobId) + "/catalog"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(skipPayload({ provider: c.provider, id: c.id })),
  });
  await loadJobs();
}

function renderCatalog(job) {
  if (!Array.isArray(job.catalog)) {
    return null;
  }
  const box = document.createElement("div");
  box.className = "catalog";
  if (job.catalog.length === 0) {
    const empty = document.createElement("div");
    empty.className = "catalog-empty";
    empty.textContent = "no seasons";
    box.appendChild(empty);
    return box;
  }
  for (const s of job.catalog) {
    box.appendChild(catalogSeason(s));
  }
  return box;
}

function catalogSeason(s) {
  const wrap = document.createElement("div");
  wrap.className = "catalog-season";
  if (s.poster) {
    const img = document.createElement("img");
    img.src = s.poster;
    img.alt = "";
    img.className = "poster";
    wrap.appendChild(img);
  }
  const body = document.createElement("div");
  body.className = "catalog-body";
  const title = document.createElement("div");
  title.className = "catalog-title";
  let name = s.title || "";
  if (s.year) name += " (" + s.year + ")";
  title.textContent = name;
  body.appendChild(title);
  if (s.synopsis) {
    const syn = document.createElement("div");
    syn.className = "catalog-syn";
    syn.textContent = s.synopsis;
    body.appendChild(syn);
  }
  wrap.appendChild(body);
  const eps = s.episodes || [];
  if (eps.length) {
    const list = document.createElement("div");
    list.className = "catalog-eps";
    for (const e of eps) {
      list.appendChild(catalogEpisode(e));
    }
    wrap.appendChild(list);
  }
  return wrap;
}

function catalogEpisode(e) {
  const wrap = document.createElement("div");
  wrap.className = "catalog-ep";
  if (e.poster) {
    const img = document.createElement("img");
    img.src = e.poster;
    img.alt = "";
    img.className = "poster";
    wrap.appendChild(img);
  }
  const body = document.createElement("div");
  body.className = "catalog-body";
  const title = document.createElement("div");
  title.className = "catalog-title";
  let name = e.title || "";
  if (e.number) name = e.number + ". " + name;
  if (e.year) name += " (" + e.year + ")";
  title.textContent = name;
  body.appendChild(title);
  if (e.synopsis) {
    const syn = document.createElement("div");
    syn.className = "catalog-syn";
    syn.textContent = e.synopsis;
    body.appendChild(syn);
  }
  wrap.appendChild(body);
  return wrap;
}

async function loadScanStatus() {
  try {
    if (!session) {
      return { files: 0, done: 0, running: false };
    }
    const r = await fetch(withSkipFlag("/v1/scan/status"));
    if (!r.ok) {
      return { files: 0, done: 0, running: false };
    }
    return await r.json();
  } catch {
    return { files: 0, done: 0, running: false };
  }
}

function renderScanProgress(st) {
  const chip = $("chip-group");
  const bar = $("scan-progress");
  const files = Number(st && st.files) || 0;
  const done = Number(st && st.done) || 0;
  const running = !!(st && st.running);
  const show = running || (files > 0 && done > 0);
  chip.hidden = !show;
  bar.hidden = !show;
  if (!show) {
    return;
  }
  chip.textContent = "group " + done + "/" + files;
  bar.max = files > 0 ? files : 1;
  bar.value = done;
  const status = $("scan-status");
  if (!status.hidden && running) {
    status.textContent = "grouped " + done + "/" + files;
  }
}

async function loadJobs() {
  if (!session) {
    renderJobs([]);
    waitRows = [];
    renderWaits();
    return [];
  }
  const r = await fetch(withSkipFlag("/v1/jobs"));
  const jobs = r.ok ? await r.json() : [];
  renderJobs(jobs);
  await loadWaits();
  return Array.isArray(jobs) ? jobs : [];
}

async function loadWaits() {
  try {
    if (!session) {
      waitRows = [];
      renderWaits();
      return;
    }
    const r = await fetch(withSkipFlag("/v1/match/log"));
    waitRows = r.ok ? await r.json() : [];
    if (!Array.isArray(waitRows)) {
      waitRows = [];
    }
  } catch {
    waitRows = [];
  }
  renderWaits();
  tickWaits();
}

function waitElapsed(w, now) {
  const start = Date.parse(w.since);
  if (!Number.isFinite(start)) {
    return 0;
  }
  const end = w.until ? Date.parse(w.until) : now;
  if (!Number.isFinite(end)) {
    return 0;
  }
  return Math.max(0, (end - start) / 1000);
}

function renderWaits() {
  const box = $("waits");
  const empty = $("waits-empty");
  box.replaceChildren();
  if (!waitRows.length) {
    empty.hidden = false;
    return;
  }
  empty.hidden = true;
  const now = Date.now();
  for (const w of waitRows) {
    const row = document.createElement("div");
    row.className = "wait";
    row.dataset.since = w.since || "";
    row.dataset.until = w.until || "";
    const name = document.createElement("span");
    name.className = "wait-name";
    name.textContent = w.name || "";
    const title = document.createElement("span");
    title.className = "wait-title";
    title.textContent = w.title || "";
    const state = document.createElement("span");
    const running = !w.until;
    state.className = "wait-state " + (w.error ? "err" : running ? "running" : "done");
    state.textContent = w.error ? "error" : running ? "running" : "done";
    const sec = document.createElement("span");
    sec.className = "wait-sec";
    sec.textContent = waitElapsed(w, now).toFixed(1) + "s";
    row.append(name, title, state, sec);
    box.appendChild(row);
  }
}

function tickWaits() {
  const now = Date.now();
  const box = $("waits");
  if (!box) {
    return;
  }
  for (const row of box.children) {
    if (row.dataset.until) {
      continue;
    }
    const sec = row.querySelector(".wait-sec");
    if (!sec) {
      continue;
    }
    sec.textContent = waitElapsed({ since: row.dataset.since, until: row.dataset.until }, now).toFixed(1) + "s";
  }
}

function stopPending() {
  if (pendingTimer) {
    clearInterval(pendingTimer);
    pendingTimer = null;
  }
}

function watchPending() {
  if (pendingTimer) {
    return;
  }
  pendingTimer = setInterval(async () => {
    const tickSession = session;
    const jobs = await loadJobs();
    const st = await loadScanStatus();
    renderScanProgress(st);
    if (tickSession !== session) {
      return;
    }
    const pending = jobs.some((j) => j.status === "pending");
    if (!pending && !st.running) {
      stopPending();
    }
  }, 1000);
  if (!waitTick) {
    waitTick = setInterval(tickWaits, 250);
  }
}

function restartPending() {
  stopPending();
  watchPending();
}

$("upload").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const file = ev.target.file.files[0];
  const status = $("upload-status");
  status.hidden = false;
  if (!file) {
    status.textContent = "no file selected";
    return;
  }
  const body = new FormData();
  body.append("file", file);
  const r = await fetch(withSkipFlag("/v1/ingest"), { method: "POST", body });
  let payload = null;
  try {
    payload = await r.json();
  } catch {
    payload = null;
  }
  if (!r.ok && r.status !== 202) {
    status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
    return;
  }
  if (payload && payload.session) {
    setSession(payload.session);
  }
  const n = payload && Array.isArray(payload.jobs) ? payload.jobs.length : 0;
  status.textContent = "queued " + n + " titles";
  await loadJobs();
  restartPending();
});

$("secrets").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const body = {};
  for (const input of $("secret-fields").querySelectorAll("input")) {
    const v = input.value.trim();
    if (v) {
      body[input.name] = v;
    }
  }
  await postSecrets(body);
});

$("llama").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const status = $("llama-status");
  status.hidden = false;
  const host = $("llama-host").value.trim() || "127.0.0.1";
  const port = Number($("llama-port").value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    status.textContent = "port must be 1–65535";
    return;
  }
  try {
    const r = await fetch("/v1/config", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ llama: { host, port } }),
    });
    let payload = null;
    try {
      payload = await r.json();
    } catch {
      payload = null;
    }
    if (!r.ok) {
      status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
      return;
    }
  } catch {
    /* connection drop on restart */
  }
  if (!(await waitUntilHealthy(status))) {
    return;
  }
  await loadLlama();
  status.textContent = "saved";
});

$("clear").addEventListener("click", async () => {
  if (!confirm("Clear all jobs?")) {
    return;
  }
  const status = $("retry-status");
  status.hidden = false;
  try {
    const r = await fetch("/v1/jobs", { method: "DELETE" });
    let payload = null;
    try {
      payload = await r.json();
    } catch {
      payload = null;
    }
    if (!r.ok) {
      status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
      return;
    }
    status.textContent = "cleared";
    setSession("");
    await loadSession();
    await loadJobs();
    renderScanProgress(await loadScanStatus());
  } catch (err) {
    status.textContent = String(err);
  }
});

$("retry").addEventListener("click", async () => {
  const status = $("retry-status");
  status.hidden = false;
  try {
    const r = await fetch(withSkipFlag("/v1/retry"), { method: "POST" });
    let payload = null;
    try {
      payload = await r.json();
    } catch {
      payload = null;
    }
    if (!r.ok && r.status !== 202) {
      status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
      return;
    }
    const n = Array.isArray(payload) ? payload.length : 0;
    status.textContent = "queued " + n + " titles";
    await loadJobs();
    watchPending();
  } catch (err) {
    status.textContent = String(err);
  }
});

$("up").addEventListener("click", () => {
  const parent = $("cwd").dataset.parent;
  if (parent) browse(parent);
});

$("scan").addEventListener("click", async () => {
  const path = $("cwd").dataset.path;
  const status = $("scan-status");
  status.hidden = false;
  status.textContent = "scanning…";
  const r = await fetch(withSkipFlag("/v1/scan"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(skipPayload({ path })),
  });
  let payload = null;
  try {
    payload = await r.json();
  } catch {
    payload = null;
  }
  if (!r.ok && r.status !== 202) {
    status.textContent = r.status + (payload && payload.error ? " " + payload.error : "");
    return;
  }
  if (payload && payload.session) {
    setSession(payload.session);
  }
  if (payload && typeof payload.files === "number") {
    status.textContent = "grouping " + payload.files + " files…";
  } else {
    const n = payload && Array.isArray(payload.jobs) ? payload.jobs.length : 0;
    status.textContent = "queued " + n + " titles";
  }
  await loadJobs();
  renderScanProgress(await loadScanStatus());
  restartPending();
});

pollHealth();
setInterval(pollHealth, 4000);
initSkipCheckbox();
loadSecrets();
loadLlama();
browse("");
loadSession().then(() => loadJobs()).then(async (jobs) => {
  const st = await loadScanStatus();
  renderScanProgress(st);
  if (jobs.some((j) => j.status === "pending") || st.running) {
    watchPending();
  }
});
