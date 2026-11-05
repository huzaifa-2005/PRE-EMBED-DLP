const API = "http://localhost:8080";
const $ = (s, r = document) => r.querySelector(s);
const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

// nav bar
const pages = [["index.html", "Home"], ["ingest.html", "Ingest"], ["documents.html", "Documents"], ["requests.html", "Data requests"], ["demo.html", "Leak demo"]];
const here = location.pathname.split("/").pop() || "index.html";
$("#nav").innerHTML = `<header><div class="w"><nav>
  <a class="logo" href="index.html"><svg viewBox="0 0 24 24" fill="none" stroke="#c9a96a" stroke-width="1.4"><path d="M12 2l9 4v6c0 5-3.8 8.6-9 10-5.2-1.4-9-5-9-10V6z"/><path d="M8 12h8M8 15h5"/></svg>Pre-Embed DLP</a>
  <ul>${pages.map(([h, t]) => `<li><a href="${h}" class="${h === here ? "on" : ""}">${t}</a></li>`).join("")}</ul>
</nav></div></header>`;

// vector store + embedder dropdowns (choice is remembered between pages)
function pickers(el) {
  el.innerHTML = `<div class="row">
    <div><label>Vector store</label><select id="store"><option value="qdrant">Qdrant</option><option value="pgvector">pgvector</option></select></div>
    <div><label>Embedding model</label><select id="embedder"><option value="gemini">Gemini</option><option value="cohere">Cohere</option></select></div></div>`;
  $("#store").value = localStorage.getItem("store") || "qdrant";
  $("#embedder").value = localStorage.getItem("embedder") || "gemini";
  el.onchange = () => {
    localStorage.setItem("store", $("#store").value);
    localStorage.setItem("embedder", $("#embedder").value);
  };
}
const qs = () => `store=${$("#store").value}&embedder=${$("#embedder").value}`;

async function api(path, opts) {
  const r = await fetch(API + path, opts);
  const t = await r.text();
  if (!r.ok) throw new Error(t || r.statusText);
  try { return JSON.parse(t); } catch { return t; }
}

function show(el, data, bad) {
  el.className = "out" + (bad ? " bad" : "");
  el.textContent = typeof data === "string" ? data : JSON.stringify(data, null, 2);
}

async function run(btn, out, fn) {
  btn.disabled = true;
  show(out, "Working…");
  try { show(out, await fn()); } catch (e) { show(out, e.message, true); }
  btn.disabled = false;
}