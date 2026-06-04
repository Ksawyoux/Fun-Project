// Wiki reader: fetches the generated Code-Wiki, renders a TOC sidebar and
// per-subsystem narrative pages with inline mermaid diagrams and clickable
// deep code links. Talks to the serving layer's /v1/wiki endpoint; resolves
// code links through the local /api/source proxy.

let CONFIG = { serving: "http://localhost:8081", namespace: "acme" };
let WIKI = null;

if (window.mermaid) {
  mermaid.initialize({ startOnLoad: false, theme: "dark", securityLevel: "loose" });
}

async function boot() {
  try {
    const cfg = await fetch("/api/config").then((r) => r.json());
    CONFIG.serving = cfg.serving || CONFIG.serving;
    CONFIG.namespace = cfg.namespace || CONFIG.namespace;
  } catch (e) {
    /* fall back to defaults */
  }
  document.getElementById("ns-label").textContent = CONFIG.namespace;
  await loadWiki();
}

async function loadWiki() {
  const url = `${CONFIG.serving}/v1/wiki?namespace=${encodeURIComponent(CONFIG.namespace)}`;
  try {
    const res = await fetch(url);
    if (!res.ok) {
      const body = await res.text();
      showError(`Wiki generation failed (${res.status}). ${body}`);
      return;
    }
    WIKI = await res.json();
  } catch (e) {
    showError("Could not reach the serving layer at " + CONFIG.serving + ". Is the stack running?");
    return;
  }
  renderTOC();
  if (WIKI.topics && WIKI.topics.length) {
    selectTopic(WIKI.topics[0].id);
  } else {
    showError("The wiki is empty — has this namespace been ingested yet?");
  }
}

function renderTOC() {
  const toc = document.getElementById("toc");
  toc.innerHTML = "";
  (WIKI.topics || []).forEach((t) => {
    const div = document.createElement("div");
    div.className = "toc-item";
    div.id = "toc-" + t.id;
    div.onclick = () => selectTopic(t.id);
    div.innerHTML = `<div class="toc-title">${escapeHtml(t.title)}</div>` +
      (t.summary ? `<div class="toc-summary">${escapeHtml(t.summary)}</div>` : "");
    toc.appendChild(div);
  });
}

function selectTopic(id) {
  document.querySelectorAll(".toc-item").forEach((el) => el.classList.remove("active"));
  const tocEl = document.getElementById("toc-" + id);
  if (tocEl) tocEl.classList.add("active");

  const page = WIKI.pages && WIKI.pages[id];
  const content = document.getElementById("content");
  if (!page) {
    content.innerHTML = `<div class="placeholder">No page generated for this topic.</div>`;
    return;
  }

  const cached = page.cached ? `<span class="badge">cached</span>` : "";
  const llm = page.used_llm ? `<span class="badge">${escapeHtml(page.used_llm)}</span>` : "";
  const nCites = (page.citations || []).length;
  content.innerHTML =
    `<h1>${escapeHtml(page.title)}</h1>` +
    (page.summary ? `<p class="page-summary">${escapeHtml(page.summary)}</p>` : "") +
    `<div class="page-meta">${llm}${cached}<span class="badge">${nCites} code links</span></div>` +
    `<div id="page-body"></div>`;

  const body = document.getElementById("page-body");
  body.innerHTML = window.marked ? marked.parse(page.markdown || "") : escapeHtml(page.markdown || "");

  renderMermaid(body);
  wireCodeLinks(body);
  content.scrollTop = 0;
}

// marked renders ```mermaid blocks as <pre><code class="language-mermaid">.
// Convert those into <div class="mermaid"> and run the renderer.
function renderMermaid(root) {
  if (!window.mermaid) return;
  root.querySelectorAll("code.language-mermaid").forEach((code, i) => {
    const div = document.createElement("div");
    div.className = "mermaid";
    div.textContent = code.textContent;
    const pre = code.closest("pre");
    (pre || code).replaceWith(div);
  });
  try {
    mermaid.run({ nodes: root.querySelectorAll(".mermaid") });
  } catch (e) {
    /* diagram syntax issues shouldn't break the page */
  }
}

// Code links look like [name](path/to/file.go#L42). Rewrite anchors whose href
// is a source path so they open the file via the local source proxy.
function wireCodeLinks(root) {
  root.querySelectorAll("a").forEach((a) => {
    const href = a.getAttribute("href") || "";
    if (/^https?:\/\//.test(href) || href.startsWith("#") || href.startsWith("mailto:")) return;
    const m = href.match(/^([^#?]+?)(?:#L(\d+))?$/);
    if (!m) return;
    const path = m[1];
    const line = m[2] ? parseInt(m[2], 10) : 0;
    if (!/\.[a-zA-Z0-9]+$/.test(path)) return; // looks like a file
    a.classList.add("codelink");
    a.href = "javascript:void(0)";
    a.onclick = (ev) => {
      ev.preventDefault();
      openSrc(path, line);
    };
  });
}

async function openSrc(path, line) {
  const title = document.getElementById("src-title");
  const bodyEl = document.getElementById("src-body");
  title.textContent = path + (line ? ":" + line : "");
  bodyEl.innerHTML = '<div class="spinner">loading…</div>';
  document.getElementById("src-modal").classList.add("open");

  const url = `/api/source?path=${encodeURIComponent(path)}&namespace=${encodeURIComponent(CONFIG.namespace)}`;
  try {
    const res = await fetch(url);
    const text = await res.text();
    if (!res.ok) {
      bodyEl.innerHTML = `<div class="placeholder">${escapeHtml(text || "file not found")}</div>`;
      return;
    }
    renderSource(bodyEl, text, line);
  } catch (e) {
    bodyEl.innerHTML = `<div class="placeholder">Could not load source.</div>`;
  }
}

function renderSource(el, text, line) {
  const lines = text.split("\n");
  const pad = String(lines.length).length;
  const html = lines
    .map((ln, i) => {
      const n = i + 1;
      const cls = n === line ? "hl" : "";
      const num = String(n).padStart(pad, " ");
      return `<span class="${cls}">${num}  ${escapeHtml(ln)}</span>`;
    })
    .join("\n");
  el.innerHTML = `<pre>${html}</pre>`;
  if (line) {
    const hl = el.querySelector(".hl");
    if (hl) hl.scrollIntoView({ block: "center" });
  }
}

function closeSrc() {
  document.getElementById("src-modal").classList.remove("open");
}

function showError(msg) {
  document.getElementById("content").innerHTML = `<div class="placeholder">${escapeHtml(msg)}</div>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c])
  );
}

boot();
