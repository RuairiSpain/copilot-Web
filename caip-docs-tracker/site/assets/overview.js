(async () => {
  const {meta, rows, summaries, announcements} = await loadData();
  $("#hdr").innerHTML = header(meta);
  $("#thr").textContent = meta.significant_threshold;
  const thr = meta.significant_threshold, weeks = meta.weeks, last = weeks[weeks.length - 1];
  const sig = rows.filter(r => r.importance >= thr);
  const pages = new Set(rows.map(r => r.file_path)).size;
  const count = (f) => rows.filter(f).length;
  const hasTag = (r, t) => r.tags.split(";").includes(t);
  const lastWeekWithData = [...weeks].reverse().find((w, i) => meta.series.total[weeks.length - 1 - i] > 0) || last;
  const kpis = [
    ["Total changes", rows.length, `${pages} pages touched`],
    ["Significant", sig.length, `${Math.round(100 * sig.length / Math.max(1, rows.length))}% of all changes`],
    ["Latest week", meta.series.total[weeks.indexOf(lastWeekWithData)] || 0, fmtWeek(lastWeekWithData)],
    ["New pages", count(r => r.change_type === "Added"), `${count(r => r.change_type === "Deleted")} removed`],
    ["Retirement / breaking", count(r => hasTag(r, "retirement") || hasTag(r, "breaking-change")), "flagged rows"],
    ["Preview / GA", count(r => hasTag(r, "preview") || hasTag(r, "ga")), "flagged rows"],
    ["Announcements", announcements.length, `${new Set(announcements.map(a => a.feed_id)).size} feeds`],
    ["Silent changes", count(r => r.silent), "significant, no announcement within 14 days"],
  ];
  $("#kpis").innerHTML = kpis.map(([l, v, n]) => `<div class="card kpi"><div class="label">${l}</div><div class="value">${v.toLocaleString("en-GB")}</div><div class="note">${esc(n)}</div></div>`).join("");

  // Charts come from a CDN; if it is blocked, still show the KPIs and product cards.
  if (typeof Chart !== "undefined") {
  new Chart($("#trend"), {type: "line", data: {labels: weeks.map(w => fmtWeek(w).slice(4)), datasets: [
      {label: "All changes", data: meta.series.total, borderColor: "#0f6cbd", backgroundColor: "rgba(15,108,189,.08)", fill: true, tension: .25, pointRadius: 3},
      {label: "Significant", data: meta.series.significant, borderColor: "#c4314b", tension: .25, pointRadius: 3},
      {label: "Pages touched", data: meta.series.pages, borderColor: "#7a3fb8", borderDash: [5, 4], tension: .25, pointRadius: 2},
      {label: "Silent changes", data: meta.series.silent || [], borderColor: "#c27c0e", tension: .25, pointRadius: 2},
      {label: "Announcements", data: meta.series.announcements || [], borderColor: "#3a7d44", borderDash: [2, 3], tension: .25, pointRadius: 2}]},
    options: {maintainAspectRatio: false, interaction: {mode: "index", intersect: false},
      plugins: {legend: {position: "bottom"}}, scales: {y: {beginAtZero: true, ticks: {precision: 0}}}}});

  const tagCounts = {};
  rows.forEach(r => r.tags.split(";").filter(Boolean).forEach(t => tagCounts[t] = (tagCounts[t] || 0) + 1));
  const topTags = Object.entries(tagCounts).filter(([t]) => t !== "metadata-only" && t !== "minor-edit").sort((a, b) => b[1] - a[1]).slice(0, 10);
  new Chart($("#tags"), {type: "bar", data: {labels: topTags.map(t => t[0]), datasets: [{label: "Rows", data: topTags.map(t => t[1]),
      backgroundColor: topTags.map(([t]) => HOT.has(t) ? "#c4314b" : "#0f6cbd")}]},
    options: {indexAxis: "y", maintainAspectRatio: false, plugins: {legend: {display: false}, title: {display: true, text: "Change categories (all weeks)"}},
      scales: {x: {beginAtZero: true, ticks: {precision: 0}}}}});
  } else {
    document.querySelectorAll(".chart-wrap").forEach(el => el.innerHTML = '<p class="muted">Charts could not be loaded.</p>');
  }

  $("#products").innerHTML = meta.products.map(p => {
    const pr = rows.filter(r => r.product === p.name);
    const s = pr.filter(r => r.importance >= thr).length;
    const lw = pr.filter(r => r.week_start === lastWeekWithData).length;
    const order = {High: 0, Medium: 1, Low: 2};
    const bullets = (summaries[p.name] && summaries[p.name][lastWeekWithData] && summaries[p.name][lastWeekWithData].bullets) || [];
    const top = [...bullets].sort((a, b) => order[a.impact] - order[b.impact])[0] || null;
    return `<a class="card pcard" href="product.html?product=${encodeURIComponent(p.name)}"><div class="name">${esc(p.name)}</div>
      <div class="stats"><div><b>${p.count}</b>changes</div><div><b>${s}</b>significant</div><div><b>${lw}</b>latest week</div><div><b>${p.announcements || 0}</b>announced</div></div>
      ${top ? `<div class="note"><span class="badge ${esc(top.impact)}">${esc(top.impact)}</span>${esc(top.headline)}</div>` : ""}</a>`;
  }).join("");

  $("#announcements").innerHTML = announcements.length ? `<ul class="alist">${announcements.slice(0, 12).map(a =>
    `<li>${labelBadge(a.label)}<a href="${esc(a.url)}" target="_blank" rel="noopener">${esc(a.title)}</a>
      <span class="muted">· ${fmtDate(a.published)}${a.products ? " · " + esc(a.products.split(";").join(", ")) : ""}</span></li>`).join("")}</ul>`
    : '<p class="muted">No announcements collected yet.</p>';
  $("#feeds").innerHTML = '<a href="feeds/all.xml">All products</a>' +
    meta.products.map(p => ` · <a href="feeds/${esc(p.slug)}.xml">${esc(p.name)}</a>`).join("");
})();
