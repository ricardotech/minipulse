"use strict";
const $ = (id) => document.getElementById(id);
const state = { range: "1h", diskRoot: "", diskView: "tree", diskParent: "", procSort: "mem", currency: "USD" };

function fmtBytes(n) {
  if (n == null) return "–";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (Math.abs(n) >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + " " + u[i];
}
function fmtRate(n) { return fmtBytes(n) + "/s"; }
function fmtW(n) { return n == null || isNaN(n) ? "–" : n.toFixed(1) + " W"; }
function fmtMoney(v, cur) { return v == null || isNaN(v) ? "–" : v.toFixed(2) + " " + cur; }
function esc(s) { return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])); }
async function getJSON(url) { const r = await fetch(url); if (!r.ok) throw new Error(url + " " + r.status); return r.json(); }

// --- tiny canvas line chart (no dependencies, works offline) ---
function chart(canvas, series, opts = {}) {
  const dpr = window.devicePixelRatio || 1;
  if (!canvas._h) canvas._h = canvas.height; // logical height, set once
  const w = canvas.clientWidth, h = canvas._h;
  if (!w) return;
  if (canvas.width !== w * dpr) { canvas.width = w * dpr; canvas.height = h * dpr; }
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);
  let max = opts.max ?? 0;
  for (const s of series) for (const v of s.data) if (v > max) max = v;
  if (max <= 0) max = 1;
  ctx.strokeStyle = "#262b31"; ctx.beginPath(); ctx.moveTo(0, h - 1); ctx.lineTo(w, h - 1); ctx.stroke();
  ctx.fillStyle = "#9aa0a6"; ctx.font = "10px ui-monospace, monospace";
  ctx.fillText(opts.fmt ? opts.fmt(max) : max.toFixed(0), 2, 11);
  for (const s of series) {
    if (s.data.length < 2) continue;
    ctx.beginPath();
    s.data.forEach((v, i) => {
      const x = (i / (s.data.length - 1)) * (w - 2) + 1;
      const y = h - 2 - (v / max) * (h - 16);
      i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
    });
    ctx.strokeStyle = s.color; ctx.lineWidth = 1.4; ctx.stroke();
    if (s.fill) {
      ctx.lineTo(w - 1, h - 1); ctx.lineTo(1, h - 1); ctx.closePath();
      ctx.fillStyle = s.fill; ctx.fill();
    }
  }
}

let history = [];
async function loadHistory() {
  try {
    const d = await getJSON("/api/history?range=" + state.range);
    history = (d.points || []);
    drawCharts();
  } catch (e) { /* keep old data */ }
}
function drawCharts() {
  const col = (k) => history.map((p) => p[k] || 0);
  chart($("chart-power"), [
    { data: col("sys_w"), color: "#f0a831", fill: "rgba(240,168,49,.10)" },
    { data: col("soc_w"), color: "#9aa0a6" },
  ], { fmt: (v) => v.toFixed(0) + " W" });
  chart($("chart-cpu"), [{ data: col("cpu_pct"), color: "#5aa2d6", fill: "rgba(90,162,214,.10)" }], { max: 100, fmt: () => "100%" });
  chart($("chart-mem"), [{ data: col("mem_pct"), color: "#4caf7d", fill: "rgba(76,175,125,.10)" }], { max: 100, fmt: () => "100%" });
  chart($("chart-net"), [
    { data: col("net_rx_bps"), color: "#4caf7d" },
    { data: col("net_tx_bps"), color: "#f0a831" },
  ], { fmt: fmtRate });
  chart($("chart-io"), [
    { data: col("disk_read_bps"), color: "#5aa2d6" },
    { data: col("disk_write_bps"), color: "#e05c4b" },
  ], { fmt: fmtRate });
}

function renderSample(s) {
  const p = s.power || {};
  $("watts").textContent = p.available ? p.sys_w.toFixed(1) : "–";
  $("watts-pill").textContent = p.available ? p.sys_w.toFixed(1) + " W" : "– W";
  $("power-sub").textContent = p.available
    ? "whole system · SoC " + fmtW(p.soc_w)
    : (p.reason || "power unavailable");
  $("pw-cpu").textContent = fmtW(p.cpu_w);
  $("pw-gpu").textContent = fmtW(p.gpu_w);
  $("pw-ane").textContent = fmtW(p.ane_w);
  $("pw-temp").textContent = p.cpu_temp_c ? p.cpu_temp_c.toFixed(0) + " °C" : "–";
  $("pw-fan").textContent = p.fan_rpm ? p.fan_rpm + " rpm" : "–";

  $("cpu-val").textContent = s.cpu_pct.toFixed(0) + "%";
  const tiers = (p.cpu_tiers && p.cpu_tiers.length)
    ? p.cpu_tiers.flatMap((t) => t.cores.map((c) => ({ label: t.label + c.id, pct: c.active_ratio * 100 })))
    : (s.cpu_per_core || []).map((v, i) => ({ label: "C" + i, pct: v }));
  $("cores").innerHTML = tiers.map((c) =>
    `<div class="core"><span>${esc(c.label)}</span><div class="bar"><i style="width:${Math.min(100, c.pct)}%"></i></div><span>${c.pct.toFixed(0)}%</span></div>`).join("");
  $("load-line").textContent = `${s.load1.toFixed(2)} · ${s.load5.toFixed(2)} · ${s.load15.toFixed(2)}`;

  $("mem-val").textContent = s.mem_pct.toFixed(0) + "%";
  $("mem-bar").style.width = s.mem_pct + "%";
  $("mem-bar").className = s.mem_pct > 90 ? "hot" : "";
  $("mem-line").textContent = fmtBytes(s.mem_used) + " of " + fmtBytes(s.mem_total);
  $("swap-line").textContent = "swap " + fmtBytes(s.swap_used) + (s.swap_total ? " of " + fmtBytes(s.swap_total) : "");

  $("net-rx").textContent = fmtRate(s.net_rx_bps);
  $("net-tx").textContent = fmtRate(s.net_tx_bps);
  $("io-r").textContent = fmtRate(s.disk_read_bps);
  $("io-w").textContent = fmtRate(s.disk_write_bps);

  if (s.volumes && s.volumes.length) {
    $("volumes").innerHTML = s.volumes.map((v) => `
      <div class="vol">
        <div class="row1"><span>${esc(v.mount)} <span class="muted">${esc(v.device)}</span></span>
        <span>${fmtBytes(v.used)} / ${fmtBytes(v.total)} · ${v.used_pct.toFixed(0)}% · ${fmtBytes(v.free)} free</span></div>
        <div class="bar"><i class="${v.used_pct > 85 ? "hot" : ""}" style="width:${v.used_pct}%"></i></div>
      </div>`).join("");
  }
}

async function loadOverview() {
  try {
    const d = await getJSON("/api/overview");
    state.currency = d.currency || "USD";
    const h = d.host;
    $("host-line").textContent = h.hostname || "this Mac";
    $("host-meta").textContent = `${h.chip || h.model} · ${h.os}`;
    const up = Math.floor((Date.now() / 1000 - h.boot_time) / 3600);
    $("uptime").textContent = `${fmtBytes(h.mem_total)} RAM · up ${Math.floor(up / 24)}d ${up % 24}h`;
    renderSample(d.sample);
    $("e-today").textContent = d.energy_today.kwh.toFixed(3) + " kWh";
    $("e-cost").textContent = fmtMoney(d.energy_today.cost, state.currency);
    renderHealth(d.health);
  } catch (e) { /* stream will retry */ }
}

async function loadEnergy() {
  try {
    const d = await getJSON("/api/energy?range=7d");
    $("e-7d").textContent = d.kwh.toFixed(2) + " kWh";
    $("e-7d").title = fmtMoney(d.cost, state.currency) + " in 7 days";
    const days = (d.days || []).filter((x) => x.kwh > 0);
    if (days.length) {
      const avgDayCost = days.reduce((a, x) => a + x.cost, 0) / days.length;
      $("e-month").textContent = "≈ " + fmtMoney(avgDayCost * 30, state.currency) + " / mo";
    }
  } catch (e) {}
}

async function loadProcs() {
  try {
    const list = await getJSON("/api/processes?sort=" + state.procSort + "&limit=15");
    $("procs").querySelector("tbody").innerHTML = list.map((p) =>
      `<tr><td title="${esc(p.command || p.name)}">${esc(p.name)}</td><td class="r">${fmtBytes(p.rss)}</td><td class="r">${p.cpu_pct.toFixed(0)}%</td></tr>`).join("");
  } catch (e) {}
}

async function loadContainers() {
  try {
    const d = await getJSON("/api/containers");
    const list = d.containers || [];
    $("docker-note").textContent = list.length ? "" : "(no Docker runtime or no containers)";
    $("containers").innerHTML = list.length ? list.map((c) =>
      `<div class="kv"><span>${esc(c.name)} <span class="muted">${esc(c.image)}</span></span>` +
      `<span class="${c.state === "running" ? "up" : "down"}">${esc(c.state)}</span>` +
      `<b>${c.state === "running" ? fmtBytes(c.mem_used) + " · " + c.cpu_pct.toFixed(0) + "%" : ""}</b></div>`).join("")
      : "–";
  } catch (e) {}
}

function renderHealth(list) {
  if (!list || !list.length) return;
  $("health").innerHTML = list.map((h) =>
    `<div class="kv"><span>${esc(h.name)}</span><span class="${h.up ? "up" : "down"}">${h.up ? "up" : "DOWN"}</span><b>${h.latency_ms} ms</b></div>`).join("");
}

async function loadServices() {
  try {
    const d = await getJSON("/api/services");
    const lis = (d.listeners || []).slice().sort((a, b) => a.port - b.port);
    $("listeners").innerHTML = lis.length ? lis.map((l) =>
      `<span class="pill">${esc(l.process)} :${l.port}</span> `).join("") : "–";
    const svcs = (d.services || []).filter((s) => !s.label.startsWith("com.apple."));
    $("services-list").innerHTML = svcs.length ? svcs.map((s) =>
      `<div class="kv"><span>${esc(s.label)}</span><b class="${s.pid ? "up" : ""}">${s.pid ? "pid " + s.pid : "stopped"}</b></div>`).join("")
      : "–";
    renderHealth(d.health);
  } catch (e) {}
}

async function loadEvents() {
  try {
    const list = await getJSON("/api/events?limit=30");
    if (!list || !list.length) return;
    $("events-list").innerHTML = list.map((e) =>
      `<div class="ev"><time>${new Date(e.time).toLocaleString()}</time><span class="${e.kind === "alert" ? "down" : "up"}">●</span> ${esc(e.title)} <span class="muted">${esc(e.detail || "")}</span></div>`).join("");
  } catch (e) {}
}

// --- disk deep-dive ---
function drow(e, max) {
  const pct = max > 0 ? Math.max(1, (e.size / max) * 100) : 0;
  const name = e.is_dir
    ? `<span class="nm dir" data-path="${esc(e.path)}" title="${esc(e.path)}">${esc(e.name)}/</span>`
    : `<span class="nm" title="${esc(e.path)}">${esc(e.name)}</span>`;
  return `<div class="drow">${name}<div class="bar"><i style="width:${pct}%"></i></div><span class="sz">${fmtBytes(e.size)}</span></div>`;
}
function crumbs(path) {
  const parts = path.split("/").filter(Boolean);
  let acc = "", out = `<a data-path="${esc(state.diskRoot)}">⌂ root</a>`;
  const rootParts = state.diskRoot.split("/").filter(Boolean).length;
  parts.forEach((p, i) => {
    acc += "/" + p;
    if (i >= rootParts - 1) out += ` / <a data-path="${esc(acc)}">${esc(p)}</a>`;
  });
  return out;
}
async function loadDisk() {
  try {
    const q = new URLSearchParams({ root: state.diskRoot, view: state.diskView, parent: state.diskParent });
    const d = await getJSON("/api/disk?" + q);
    const sel = $("disk-root");
    if (d.roots && d.roots.join() !== [...sel.options].map((o) => o.value).join()) {
      sel.innerHTML = d.roots.map((r) => `<option value="${esc(r)}">${esc(r)}</option>`).join("");
    }
    if (d.root) { sel.value = d.root; if (!state.diskRoot) { state.diskRoot = d.root; state.diskParent = d.root; } }
    $("rescan").disabled = !!d.scan_running;
    $("rescan").textContent = d.scan_running ? "Scanning…" : "Rescan";
    if (!d.scan) { $("scan-line").textContent = d.hint || "no scan yet"; $("disk-body").innerHTML = ""; return; }
    $("scan-line").textContent = `scanned ${new Date(d.scan.time).toLocaleString()} · ${fmtBytes(d.scan.total_bytes)} in ${(d.scan.file_count || 0).toLocaleString()} files · took ${(d.scan.duration_ms / 1000).toFixed(1)}s`;
    if (state.diskView === "tree") {
      $("crumbs").innerHTML = crumbs(d.parent || state.diskRoot);
      const kids = d.children || [];
      const max = kids.length ? kids[0].size : 0;
      $("disk-body").innerHTML = kids.length ? kids.map((e) => drow(e, max)).join("") : `<div class="sub">nothing ≥ threshold here</div>`;
    } else if (state.diskView === "growth") {
      $("crumbs").textContent = d.previous_scan ? "vs scan of " + new Date(d.previous_scan.time).toLocaleString() : "needs two scans";
      const g = d.growth || [];
      $("disk-body").innerHTML = g.length ? g.map((x) =>
        `<div class="drow"><span class="nm" title="${esc(x.path)}">${esc(x.path)}</span><span class="muted">${fmtBytes(x.was)} → ${fmtBytes(x.now)}</span><span class="sz ${x.delta > 0 ? "pos" : "neg"}">${x.delta > 0 ? "+" : "−"}${fmtBytes(Math.abs(x.delta))}</span></div>`).join("")
        : `<div class="sub">no changes above the size threshold yet</div>`;
    } else {
      $("crumbs").textContent = "largest single files under " + state.diskRoot;
      const f = d.files || [];
      const max = f.length ? f[0].size : 0;
      $("disk-body").innerHTML = f.length ? f.map((e) => drow({ ...e, name: e.path }, max)).join("") : `<div class="sub">none</div>`;
    }
    document.querySelectorAll("[data-path]").forEach((el) =>
      el.addEventListener("click", () => { state.diskParent = el.dataset.path; state.diskView = "tree"; syncTabs(); loadDisk(); }));
  } catch (e) {}
}
function syncTabs() {
  document.querySelectorAll("#disk-tabs button").forEach((b) => b.classList.toggle("on", b.dataset.view === state.diskView));
}
$("disk-tabs").addEventListener("click", (e) => { if (e.target.dataset.view) { state.diskView = e.target.dataset.view; syncTabs(); loadDisk(); } });
$("disk-root").addEventListener("change", (e) => { state.diskRoot = e.target.value; state.diskParent = e.target.value; loadDisk(); });
$("rescan").addEventListener("click", async () => { await fetch("/api/disk/scan", { method: "POST" }); loadDisk(); });
$("proc-tabs").addEventListener("click", (e) => {
  if (!e.target.dataset.sort) return;
  state.procSort = e.target.dataset.sort;
  document.querySelectorAll("#proc-tabs button").forEach((b) => b.classList.toggle("on", b === e.target));
  loadProcs();
});
$("ranges").addEventListener("click", (e) => {
  if (!e.target.dataset.range) return;
  state.range = e.target.dataset.range;
  document.querySelectorAll("#ranges button").forEach((b) => b.classList.toggle("on", b === e.target));
  loadHistory();
});
window.addEventListener("resize", drawCharts);

// --- live stream with auto-reconnect ---
function connectStream() {
  const es = new EventSource("/api/stream");
  es.onopen = () => { $("live-dot").className = "dot on"; $("live-txt").textContent = "live"; };
  es.onmessage = (ev) => { try { renderSample(JSON.parse(ev.data)); } catch (e) {} };
  es.onerror = () => { $("live-dot").className = "dot off"; $("live-txt").textContent = "reconnecting"; };
}

// Scrollspy: highlight the last section whose top has passed the
// sticky topbar. Deterministic on scroll (an IntersectionObserver
// fires in entry order and can leave the wrong link lit when several
// sections are visible at max scroll). At the very bottom the last
// section wins, since it can never reach the top.
const spyIds = ["overview", "storage", "disk", "processes", "services", "events"];
let spyTick = false;
// Processes/Services sit side by side and the last sections cannot
// scroll to the top, so position alone cannot tell which link the
// user picked. A click locks the highlight to the chosen link until
// the user scrolls on their own (wheel/touch).
let spyLock = false;
document.querySelectorAll("#sidenav a").forEach((a) => {
  a.addEventListener("click", () => {
    spyLock = true;
    document.querySelectorAll("#sidenav a").forEach((x) => x.classList.toggle("on", x === a));
  });
});
["wheel", "touchstart"].forEach((ev) =>
  $("main").addEventListener(ev, () => { spyLock = false; }, { passive: true }));

function updateSpy() {
  spyTick = false;
  if (spyLock) return;
  const main = $("main");
  const barBottom = document.querySelector(".topbar").getBoundingClientRect().bottom;
  let current = spyIds[0];
  for (const id of spyIds) {
    const el = document.getElementById(id);
    if (el && el.getBoundingClientRect().top <= barBottom + 40) current = id;
  }
  if (main.scrollTop + main.clientHeight >= main.scrollHeight - 2) {
    current = spyIds[spyIds.length - 1];
  }
  document.querySelectorAll("#sidenav a").forEach((a) =>
    a.classList.toggle("on", a.getAttribute("href") === "#" + current));
}
$("main").addEventListener("scroll", () => {
  if (!spyTick) { spyTick = true; requestAnimationFrame(updateSpy); }
}, { passive: true });

// First sample immediately, then paced refreshes.
loadOverview(); loadHistory(); loadEnergy(); loadProcs(); loadContainers(); loadServices(); loadEvents(); loadDisk();
connectStream();
setInterval(loadHistory, 15000);
setInterval(() => { loadOverview(); loadProcs(); }, 10000);
setInterval(() => { loadContainers(); loadServices(); loadEvents(); loadEnergy(); }, 30000);
setInterval(loadDisk, 60000);
