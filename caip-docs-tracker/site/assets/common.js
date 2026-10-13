const $ = (s, el = document) => el.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const fmtDate = iso => iso ? new Date(iso).toLocaleDateString("en-GB", {day:"2-digit", month:"short", year:"numeric"}) : "";
const fmtWeek = w => "w/c " + new Date(w + "T00:00:00Z").toLocaleDateString("en-GB", {day:"2-digit", month:"short", year:"numeric", timeZone:"UTC"});
const HOT = new Set(["breaking-change","retirement","ga","security","pricing-quota"]);
const LABEL_CLASS = {"Breaking change": "High", "Retirement": "High", "GA": "Medium", "Preview": "Medium", "New": "Low", "Doc update": "Low"};
const labelBadge = l => l ? `<span class="badge ${LABEL_CLASS[l] || "Low"}">${esc(l)}</span>` : "";
async function loadData() {
  // announcements.json is newer than the other files; treat it as empty if a site predates it.
  const get = (n, fallback) => fetch(`data/${n}.json`, {cache: "no-cache"}).then(r => r.ok ? r.json() : fallback).catch(() => fallback);
  const [meta, rows, summaries, announcements] = await Promise.all(
    [get("meta"), get("changes"), get("summaries"), get("announcements", [])]);
  return {meta, rows, summaries, announcements};
}
function header(meta, extra = "") {
  const ls = meta.last_scan;
  const scan = ls && ls.run_at ? `Last scan ${fmtDate(ls.run_at)} · ${esc(ls.status)}` : "No scan yet";
  const repos = meta.sources.map(s => s.repo).join(", "), feeds = (meta.feeds || []).map(f => f.url).join(", ");
  return `<header class="top"><div><h1>${esc(meta.name)}${extra}</h1>
    <div class="sub">Tracking <span title="${esc(repos)}">${meta.sources.length} repositories</span> and
      <span title="${esc(feeds)}">${(meta.feeds || []).length} announcement feeds</span> · since ${fmtDate(meta.start_date)} · ${scan}</div></div>
    <div class="sub"><a href="index.html">Overview</a> · <a href="product.html?product=All">All changes</a> · <a href="feeds/all.xml">RSS</a></div></header>`;
}
function impColour(v) { return v >= 50 ? "background:#fde7ea;color:#a1243a" : v >= 25 ? "background:#fdf1dc;color:#8a5a00" : "background:#eef0f3;color:#4a525c"; }
