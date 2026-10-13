const $ = (s, el = document) => el.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const fmtDate = iso => iso ? new Date(iso).toLocaleDateString("en-GB", {day:"2-digit", month:"short", year:"numeric"}) : "";
const fmtWeek = w => "w/c " + new Date(w + "T00:00:00Z").toLocaleDateString("en-GB", {day:"2-digit", month:"short", year:"numeric", timeZone:"UTC"});
const HOT = new Set(["breaking-change","retirement","ga","security","pricing-quota"]);
async function loadData() {
  const [meta, rows, summaries] = await Promise.all(
    ["meta", "changes", "summaries"].map(n => fetch(`data/${n}.json`, {cache: "no-cache"}).then(r => r.json())));
  return {meta, rows, summaries};
}
function header(meta, extra = "") {
  const ls = meta.last_scan;
  const scan = ls && ls.run_at ? `Last scan ${fmtDate(ls.run_at)} · ${esc(ls.status)}` : "No scan yet";
  const repos = meta.sources.map(s => `<a href="https://github.com/${esc(s.repo)}">${esc(s.repo)}</a>`).join(", ");
  return `<header class="top"><div><h1>${esc(meta.name)}${extra}</h1>
    <div class="sub">Tracking ${repos} · since ${fmtDate(meta.start_date)} · ${scan}</div></div>
    <div class="sub"><a href="index.html">Overview</a> · <a href="product.html?product=All">All changes</a></div></header>`;
}
function impColour(v) { return v >= 50 ? "background:#fde7ea;color:#a1243a" : v >= 25 ? "background:#fdf1dc;color:#8a5a00" : "background:#eef0f3;color:#4a525c"; }
