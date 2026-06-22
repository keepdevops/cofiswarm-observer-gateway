"use strict";
// Cofiswarm Observer Panel — a dependency-free, configurable widget dashboard. Widgets bind
// to live bus topics streamed over the gateway's /ws WebSocket. Layout (which widgets, their
// order and size) is draggable, resizable, and persisted to localStorage. No external libs:
// charts are drawn on <canvas>, so the gateway stays a single offline-safe binary.

const LS_KEY = "cofiswarm.dashboard.layout";

// Widget catalog: each kind declares a title, a body builder, and a render(el, state) fn.
const CATALOG = {
  online: { title: "Online components", build: statBody, render: (el, s) =>
    setStat(el, Object.keys(s.roster).length, "live now") },
  eps: { title: "Events / sec", build: statBody, render: (el, s) =>
    setStat(el, eventsPerSec(s).toFixed(1), "last 10s") },
  slot: { title: "Slot pressure", build: canvasBody, render: (el, s) =>
    drawLine(el, s.series.slot, 0, 1) },
  kv: { title: "KV pressure", build: canvasBody, render: (el, s) =>
    drawLine(el, s.series.kv, 0, 1) },
  topics: { title: "Events by topic", build: canvasBody, render: (el, s) =>
    drawBars(el, topTopics(s, 6)) },
  feed: { title: "Live event feed", build: feedBody, render: renderFeed },
};

const DEFAULT_LAYOUT = [
  { id: "w1", kind: "online", w: 280, h: 180 },
  { id: "w2", kind: "slot", w: 420, h: 220 },
  { id: "w3", kind: "kv", w: 420, h: 220 },
  { id: "w4", kind: "eps", w: 280, h: 180 },
  { id: "w5", kind: "topics", w: 420, h: 220 },
  { id: "w6", kind: "feed", w: 420, h: 260 },
];

const state = {
  roster: {}, series: { slot: [], kv: [] }, topicCounts: {}, feed: [], eventTimes: [],
};
let layout = loadLayout();

// ---- bus event ingestion -------------------------------------------------------------

function onEvent(topic, payload) {
  const now = Date.now();
  state.eventTimes.push(now);
  state.eventTimes = state.eventTimes.filter((t) => now - t <= 10000);
  state.topicCounts[topic] = (state.topicCounts[topic] || 0) + 1;
  state.feed.unshift({ topic, payload, t: now });
  if (state.feed.length > 100) state.feed.pop();

  if (topic.endsWith(".presence")) applyPresence(payload);
  else if (topic.endsWith(".announce")) setOnline(payload, true);
  else if (topic.endsWith(".goodbye")) setOnline(payload, false);
  else if (topic.endsWith("slot.pressure")) pushSeries("slot", payload.usage);
  else if (topic.endsWith("kvpool.pressure")) pushSeries("kv", payload.usage);
}

function applyPresence(p) {
  if (!p || !p.component_id) return;
  if (p.status === "offline") delete state.roster[p.component_id];
  else state.roster[p.component_id] = p;
}
function setOnline(p, online) {
  if (!p || !p.component_id) return;
  if (online) state.roster[p.component_id] = { ...p, status: "online" };
  else delete state.roster[p.component_id];
}
function pushSeries(key, v) {
  const n = Number(v);
  if (Number.isNaN(n)) return;
  state.series[key].push(n);
  if (state.series[key].length > 120) state.series[key].shift();
}

function eventsPerSec(s) { return s.eventTimes.length / 10; }
function topTopics(s, n) {
  return Object.entries(s.topicCounts).sort((a, b) => b[1] - a[1]).slice(0, n);
}

// ---- widget bodies + renderers -------------------------------------------------------

function statBody(body) {
  body.innerHTML = '<div class="stat-num">—</div><div class="stat-label"></div>';
}
function setStat(body, num, label) {
  body.querySelector(".stat-num").textContent = num;
  body.querySelector(".stat-label").textContent = label;
}
function canvasBody(body) { body.innerHTML = "<canvas></canvas>"; }
function feedBody(body) { body.innerHTML = '<div class="feed"></div>'; }

function renderFeed(body, s) {
  const feed = body.querySelector(".feed");
  feed.innerHTML = s.feed.slice(0, 40).map((e) =>
    `<div><span class="topic">${esc(e.topic)}</span> ${esc(JSON.stringify(e.payload || {}))}</div>`
  ).join("");
}

function fitCanvas(canvas) {
  const r = canvas.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.max(1, r.width * dpr);
  canvas.height = Math.max(1, r.height * dpr);
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w: r.width, h: r.height };
}

function drawLine(body, data, min, max) {
  const canvas = body.querySelector("canvas");
  const { ctx, w, h } = fitCanvas(canvas);
  ctx.clearRect(0, 0, w, h);
  drawAxes(ctx, w, h, max);
  if (data.length < 2) return;
  const pad = 24, n = data.length;
  const x = (i) => pad + (i / (n - 1)) * (w - pad - 6);
  const y = (v) => h - 16 - ((v - min) / (max - min)) * (h - 28);
  ctx.beginPath();
  data.forEach((v, i) => (i ? ctx.lineTo(x(i), y(v)) : ctx.moveTo(x(i), y(v))));
  ctx.strokeStyle = cssVar("--accent");
  ctx.lineWidth = 2;
  ctx.stroke();
  // soft fill under the line
  ctx.lineTo(x(n - 1), h - 16); ctx.lineTo(x(0), h - 16); ctx.closePath();
  ctx.fillStyle = cssVar("--accent") + "22";
  ctx.fill();
}

function drawBars(body, entries) {
  const canvas = body.querySelector("canvas");
  const { ctx, w, h } = fitCanvas(canvas);
  ctx.clearRect(0, 0, w, h);
  if (!entries.length) return;
  const max = Math.max(...entries.map((e) => e[1]));
  const pad = 6, bw = (w - pad * 2) / entries.length;
  entries.forEach((e, i) => {
    const bh = (e[1] / max) * (h - 28);
    ctx.fillStyle = cssVar("--accent");
    ctx.fillRect(pad + i * bw + 2, h - 16 - bh, bw - 4, bh);
    ctx.fillStyle = cssVar("--muted");
    ctx.font = "9px monospace";
    const label = e[0].split(".").slice(-2).join(".");
    ctx.fillText(label.slice(0, 10), pad + i * bw + 2, h - 4);
  });
}

function drawAxes(ctx, w, h, max) {
  ctx.strokeStyle = cssVar("--border"); ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(24, h - 16); ctx.lineTo(w - 6, h - 16); ctx.stroke();
  ctx.fillStyle = cssVar("--muted"); ctx.font = "9px monospace";
  ctx.fillText(String(max), 2, 12); ctx.fillText("0", 2, h - 16);
}

// ---- layout: build, drag, resize, persist --------------------------------------------

const grid = document.getElementById("grid");

function buildGrid() {
  grid.innerHTML = "";
  layout.forEach((item) => grid.appendChild(buildWidget(item)));
}

function buildWidget(item) {
  const spec = CATALOG[item.kind];
  const el = document.createElement("div");
  el.className = "widget";
  el.dataset.id = item.id;
  el.draggable = false;
  el.style.width = item.w + "px";
  el.style.height = item.h + "px";
  el.innerHTML = `<div class="w-head" draggable="true">
      <span class="w-grip">⠿</span><span class="w-title">${esc(spec ? spec.title : item.kind)}</span>
      <button class="w-close" title="Remove">×</button>
    </div><div class="w-body"></div>`;
  const body = el.querySelector(".w-body");
  if (spec) spec.build(body);
  el.querySelector(".w-close").onclick = () => removeWidget(item.id);
  wireDrag(el, item.id);
  wireResize(el, item);
  return el;
}

function wireDrag(el, id) {
  const head = el.querySelector(".w-head");
  head.addEventListener("dragstart", (e) => e.dataTransfer.setData("text/plain", id));
  el.addEventListener("dragover", (e) => { e.preventDefault(); el.classList.add("drag-over"); });
  el.addEventListener("dragleave", () => el.classList.remove("drag-over"));
  el.addEventListener("drop", (e) => {
    e.preventDefault(); el.classList.remove("drag-over");
    reorder(e.dataTransfer.getData("text/plain"), id);
  });
}

function wireResize(el, item) {
  const ro = new ResizeObserver(() => {
    item.w = Math.round(el.clientWidth); item.h = Math.round(el.clientHeight);
    saveLayout();
  });
  ro.observe(el);
}

function reorder(fromId, toId) {
  if (fromId === toId) return;
  const from = layout.findIndex((w) => w.id === fromId);
  const to = layout.findIndex((w) => w.id === toId);
  if (from < 0 || to < 0) return;
  const [moved] = layout.splice(from, 1);
  layout.splice(to, 0, moved);
  saveLayout(); buildGrid();
}

function removeWidget(id) {
  layout = layout.filter((w) => w.id !== id);
  saveLayout(); buildGrid();
}

function addWidget(kind) {
  if (!CATALOG[kind]) return;
  layout.push({ id: "w" + Date.now(), kind, w: 360, h: 200 });
  saveLayout(); buildGrid();
}

function loadLayout() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) return JSON.parse(raw);
  } catch (e) { console.error("dashboard: bad saved layout, using default:", e); }
  return DEFAULT_LAYOUT.map((w) => ({ ...w }));
}
function saveLayout() {
  try { localStorage.setItem(LS_KEY, JSON.stringify(layout)); }
  catch (e) { console.error("dashboard: cannot persist layout:", e); }
}

// ---- render loop + websocket ---------------------------------------------------------

function renderAll() {
  for (const item of layout) {
    const el = grid.querySelector(`.widget[data-id="${item.id}"] .w-body`);
    const spec = CATALOG[item.kind];
    if (el && spec) {
      try { spec.render(el, state); }
      catch (e) { console.error(`dashboard: render ${item.kind}:`, e); }
    }
  }
}

function connect() {
  const status = document.getElementById("status");
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws`);
  ws.onopen = () => { status.textContent = "connected"; status.className = "up"; };
  ws.onclose = () => {
    status.textContent = "disconnected — retrying"; status.className = "down";
    setTimeout(connect, 1000);
  };
  ws.onmessage = (e) => {
    let m; try { m = JSON.parse(e.data); } catch { return; }
    if (m.type === "ack" || m.type === "error") return; // command replies, not bus events
    if (m.topic) onEvent(m.topic, m.payload);
  };
}

// ---- helpers + boot ------------------------------------------------------------------

function cssVar(name) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }
function esc(s) { return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c])); }

function initControls() {
  const sel = document.getElementById("add-widget");
  for (const [kind, spec] of Object.entries(CATALOG)) {
    const o = document.createElement("option");
    o.value = kind; o.textContent = spec.title;
    sel.appendChild(o);
  }
  sel.onchange = () => { if (sel.value) { addWidget(sel.value); sel.value = ""; } };
  document.getElementById("reset").onclick = () => {
    layout = DEFAULT_LAYOUT.map((w) => ({ ...w })); saveLayout(); buildGrid();
  };
}

initControls();
buildGrid();
connect();
setInterval(renderAll, 500);
