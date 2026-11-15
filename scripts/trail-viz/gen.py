# -*- coding: utf-8 -*-
"""Trail wiring.json -> интерактивный force-граф (self-contained HTML).

Раскладка считается в Python (vectorized Fruchterman-Reingold на numpy)
и вшивается в HTML готовыми координатами: JS делает только pan/zoom/drag/
highlight/поиск. Запуск:
  python -I gen.py <wiring.json> trail-viz/index.html

  Путь к wiring.json зависит от окружения: .trail/cache/.graph/wiring.json
  (репо с .trail/) или graft/wiring.json (дефолт trail без .trail/, как в CI).
"""
import json, sys, pathlib, math
import numpy as np

WIRING, OUT = sys.argv[1], sys.argv[2]
data = json.load(open(WIRING, encoding="utf-8"))

nodes_in, edges_in = data["nodes"], data["edges"]
ids = {n["id"] for n in nodes_in}
edges = [(e["source"], e["target"], e["relation"])
         for e in edges_in
         if e["source"] in ids and e["target"] in ids]
nodes = nodes_in
N = len(nodes)

# --- раскладка (vectorized FR) ---
AREA_W, AREA_H = 2200.0, 1500.0
scope_left = np.array([n["id"].startswith("notifier") for n in nodes])
rng = np.random.default_rng(42)
pos = np.empty((N, 2))
pos[scope_left, 0] = rng.uniform(200, 900, scope_left.sum())
pos[~scope_left, 0] = rng.uniform(1300, 2000, (~scope_left).sum())
pos[:, 1] = rng.uniform(200, AREA_H - 200, N)

# скоуп-центры (лёгкая гравитация, чтобы кластеры держались по сторонам)
centers = np.where(scope_left[:, None], np.array([550.0, AREA_H / 2]), np.array([1650.0, AREA_H / 2]))

k = 0.9 * np.sqrt(AREA_W * AREA_H / N)  # идеальное расстояние (~118px)

# веса рёбер: contains — структурные (символ у своего файла), слабее;
# calls/imports/extends — смысловые, сильнее
wmap = {"contains": 0.15, "extends": 0.6}
ea, eb, ew = [], [], []
idx = {n["id"]: i for i, n in enumerate(nodes)}
for s, t, r in edges:
    if s not in ids or t not in ids:
        continue
    ea.append(idx[s]); eb.append(idx[t])
    ew.append(wmap.get(r, 1.0))
ea, eb, ew = np.array(ea), np.array(eb), np.array(ew)

# Детерминированная кластерная раскладка: каждый файл — ячейка, его символы
# в мини-сетке вокруг файла; notifier и core-api — два блока рядом.
# Физсимулятор на этом графе (404 ребра на 267 узлов, слабо связный) слипается
# или разлетается при любых параметрах — сетка даёт гарантированную читаемость.
idx = {n["id"]: i for i, n in enumerate(nodes)}
contains = [(s, t) for s, t, r in edges if r == "contains"]
members = {}  # file_id -> [node_idx]
for s, t in contains:
    members.setdefault(s, []).append(idx[t])
file_ids = [n["id"] for n in nodes if n["kind"] == "file"]
orphan_syms = [i for i in range(N) if nodes[i]["kind"] != "file"
               and not any(i in ms for ms in members.values())]

CELL, GAP = 300, 60
per_row = 4

def block(origin_x, fids):
    """Расставить файлы скоупа сеткой; вернуть (макс.высота, x-граница)."""
    global pos
    col_h = [0] * per_row
    max_x = origin_x
    for fi, fid in enumerate(fids):
        cx, cy = divmod(fi, per_row)
        ox = origin_x + cx * (CELL + GAP)
        oy = cy * (CELL + GAP)
        col_h[cy] = max(col_h[cy], oy + CELL)
        max_x = max(max_x, ox + CELL)
        # файл в центре ячейки, символы кольцами вокруг (макс 18/файл:
        # кольцо 62px до 8, кольцо 104px до 12)
        fx, fy = ox + CELL / 2, oy + CELL / 2
        pos[idx[fid]] = (fx, fy)
        syms = members.get(fid, [])
        for k, si in enumerate(syms):
            if k < 8:
                rr, ang = 62.0, 2 * math.pi * k / 8
            else:
                rr, ang = 104.0, 2 * math.pi * (k - 8) / max(1, len(syms) - 8)
            pos[si] = (fx + rr * math.cos(ang), fy + rr * math.sin(ang))
    return max(col_h), max_x

scope_files = {
    "notifier": sorted(f for f in file_ids if f.startswith("notifier")),
    "core-api": sorted(f for f in file_ids if f.startswith("core-api")),
}
pos = np.zeros((N, 2))
y0 = 60
h1, xmax1 = block(80, scope_files["notifier"])
h2, xmax2 = block(xmax1 + 160, scope_files["core-api"])

# сироты без contains-ребра — в отдельную ячейку справа
if orphan_syms:
    for k, si in enumerate(orphan_syms):
        pos[si] = (xmax2 + 120 + (k % 5) * 52, 92 + (k // 5) * 52)

# вложенные символы (contains от символа-родителя: методы классов) — вокруг родителя
file_set = {n["id"] for n in nodes if n["kind"] == "file"}
nested = [(s, t) for s, t, r in edges if r == "contains" and s not in file_set]
per_parent: dict[str, list[int]] = {}
for s, t in nested:
    per_parent.setdefault(s, []).append(idx[t])
for parent, kids in per_parent.items():
    px, py = pos[idx[parent]]
    for k, ki in enumerate(kids):
        ang = 2 * math.pi * k / max(1, len(kids))
        pos[ki] = (px + 30 * math.cos(ang), py + 30 * math.sin(ang))

AREA_W = max(xmax2, 2600) + 320 if orphan_syms else max(xmax2, 2600)
AREA_H = max(h1, h2, 1000) + 120

# --- контроль качества раскладки ---
D = np.sqrt(((pos[:, None, :] - pos[None, :, :]) ** 2).sum(-1))
off = D + np.eye(N) * 1e9
print(f"layout: min pair dist={off.min():.1f}px  median={np.median(off):.0f}px")
for sc, mask in (("notifier", scope_left), ("core-api", ~scope_left)):
    if mask.any():
        print(f"  {sc}: x [{pos[mask,0].min():.0f}..{pos[mask,0].max():.0f}]  "
              f"y [{pos[mask,1].min():.0f}..{pos[mask,1].max():.0f}]")
print(f"canvas {AREA_W:.0f}x{AREA_H:.0f}, orphans: {len(orphan_syms)}")

nodes_out = [
    {"id": n["id"], "name": n["name"], "kind": n["kind"],
     "path": n.get("path") or n["id"].split("#")[0],
     "span": n.get("span") or "", "sig": n.get("signature"),
     "x": round(float(pos[i][0]), 1), "y": round(float(pos[i][1]), 1)}
    for i, n in enumerate(nodes)
]
edges_out = [{"s": s, "t": t, "r": r} for s, t, r in edges]
payload = json.dumps(
    {"nodes": nodes_out, "edges": edges_out, "w": AREA_W, "h": AREA_H},
    ensure_ascii=False)

TEMPLATE = r"""<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>Trail-Code graph — real-estate-pro</title>
<style>
  :root { color-scheme: dark; }
  html, body { margin:0; height:100%; overflow:hidden; background:#0b1020;
               font: 13px/1.4 "Segoe UI", system-ui, sans-serif; color:#e2e8f0; }
  svg { display:block; width:100vw; height:100vh; cursor:grab; }
  svg:active { cursor:grabbing; }
  .panel { position:fixed; top:12px; left:12px; background:rgba(13,20,38,.92);
           border:1px solid #24304d; border-radius:10px; padding:10px 12px;
           max-width:250px; box-shadow:0 6px 24px rgba(0,0,0,.5); }
  .panel h1 { font-size:13px; margin:0 0 6px; color:#7dd3fc; }
  .legend { display:flex; flex-wrap:wrap; gap:4px 10px; margin-bottom:8px; }
  .legend span { display:inline-flex; align-items:center; gap:5px; font-size:11px; color:#94a3b8; }
  .dot { width:9px; height:9px; border-radius:50%; display:inline-block; }
  label.chk { display:flex; align-items:center; gap:5px; font-size:11px; color:#94a3b8;
              margin:2px 0; cursor:pointer; user-select:none; }
  input[type=text] { width:100%; box-sizing:border-box; background:#111a2e; color:#e2e8f0;
           border:1px solid #2c3a5c; border-radius:6px; padding:5px 8px; font-size:12px; }
  button { background:#1c2a4a; color:#93c5fd; border:1px solid #2c3a5c; border-radius:6px;
           padding:4px 10px; font-size:11px; cursor:pointer; margin-top:6px; margin-right:6px; }
  button:hover { background:#26375d; }
  .hint { font-size:10px; color:#64748b; margin-top:6px; }
  .tooltip { position:fixed; pointer-events:none; background:#0f1729ee; border:1px solid #334;
             border-radius:8px; padding:8px 10px; font-size:12px; max-width:420px;
             display:none; z-index:10; box-shadow:0 4px 18px rgba(0,0,0,.6); }
  .tooltip .t-name { font-weight:600; color:#7dd3fc; }
  .tooltip .t-path { color:#94a3b8; font-size:11px; }
  .tooltip .t-sig { color:#a5b4fc; font-size:11px; font-family:Consolas,monospace;
                    white-space:pre-wrap; margin-top:3px; }
  .edge-calls   { stroke:#38bdf8; stroke-opacity:.30; }
  .edge-imports { stroke:#4ade80; stroke-opacity:.20; stroke-dasharray:4 3; }
  .edge-contains{ stroke:#33415c; stroke-opacity:.22; }
  .edge-extends { stroke:#f472b6; stroke-opacity:.45; }
  .edge-dim { stroke-opacity:.04 !important; }
  text.nlabel { font-size:10px; fill:#cbd5e1; pointer-events:none;
                paint-order:stroke; stroke:#0b1020; stroke-width:3px; }
  text.flabel { font-size:11px; fill:#e2e8f0; font-weight:600;
                paint-order:stroke; stroke:#0b1020; stroke-width:3px; pointer-events:none; }
</style>
</head>
<body>
<div class="panel">
  <h1>Trail-Code · real-estate-pro</h1>
  <div class="legend">
    <span><i class="dot" style="background:#64748b"></i>file</span>
    <span><i class="dot" style="background:#38bdf8"></i>function</span>
    <span><i class="dot" style="background:#a78bfa"></i>method</span>
    <span><i class="dot" style="background:#fb923c"></i>struct</span>
    <span><i class="dot" style="background:#fbbf24"></i>class</span>
    <span><i class="dot" style="background:#f472b6"></i>interface</span>
    <span><i class="dot" style="background:#4ade80"></i>type</span>
  </div>
  <label class="chk"><input type="checkbox" id="r-calls" checked> calls</label>
  <label class="chk"><input type="checkbox" id="r-imports" checked> imports</label>
  <label class="chk"><input type="checkbox" id="r-contains" checked> contains</label>
  <label class="chk"><input type="checkbox" id="r-extends" checked> extends</label>
  <label class="chk"><input type="checkbox" id="lbl-mode"> подписи всех символов</label>
  <input type="text" id="search" placeholder="Поиск символа или файла… (Enter)">
  <button id="fit">Вписать в экран</button>
  <div class="hint">ЛКМ по узлу — выделить соседей, тащить — мышью. Клик по фону / Esc — сброс. Колесо — зум.</div>
</div>
<div class="tooltip" id="tt"></div>
<svg id="g"></svg>
<script>
const DATA = __PAYLOAD__;
const KINDD = {file:"#64748b", function:"#38bdf8", method:"#a78bfa",
               struct:"#fb923c", class:"#fbbf24", interface:"#f472b6", type:"#4ade80"};
const svg = document.getElementById("g");
const NS = "http://www.w3.org/2000/svg";
const nodes = DATA.nodes;
const byId = new Map(nodes.map(n=>[n.id,n]));
const edges = DATA.edges.filter(e=>byId.has(e.s)&&byId.has(e.t));
const W=innerWidth, H=innerHeight;

const gE=document.createElementNS(NS,"g"); svg.appendChild(gE);
const gN=document.createElementNS(NS,"g"); svg.appendChild(gN);
const gT=document.createElementNS(NS,"g"); svg.appendChild(gT);
const edgeEls=[], nodeEls=[], labelEls=[];

edges.forEach(e=>{
  const l=document.createElementNS(NS,"line");
  l.setAttribute("class","edge-"+e.r);
  gE.appendChild(l); edgeEls.push({el:l,e});
});
nodes.forEach(n=>{
  const r = n.kind==="file" ? 8 : 4;
  const c=document.createElementNS(NS,"circle");
  c.setAttribute("r",r); c.setAttribute("fill",KINDD[n.kind]||"#64748b");
  c.setAttribute("stroke", n.id.startsWith("notifier")? "#0d9488" : "#6366f1");
  c.setAttribute("stroke-width", n.kind==="file"? 2 : 1);
  c.style.cursor="pointer"; c.__n=n; gN.appendChild(c); nodeEls.push(c);
  const t=document.createElementNS(NS,"text");
  t.setAttribute("x",n.x); t.setAttribute("y",n.y-r-4);
  t.setAttribute("text-anchor","middle"); t.textContent=n.name;
  if(n.kind==="file") t.setAttribute("class","flabel");
  else t.setAttribute("class","nlabel");
  t.style.display = n.kind==="file" ? "" : "none";
  gT.appendChild(t); labelEls.push(t);
});

const tt=document.getElementById("tt");
let hl=null;
function neighbors(n){
  const s=new Set([n]);
  edges.forEach(e=>{ if(e.s===n.id) s.add(byId.get(e.t)); if(e.t===n.id) s.add(byId.get(e.s)); });
  return s;
}
function applyHl(){
  const visRels=["calls","imports","contains","extends"].filter(r=>document.getElementById("r-"+r).checked);
  const set = hl? neighbors(hl) : null;
  edgeEls.forEach(({el,e})=>{
    const on = visRels.includes(e.r);
    el.style.display = on? "" : "none";
    el.classList.toggle("edge-dim", !!set && on && !(set.has(byId.get(e.s))&&set.has(byId.get(e.t))));
  });
  nodeEls.forEach(c=>{ c.style.opacity = (set && !set.has(c.__n))? .15 : 1; });
  labelEls.forEach((t,i)=>{
    const n=nodes[i];
    const showAll=document.getElementById("lbl-mode").checked;
    t.style.display = (n.kind==="file" || showAll || (set&&set.has(n)))? "" : "none";
    t.style.fontWeight = (hl===n)? "700":"";
  });
}
function tick(){
  edgeEls.forEach(({el,e})=>{
    const a=byId.get(e.s), b=byId.get(e.t);
    el.setAttribute("x1",a.x); el.setAttribute("y1",a.y);
    el.setAttribute("x2",b.x); el.setAttribute("y2",b.y);
  });
  nodes.forEach((n,i)=>{
    nodeEls[i].setAttribute("cx",n.x); nodeEls[i].setAttribute("cy",n.y);
    const r=nodeEls[i].getAttribute("r");
    labelEls[i].setAttribute("x",n.x); labelEls[i].setAttribute("y",n.y-r-4);
  });
}
let vx=0, vy=0, scale=1;
function draw(){ svg.setAttribute("viewBox", vx+" "+vy+" "+(W/scale)+" "+(H/scale)); tick(); }

let drag=null, pan=null;
svg.addEventListener("mousedown", ev=>{
  if(ev.target.__n){ drag=ev.target.__n; ev.preventDefault(); }
  else pan={x:ev.clientX,y:ev.clientY,vx,vy};
});
addEventListener("mousemove", ev=>{
  if(drag){
    drag.x = vx+ev.clientX/scale; drag.y = vy+ev.clientY/scale; tick(); return;
  }
  if(pan){
    vx = pan.vx-(ev.clientX-pan.x)/scale; vy = pan.vy-(ev.clientY-pan.y)/scale;
    draw();
  }
});
addEventListener("mouseup", ()=>{ drag=null; pan=null; });
svg.addEventListener("wheel", ev=>{
  ev.preventDefault();
  const f = ev.deltaY<0? 1.12 : 1/1.12;
  const mx=vx+ev.clientX/scale, my=vy+ev.clientY/scale;
  scale=Math.min(8,Math.max(.2,scale*f));
  vx=mx-ev.clientX/scale; vy=my-ev.clientY/scale;
  draw();
},{passive:false});

nodeEls.forEach(c=>{
  c.addEventListener("mouseenter", ev=>{
    const n=c.__n;
    tt.innerHTML =
      `<div class="t-name">${esc(n.name)} <span style="color:#64748b">· ${n.kind}</span></div>`+
      `<div class="t-path">${esc(n.path)} ${n.span}</div>`+
      (n.sig? `<div class="t-sig">${esc(n.sig)}</div>`:"");
    tt.style.display="block"; moveTT(ev);
    c.setAttribute("stroke-width",3);
  });
  c.addEventListener("mousemove", moveTT);
  c.addEventListener("mouseleave", ()=>{ tt.style.display="none"; c.setAttribute("stroke-width", c.__n.kind==="file"?2:1); });
  c.addEventListener("click", ev=>{ ev.stopPropagation(); hl = (hl===c.__n)? null : c.__n; applyHl(); draw(); });
});
svg.addEventListener("click", ()=>{ if(hl){ hl=null; applyHl(); draw(); } });
addEventListener("keydown", ev=>{ if(ev.key==="Escape" && hl){ hl=null; applyHl(); draw(); } });
function moveTT(ev){ tt.style.left=Math.min(innerWidth-440,ev.clientX+14)+"px"; tt.style.top=(ev.clientY+14)+"px"; }
function esc(s){ return s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;"); }

document.querySelectorAll('input[type=checkbox]').forEach(c=>c.addEventListener("change", applyHl));
document.getElementById("fit").onclick = fit;
document.getElementById("search").addEventListener("keydown", ev=>{
  if(ev.key!=="Enter") return;
  const q=ev.target.value.trim().toLowerCase(); if(!q) return;
  const hits=nodes.filter(n=>n.name.toLowerCase().includes(q)||n.path.toLowerCase().includes(q));
  if(!hits.length) return;
  hl=hits[0];
  scale=1.6; vx=hits[0].x-innerWidth/2/scale; vy=hits[0].y-innerHeight/2/scale;
  applyHl(); draw();
  if(hits.length>1) console.log("Ещё найдено:", hits.slice(1,8).map(h=>h.id).join(", "));
});
function fit(){
  const xs=nodes.map(n=>n.x), ys=nodes.map(n=>n.y);
  const x0=Math.min(...xs)-70, x1=Math.max(...xs)+70, y0=Math.min(...ys)-70, y1=Math.max(...ys)+70;
  scale=Math.min(4, Math.max(.15, Math.min(W/(x1-x0), H/(y1-y0))));
  vx=x0; vy=y0; draw();
}
fit();
</script>
</body>
</html>
"""

out = TEMPLATE.replace("__PAYLOAD__", payload)
pathlib.Path(OUT).write_text(out, encoding="utf-8")
print(f"OK: {OUT}  nodes={N} internal_edges={len(edges_out)}")
