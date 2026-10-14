(async () => {
  const {meta, rows, summaries, announcements} = await loadData();
  const params = new URLSearchParams(location.search);
  let product = params.get("product") || "All";
  const thr = meta.significant_threshold, PAGE = 50;
  let sortKey = "commit_date", asc = false, page = 0, tagsOn = new Set(params.get("tags") ? params.get("tags").split(",") : []);

  const opt = (v, l = v) => `<option value="${esc(v)}">${esc(l)}</option>`;
  $("#product").innerHTML = opt("All", "All products") + meta.products.map(p => opt(p.name, `${p.name} (${p.count})`)).join("");
  $("#product").value = product;
  const topics = [...new Set(rows.map(r => r.ms_topic).filter(Boolean))].sort();
  $("#topic").innerHTML += topics.map(t => opt(t)).join("");
  $("#label").innerHTML += (meta.labels || []).map(l => opt(l)).join("");

  function scoped() { return product === "All" ? rows : rows.filter(r => r.product === product); }

  function renderHeader() {
    const title = product === "All" ? "All products" : product;
    $("#hdr").innerHTML = header(meta, ` · ${esc(title)}`);
    renderAnnouncements();
    document.title = `${title} · CAIP Docs Change Tracker`;
    const pr = scoped(), weeks = [...new Set(pr.map(r => r.week_start))].sort().reverse();
    $("#week").innerHTML = opt("", "All weeks") + weeks.map(w => opt(w, fmtWeek(w))).join("");
    const kp = [["Changes", pr.length], ["Significant", pr.filter(r => r.importance >= thr).length],
      ["Pages touched", new Set(pr.map(r => r.file_path)).size], ["New pages", pr.filter(r => r.change_type === "Added").length],
      ["Weeks with changes", weeks.length]];
    $("#kpis").innerHTML = kp.map(([l, v]) => `<div class="card kpi"><div class="label">${l}</div><div class="value">${v.toLocaleString("en-GB")}</div></div>`).join("");
    // summary weeks: weeks that have a summary for this product (or any product when All)
    const sw = new Set();
    Object.entries(summaries).forEach(([p, byWeek]) => { if (product === "All" || p === product) Object.keys(byWeek).forEach(w => sw.add(w)); });
    const swl = [...sw].sort().reverse();
    $("#sumweek").innerHTML = swl.length ? swl.map(w => opt(w, fmtWeek(w))).join("") : opt("", "No summaries yet");
    renderSummary();
    const tagCount = {};
    pr.forEach(r => r.tags.split(";").filter(Boolean).forEach(t => tagCount[t] = (tagCount[t] || 0) + 1));
    $("#tagbar").innerHTML = '<span class="muted" style="font-size:12px;align-self:center">Categories:</span>' +
      Object.entries(tagCount).sort((a, b) => b[1] - a[1]).map(([t, c]) =>
        `<button class="tagbtn ${tagsOn.has(t) ? "on" : ""}" data-t="${esc(t)}">${esc(t)} <span class="muted">${c}</span></button>`).join("");
    $("#tagbar").querySelectorAll(".tagbtn").forEach(b => b.onclick = () => {
      const t = b.dataset.t; tagsOn.has(t) ? tagsOn.delete(t) : tagsOn.add(t); b.classList.toggle("on"); page = 0; renderTable(); });
  }

  function renderAnnouncements() {
    const mine = announcements.filter(a => product === "All" || a.products.split(";").includes(product));
    const p = meta.products.find(x => x.name === product);
    $("#annsub").innerHTML = `· ${mine.length} matched` + (p ? ` · <a href="feeds/${esc(p.slug)}.xml">RSS for ${esc(product)}</a>` : ' · <a href="feeds/all.xml">RSS</a>');
    $("#anns").innerHTML = mine.length ? `<ul class="alist">${mine.slice(0, 20).map(a =>
      `<li>${labelBadge(a.label)}<a href="${esc(a.url)}" target="_blank" rel="noopener">${esc(a.title)}</a>
        <span class="muted">· ${fmtDate(a.published)} · ${a.doc_changes || 0} matching doc changes</span></li>`).join("")}</ul>`
      : '<p class="muted">No announcements matched to this product yet.</p>';
  }

  function renderSummary() {
    const w = $("#sumweek").value;
    $("#sumsub").textContent = w ? `· ${fmtWeek(w)} · ranked by customer impact` : "";
    const blocks = Object.entries(summaries).filter(([p, byWeek]) => (product === "All" || p === product) && byWeek[w]);
    if (!blocks.length) { $("#summary").innerHTML = '<p class="muted">No summary for this week yet. Summaries are produced by the Thursday scan.</p>'; return; }
    const order = {High: 0, Medium: 1, Low: 2};
    const items = blocks.flatMap(([p, byWeek]) => byWeek[w].bullets.map((b, i) => ({...b, product: p, i})))
      .sort((a, b) => (order[a.impact] - order[b.impact]) || (a.i - b.i));
    if (!items.length) { $("#summary").innerHTML = '<p class="muted">Only minor or metadata-only edits this week, so there is nothing to summarise.</p>'; return; }
    $("#summary").innerHTML = `<ol>${items.map(b => `<li><span class="badge ${esc(b.impact)}">${esc(b.impact)}</span>
      ${product === "All" ? `<span class="chip">${esc(b.product)}</span>` : ""}<span class="hl">${esc(b.headline)}</span>
      <div class="det">${esc(b.detail)}</div><div class="links">${(b.urls || []).map(u => `<a href="${esc(u)}" target="_blank" rel="noopener">${esc(u.replace(/^https:\/\/(learn\.microsoft\.com\/en-us|github\.com)/, ""))}</a>`).join("")}</div></li>`).join("")}</ol>
      <p class="note">Model: ${esc([...new Set(blocks.map(([, bw]) => bw[w].provider))].join(", "))} · generated ${fmtDate(blocks[0][1][w].generated_at)}</p>`;
  }

  // Dates sort by time: commit_date carries the committer's UTC offset (use commit_utc), and ms.date is
  // MM/DD/YYYY (reorder it to YYYYMMDD).
  function sortValue(r, k) {
    const v = r[k];
    if (k === "commit_date") return r.commit_utc || v;
    if (k !== "ms_date") return v;
    const m = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/.exec(v || "");
    return m ? m[3] + m[1].padStart(2, "0") + m[2].padStart(2, "0") : String(v || "");
  }

  function filtered() {
    const q = $("#q").value.trim().toLowerCase(), wk = $("#week").value, ct = $("#ctype").value, tp = $("#topic").value;
    const mi = +$("#minimp").value, hide = $("#hideminor").checked, lb = $("#label").value, silentOnly = $("#silentonly").checked;
    return scoped().filter(r => {
      const tags = r.tags.split(";");
      if (hide && (r.noise === "true" || r.metadata_only === "true" || tags.includes("minor-edit"))) return false;
      if (lb && r.change_label !== lb) return false;
      if (silentOnly && !r.silent) return false;
      if (wk && r.week_start !== wk) return false;
      if (ct && r.change_type !== ct) return false;
      if (tp && r.ms_topic !== tp) return false;
      if (r.importance < mi) return false;
      if (tagsOn.size && ![...tagsOn].some(t => tags.includes(t))) return false;
      if (q && ![r.title, r.url, r.change_summary, r.commit_subject, r.author, r.file_path, r.key_lines, r.description, r.ms_service]
        .some(v => (v || "").toLowerCase().includes(q))) return false;
      return true;
    }).sort((a, b) => {
      const x = sortValue(a, sortKey), y = sortValue(b, sortKey);
      const c = typeof x === "number" ? x - y : String(x).localeCompare(String(y));
      return asc ? c : -c;
    });
  }

  function renderTable() {
    const data = filtered(), pages = Math.max(1, Math.ceil(data.length / PAGE));
    page = Math.min(page, pages - 1);
    $("#count").textContent = `· ${data.length.toLocaleString("en-GB")} matching rows`;
    $("#tbl tbody").innerHTML = data.slice(page * PAGE, (page + 1) * PAGE).map(r => `<tr>
      <td>${fmtDate(r.commit_date)}</td>
      <td>${r.url ? `<a href="${esc(r.url)}" target="_blank" rel="noopener">${esc(r.title || r.file_path)}</a>` : esc(r.title || r.file_path)}
        <div class="path">${esc(r.file_path)}${r.old_path ? ` (from ${esc(r.old_path)})` : ""}</div>
        ${product === "All" ? `<span class="chip">${esc(r.product)}</span>` : ""}</td>
      <td><span class="ct ${esc(r.change_type)}">${esc(r.change_type)}</span><div style="margin-top:4px">${labelBadge(r.change_label)}</div>
        ${r.silent ? '<div class="note" title="No announcement for this product within 14 days">silent</div>' : ""}
        ${(r.announcements || []).map(a => `<div class="note"><a href="${esc(a.url)}" target="_blank" rel="noopener" title="${esc(a.title)}">announced</a></div>`).join("")}</td>
      <td>${r.tags.split(";").filter(Boolean).map(t => `<span class="chip ${HOT.has(t) ? "hot" : ""}">${esc(t)}</span>`).join("")}</td>
      <td>${esc(r.change_summary)} <a href="${esc(r.commit_url)}" target="_blank" rel="noopener" title="${esc(r.commit_subject)}">${esc(r.commit_sha.slice(0, 7))}</a>
        ${r.pr_number ? ` · PR #${esc(r.pr_number)}` : ""}${r.key_lines ? `<div class="ev">“${esc(r.key_lines)}”</div>` : ""}</td>
      <td>${esc(r.author)}</td><td>${esc(r.ms_topic)}</td><td>${esc(r.ms_date)}</td>
      <td class="num"><span style="color:#256a35">+${r.lines_added}</span> <span style="color:#a1243a">−${r.lines_removed}</span></td>
      <td class="num"><span class="imp" style="${impColour(r.importance)}">${r.importance}</span></td></tr>`).join("")
      || '<tr><td colspan="10" class="muted">No changes match these filters.</td></tr>';
    $("#pinfo").textContent = `Page ${page + 1} of ${pages}`;
    $("#prev").disabled = page === 0; $("#next").disabled = page >= pages - 1;
    document.querySelectorAll("th").forEach(th => th.className = (th.dataset.k === sortKey ? "sorted" + (asc ? " asc" : "") : "") + (th.classList.contains("num") ? " num" : ""));
  }

  document.querySelectorAll("th").forEach(th => th.onclick = () => {
    const k = th.dataset.k; if (sortKey === k) asc = !asc; else { sortKey = k; asc = false; } renderTable(); });
  ["q", "week", "ctype", "topic", "minimp", "hideminor", "label", "silentonly"].forEach(id => $("#" + id).addEventListener("input", () => { page = 0; renderTable(); }));
  $("#product").onchange = () => { product = $("#product").value; history.replaceState(null, "", `?product=${encodeURIComponent(product)}`); tagsOn.clear(); page = 0; renderHeader(); renderTable(); };
  $("#sumweek").onchange = renderSummary;
  $("#prev").onclick = () => { page--; renderTable(); }; $("#next").onclick = () => { page++; renderTable(); };
  $("#reset").onclick = () => { ["q", "week", "ctype", "topic", "label"].forEach(id => $("#" + id).value = ""); $("#minimp").value = "0"; $("#hideminor").checked = true; $("#silentonly").checked = false; tagsOn.clear(); page = 0; renderHeader(); renderTable(); };
  $("#export").onclick = () => {
    const cols = ["commit_date", "product", "title", "url", "change_type", "change_label", "silent", "tags", "change_summary", "commit_subject", "commit_url", "pr_number", "author", "ms_topic", "ms_service", "ms_date", "lines_added", "lines_removed", "importance", "file_path"];
    const csv = [cols.join(","), ...filtered().map(r => cols.map(c => `"${String(r[c] ?? "").replace(/"/g, '""')}"`).join(","))].join("\n");
    const a = Object.assign(document.createElement("a"), {href: URL.createObjectURL(new Blob([csv], {type: "text/csv"})), download: `changes-${product}.csv`});
    a.click();
  };
  renderHeader();
  ["q", "week", "ctype", "topic", "minimp", "label"].forEach(id => { if (params.get(id)) $("#" + id).value = params.get(id); });
  if (params.get("silent") === "1") $("#silentonly").checked = true;
  renderTable();
})();
