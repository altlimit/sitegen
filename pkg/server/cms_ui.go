package server

// cmsHTML is the single-page editor served at /__cms. Self-contained (no build
// step, no external assets) to preserve the single-static-binary model.
//
// Two editing modes:
//   - Blocks: a form-based builder for pages whose frontmatter has a blocks:
//     list. Fields are inferred from existing values; an optional site/cms.yaml
//     adds typed widgets and an "add block" type picker (hybrid). Saves go to
//     /api/blocks (structured, type-first, order/comment preserving).
//   - Raw: the frontmatter (YAML) + body textareas, for any file. Saves go to
//     /api/source.
const cmsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>SiteGen CMS</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  :root { --bg:#0f1117; --panel:#171a23; --border:#2a2f3a; --fg:#e6e8ee; --muted:#8b93a7; --accent:#5b9dff; --danger:#ff6b6b; }
  * { box-sizing:border-box; }
  body { margin:0; font:14px/1.5 system-ui,sans-serif; background:var(--bg); color:var(--fg); height:100vh; display:flex; flex-direction:column; }
  header { display:flex; align-items:center; gap:12px; padding:10px 16px; background:var(--panel); border-bottom:1px solid var(--border); }
  header h1 { font-size:15px; margin:0; font-weight:600; }
  header .path { color:var(--muted); font-size:13px; }
  header .spacer { flex:1; }
  button { background:var(--accent); color:#fff; border:0; border-radius:6px; padding:7px 14px; font:inherit; cursor:pointer; }
  button.ghost { background:transparent; border:1px solid var(--border); color:var(--muted); padding:4px 9px; }
  button.ghost.on { color:var(--accent); border-color:var(--accent); }
  button:disabled { opacity:.5; cursor:default; }
  #status { font-size:13px; color:var(--muted); min-width:80px; }
  main { flex:1; display:grid; grid-template-columns:210px 1fr 1fr; min-height:0; }
  main.nopreview { grid-template-columns:210px 1fr; }
  main.nopreview .preview { display:none; }
  aside { background:var(--panel); border-right:1px solid var(--border); overflow:auto; }
  #collections, #datafiles { border-bottom:1px solid var(--border); padding:6px 0; }
  .colrow { display:flex; align-items:center; gap:4px; padding:4px 10px 4px 14px; }
  .colrow .cname { flex:1; color:var(--fg); font-size:12px; text-transform:uppercase; letter-spacing:.04em; }
  .colrow .new { background:transparent; border:1px solid var(--border); color:var(--accent); border-radius:5px; padding:2px 8px; font-size:12px; }
  .datarow { padding:5px 14px; cursor:pointer; color:var(--muted); font-size:13px; }
  .datarow:hover { background:#1e2230; color:var(--fg); }
  .datarow.active { background:#1e2230; color:var(--accent); }
  .create { display:none; flex-direction:column; padding:16px; overflow:auto; }
  .create.show { display:flex; }
  .create h2 { font-size:15px; margin:0 0 12px; }
  .create .err { color:var(--danger); font-size:13px; min-height:18px; }
  .create .actions { display:flex; gap:8px; margin-top:10px; }
  aside .file { padding:7px 14px; cursor:pointer; color:var(--muted); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  aside .file:hover { background:#1e2230; color:var(--fg); }
  aside .file.active { background:#1e2230; color:var(--accent); }
  .work { display:flex; flex-direction:column; border-right:1px solid var(--border); min-width:0; overflow:auto; }
  .toolbar { display:flex; align-items:center; gap:8px; padding:8px 12px; border-bottom:1px solid var(--border); }
  .toolbar .grow { flex:1; }
  .blocks { padding:12px; overflow:auto; }
  .block { background:var(--panel); border:1px solid var(--border); border-radius:8px; margin-bottom:12px; }
  .block .bhead { display:flex; align-items:center; gap:8px; padding:8px 12px; border-bottom:1px solid var(--border); }
  .block .btype { font-weight:600; color:var(--accent); font-size:13px; }
  .block .bhead .spacer { flex:1; }
  .block .bbody { padding:10px 12px; }
  .fld { margin-bottom:10px; }
  .fld label { display:block; color:var(--muted); font-size:12px; margin-bottom:3px; }
  .fld input, .fld textarea { width:100%; background:var(--bg); color:var(--fg); border:1px solid var(--border); border-radius:5px; padding:7px 9px; font:inherit; outline:none; }
  .fld textarea { resize:vertical; min-height:60px; font:13px/1.5 ui-monospace,Menlo,monospace; }
  .fld.adv textarea { min-height:80px; }
  .fld .hint { color:var(--muted); font-size:11px; }
  .fld .cbrow { display:flex; align-items:center; gap:8px; margin-bottom:0; cursor:pointer; }
  .fld .cbrow input { width:auto; }
  .fld .cbrow span { color:var(--fg); text-transform:none; letter-spacing:0; }
  .subbox { border:1px solid var(--border); border-radius:6px; padding:8px 10px; background:#12151d; }
  .listitem { border:1px solid var(--border); border-radius:6px; padding:8px 10px; margin-top:6px; background:#12151d; }
  .ihead { display:flex; align-items:center; gap:6px; margin-bottom:6px; }
  .ihead .iname { color:var(--muted); font-size:12px; }
  .ihead .spacer { flex:1; }
  button.additem { margin-top:8px; }
  .imgfield { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
  .imgfield .thumb { max-height:56px; max-width:96px; border:1px solid var(--border); border-radius:5px; background:#fff; }
  .imgfield .imgpath { color:var(--muted); font-size:12px; font-family:ui-monospace,Menlo,monospace; }
  .iconbtn { background:transparent; border:0; color:var(--muted); cursor:pointer; font-size:15px; padding:2px 6px; }
  .iconbtn:hover { color:var(--fg); }
  .iconbtn.del:hover { color:var(--danger); }
  .raw { display:none; flex-direction:column; flex:1; min-height:0; }
  .raw.show { display:flex; }
  .blocks.hide { display:none; }
  .raw label { padding:8px 14px 4px; color:var(--muted); font-size:12px; text-transform:uppercase; letter-spacing:.04em; }
  .raw textarea { width:100%; background:var(--bg); color:var(--fg); border:0; border-top:1px solid var(--border); padding:12px 14px; font:13px/1.55 ui-monospace,Menlo,monospace; resize:none; outline:none; }
  .raw #fm { flex:0 0 32%; }
  .raw #body { flex:1; }
  .preview { display:flex; flex-direction:column; min-width:0; }
  .preview .bar { display:flex; align-items:center; gap:6px; padding:5px 8px; background:var(--panel); border-bottom:1px solid var(--border); }
  .reloadbtn { background:transparent; border:0; color:var(--muted); cursor:pointer; font-size:16px; line-height:1; padding:3px 6px; border-radius:5px; }
  .reloadbtn:hover { color:var(--fg); background:#1e2230; }
  .reloadbtn.spin { animation:spin .7s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }
  .purl { flex:1; min-width:0; background:var(--bg); color:var(--fg); border:1px solid var(--border); border-radius:6px; padding:4px 9px; font:12px/1.4 ui-monospace,Menlo,Consolas,monospace; outline:none; }
  .purl:focus { border-color:var(--accent); }
  iframe { flex:1; border:0; background:#fff; }
  select { background:var(--bg); color:var(--fg); border:1px solid var(--border); border-radius:5px; padding:5px 8px; font:inherit; }
  .empty { color:var(--muted); padding:24px; }
</style>
</head>
<body>
<header>
  <h1>SiteGen CMS</h1>
  <span class="path" id="curpath"></span>
  <span class="spacer"></span>
  <span id="status"></span>
  <button class="ghost" id="openTab" title="Open preview in a new tab" style="display:none">Open ↗</button>
  <button class="ghost" id="togglePreview" title="Toggle live preview">Hide preview</button>
  <button id="save" disabled>Save</button>
</header>
<main>
  <aside>
    <div id="collections"></div>
    <div id="datafiles"></div>
    <div id="files"></div>
  </aside>
  <section class="work">
    <div class="toolbar" id="toolbar" style="display:none">
      <button class="ghost on" id="modeBlocks">Blocks</button>
      <button class="ghost" id="modeRaw">Raw</button>
      <span class="grow"></span>
      <span id="addWrap" style="display:none">
        <select id="addType"></select>
        <button class="ghost" id="addBtn">+ Add block</button>
      </span>
    </div>
    <div class="blocks" id="blocks"></div>
    <div class="raw" id="raw">
      <label>Frontmatter (YAML)</label>
      <textarea id="fm" spellcheck="false"></textarea>
      <label>Body</label>
      <textarea id="body" spellcheck="false"></textarea>
    </div>
    <div class="create" id="create"></div>
    <div class="create" id="data"></div>
  </section>
  <section class="preview">
    <div class="bar">
      <button class="reloadbtn" id="reload" title="Reload preview">⟳</button>
      <input class="purl" id="purl" spellcheck="false" title="Preview URL — edit and press Enter to navigate">
    </div>
    <iframe id="preview" src="/"></iframe>
  </section>
</main>
<script>
var cur = null, config = {blocks:[]}, mode = 'blocks';
// rerender redraws the active view (blocks / create / data). The recursive
// field+list editors are shared across views, so their add/remove/reorder
// handlers call rerender() rather than assuming a block page.
var rerender = function(){};
var $ = function(id){ return document.getElementById(id); };
function setStatus(t){ $('status').textContent = t || ''; }

fetch('/__cms/api/config').then(function(r){return r.json();}).then(function(c){ config = c||{blocks:[],collections:[],data:[]}; renderCollections(); renderDataFiles(); });

function renderCollections(){
  var host=$('collections'); host.innerHTML='';
  (config.collections||[]).forEach(function(col){
    var row=document.createElement('div'); row.className='colrow';
    var nm=document.createElement('span'); nm.className='cname'; nm.textContent=(col.label||col.name); row.appendChild(nm);
    var btn=document.createElement('button'); btn.className='new'; btn.textContent='+ New';
    btn.onclick=function(){ openCreate(col); };
    row.appendChild(btn); host.appendChild(row);
  });
}

function renderDataFiles(){
  var host=$('datafiles'); host.innerHTML='';
  (config.data||[]).forEach(function(def){
    var row=document.createElement('div'); row.className='datarow'; row.textContent='⚙ '+(def.label||def.name);
    row.onclick=function(){ openData(def, row); };
    host.appendChild(row);
  });
}

function hideEditors(){
  $('create').classList.remove('show'); $('data').classList.remove('show');
  $('blocks').classList.add('hide'); $('raw').classList.remove('show'); $('toolbar').style.display='none';
}

function openData(def, row){
  cur=null; $('save').disabled=true; $('curpath').textContent=(def.label||def.name);
  hideEditors();
  [].forEach.call(document.querySelectorAll('.datarow'), function(c){ c.classList.remove('active'); });
  if(row) row.classList.add('active');
  fetch('/__cms/api/data?name='+encodeURIComponent(def.name)).then(function(r){return r.json();}).then(function(d){
    var holder = def.list ? {items: Array.isArray(d.value)?d.value:[]}
                          : ((d.value&&typeof d.value==='object'&&!Array.isArray(d.value))?d.value:{});
    if(!def.list){ (def.fields||[]).forEach(function(f){ if(!(f.name in holder)) holder[f.name]=''; }); }
    var err=document.createElement('div'); err.className='err';
    function draw(){
      var host=$('data'); host.innerHTML=''; host.classList.add('show');
      var h=document.createElement('h2'); h.textContent=(def.label||def.name); host.appendChild(h);
      if(def.list){ renderList(host, holder, 'items', {label:'Items', widget:'list', fields:def.fields}); }
      else { (def.fields||[]).forEach(function(f){ renderField(host, holder, f.name, f); }); }
      host.appendChild(err);
      var actions=document.createElement('div'); actions.className='actions';
      var ok=document.createElement('button'); ok.textContent='Save';
      ok.onclick=function(){ saveData(def, def.list?holder.items:holder, err, ok); };
      actions.appendChild(ok); host.appendChild(actions);
    }
    rerender = draw;
    draw();
  });
}

function saveData(def, value, errEl, btn){
  btn.disabled=true; errEl.textContent='';
  fetch('/__cms/api/data',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({name:def.name, value:value})})
    .then(function(r){ return r.json().then(function(j){return {ok:r.ok,j:j};}); })
    .then(function(res){
      btn.disabled=false;
      if(!res.ok){ errEl.textContent=res.j.error||'save failed'; return; }
      errEl.textContent=''; setStatus('saved ✓');
      setTimeout(reloadPreview, 500);
    }).catch(function(){ btn.disabled=false; errEl.textContent='save failed'; });
}

function bodyFieldOf(col){ var bf='body'; (col.fields||[]).forEach(function(f){ if(f.widget==='markdown') bf=f.name; }); return bf; }

function openCreate(col){
  cur=null; $('save').disabled=true; $('curpath').textContent='New '+(col.label||col.name);
  $('toolbar').style.display='none'; $('blocks').classList.add('hide'); $('raw').classList.remove('show');
  $('data').classList.remove('show');
  [].forEach.call($('files').children, function(c){ c.classList.remove('active'); });
  [].forEach.call(document.querySelectorAll('.datarow'), function(c){ c.classList.remove('active'); });
  var draft={}, bf=bodyFieldOf(col), bodyVal='';
  (col.fields||[]).forEach(function(f){ if(f.widget==='hidden'||f.name===bf) return; draft[f.name]=(f.widget==='list')?[]:''; });
  var err=document.createElement('div'); err.className='err';
  function draw(){
    var host=$('create'); host.innerHTML=''; host.classList.add('show');
    var h=document.createElement('h2'); h.textContent='New '+(col.label||col.name); host.appendChild(h);
    (col.fields||[]).forEach(function(f){ if(f.widget==='hidden'||f.name===bf) return; renderField(host, draft, f.name, f); });
    var brow=document.createElement('div'); brow.className='fld';
    var blbl=document.createElement('label'); blbl.textContent='Body'; brow.appendChild(blbl);
    var bta=document.createElement('textarea'); bta.style.minHeight='180px'; bta.value=bodyVal;
    bta.oninput=function(){ bodyVal=bta.value; }; brow.appendChild(bta); host.appendChild(brow);
    host.appendChild(err);
    var actions=document.createElement('div'); actions.className='actions';
    var ok=document.createElement('button'); ok.textContent='Create';
    ok.onclick=function(){ submitCreate(col, draft, bodyVal, err, ok); };
    var cancel=document.createElement('button'); cancel.className='ghost'; cancel.textContent='Cancel';
    cancel.onclick=function(){ host.classList.remove('show'); $('blocks').classList.remove('hide'); };
    actions.appendChild(ok); actions.appendChild(cancel); host.appendChild(actions);
  }
  rerender = draw;
  draw();
}

function submitCreate(col, draft, body, errEl, btn){
  btn.disabled=true; errEl.textContent='';
  fetch('/__cms/api/create',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({collection:col.name, fields:draft, body:body})})
    .then(function(r){ return r.json().then(function(j){return {ok:r.ok,j:j};}); })
    .then(function(res){
      btn.disabled=false;
      if(!res.ok){ errEl.textContent=res.j.error||'create failed'; return; }
      $('create').classList.remove('show'); $('blocks').classList.remove('hide');
      loadList(); openFile(res.j.path);
      setTimeout(reloadPreview, 500);
    }).catch(function(){ btn.disabled=false; errEl.textContent='create failed'; });
}

function loadList(){
  fetch('/__cms/api/sources').then(function(r){return r.json();}).then(function(d){
    $('files').innerHTML = '';
    (d.sources||[]).forEach(function(p){
      var el = document.createElement('div');
      el.className='file'; el.textContent=p; el.dataset.path=p;
      el.onclick=function(){ openFile(p); };
      $('files').appendChild(el);
    });
  });
}

function openFile(p){
  fetch('/__cms/api/source?path='+encodeURIComponent(p)).then(function(r){return r.json();}).then(function(d){
    cur = d;
    $('create').classList.remove('show'); $('data').classList.remove('show');
    [].forEach.call(document.querySelectorAll('.datarow'), function(c){ c.classList.remove('active'); });
    $('curpath').textContent = p;
    // make the live preview follow the page being edited
    if(d.previewURL && $('preview').getAttribute('src')!==d.previewURL){ navPreview(d.previewURL); }
    $('save').disabled = false;
    setStatus('');
    [].forEach.call($('files').children, function(c){ c.classList.toggle('active', c.dataset.path===p); });
    $('toolbar').style.display = 'flex';
    // default to blocks mode only for block pages
    mode = d.isBlockPage ? 'blocks' : 'raw';
    $('fm').value = d.frontmatter||'';
    $('body').value = d.body||'';
    applyMode();
  });
}

// refreshRawFromDisk re-reads the current file so the Raw frontmatter/body
// textareas reflect edits made in Blocks mode (which only mutate cur.blocks in
// memory until saved). Leaves cur.blocks alone so the live Blocks DOM bindings
// stay valid.
function refreshRawFromDisk(cb){
  if(!cur || !cur.path){ if(cb) cb(); return; }
  fetch('/__cms/api/source?path='+encodeURIComponent(cur.path)).then(function(r){return r.json();}).then(function(d){
    cur.frontmatter=d.frontmatter; cur.body=d.body;
    $('fm').value=d.frontmatter||''; $('body').value=d.body||'';
    if(cb) cb();
  }).catch(function(){ if(cb) cb(); });
}

function applyMode(){
  var blockPage = cur && cur.isBlockPage;
  $('modeBlocks').classList.toggle('on', mode==='blocks');
  $('modeRaw').classList.toggle('on', mode==='raw');
  $('modeBlocks').disabled = !blockPage;
  $('raw').classList.toggle('show', mode==='raw');
  $('blocks').classList.toggle('hide', mode==='raw');
  var showAdd = (mode==='blocks' && blockPage);
  $('addWrap').style.display = showAdd ? '' : 'none';
  if(showAdd){ rerender = renderBlocks; refreshAddMenu(); renderBlocks(); }
}

function blockDef(type){ return (config.blocks||[]).filter(function(b){return b.type===type;})[0]||null; }
function defByName(defs, name){ return defs? (defs.filter(function(f){return f.name===name;})[0]||null) : null; }
function labelOf(def, key){ return (def&&def.label)||key; }

// scalar widget: explicit cms.yaml widget wins, else infer from the value
function scalarWidget(widget, val){
  if(widget==='date' || widget==='datetime') return widget;
  if(widget==='text') return 'text';
  if(widget==='string') return 'string';
  if(typeof val==='string' && (val.length>60 || val.indexOf('\n')>=0)) return 'text';
  return 'string';
}

function iconBtn(txt, title, cls, fn){
  if(typeof cls==='function'){ fn=cls; cls=''; }
  var b=document.createElement('button'); b.className='iconbtn '+(cls||''); b.textContent=txt; b.title=title; b.onclick=fn; return b;
}

function renderBlocks(){
  var host = $('blocks'); host.innerHTML='';
  if(!cur.blocks || !cur.blocks.length){ host.innerHTML='<div class="empty">No blocks. Use “+ Add block”.</div>'; }
  (cur.blocks||[]).forEach(function(block, idx){
    var def = blockDef(block.type);
    var card = document.createElement('div'); card.className='block';
    var head = document.createElement('div'); head.className='bhead';
    head.innerHTML = '<span class="btype">'+((def&&def.label)||block.type||'block')+'</span><span class="spacer"></span>';
    head.appendChild(iconBtn('↑','move up',function(){ swapAt(cur.blocks,idx,idx-1); }));
    head.appendChild(iconBtn('↓','move down',function(){ swapAt(cur.blocks,idx,idx+1); }));
    head.appendChild(iconBtn('✕','delete','del',function(){ cur.blocks.splice(idx,1); rerender(); }));
    card.appendChild(head);
    var body = document.createElement('div'); body.className='bbody';
    Object.keys(block).forEach(function(key){
      if(key==='type') return;
      renderField(body, block, key, defByName(def&&def.fields, key));
    });
    card.appendChild(body);
    host.appendChild(card);
  });
}

// renderField dispatches by value/schema: list -> list editor, object ->
// nested sub-form, scalar -> input/textarea. obj[key] is mutated in place.
function renderField(parent, obj, key, def){
  var val = obj[key];
  if(def&&def.widget==='boolean'){ renderBoolean(parent, obj, key, labelOf(def,key)); return; }
  if(def&&def.widget==='image'){ renderImage(parent, obj, key, labelOf(def,key)); return; }
  if((def&&def.widget==='list') || Array.isArray(val)){ renderList(parent, obj, key, def); return; }
  if(val!==null && typeof val==='object'){ renderObject(parent, obj, key, def); return; }
  renderScalar(parent, obj, key, scalarWidget(def&&def.widget, val), labelOf(def,key));
}

function renderImage(parent, obj, key, labelText){
  var row=document.createElement('div'); row.className='fld';
  var lbl=document.createElement('label'); lbl.textContent=labelText; row.appendChild(lbl);
  var wrap=document.createElement('div'); wrap.className='imgfield';
  function refresh(){
    wrap.innerHTML='';
    if(obj[key]){
      var img=document.createElement('img'); img.className='thumb'; img.src=obj[key]; wrap.appendChild(img);
      var pth=document.createElement('span'); pth.className='imgpath'; pth.textContent=obj[key]; wrap.appendChild(pth);
      var rm=document.createElement('button'); rm.className='ghost'; rm.textContent='Remove'; rm.onclick=function(){ obj[key]=''; refresh(); }; wrap.appendChild(rm);
    }
    var fin=document.createElement('input'); fin.type='file'; fin.accept='image/*'; fin.style.display='none';
    var up=document.createElement('button'); up.className='ghost'; up.textContent=obj[key]?'Replace':'Upload image'; up.onclick=function(){ fin.click(); };
    fin.onchange=function(){ if(fin.files[0]) uploadImage(fin.files[0], function(url){ obj[key]=url; refresh(); }); };
    wrap.appendChild(up); wrap.appendChild(fin);
  }
  refresh();
  row.appendChild(wrap); parent.appendChild(row);
}

function uploadImage(file, cb){
  var fd=new FormData(); fd.append('file', file);
  setStatus('uploading…');
  fetch('/__cms/api/upload',{method:'POST',body:fd})
    .then(function(r){ return r.json().then(function(j){return {ok:r.ok,j:j};}); })
    .then(function(res){ if(!res.ok){ setStatus('upload failed'); alert((res.j&&res.j.error)||'upload failed'); return; } setStatus('uploaded ✓'); cb(res.j.url); })
    .catch(function(){ setStatus('upload failed'); });
}

function renderBoolean(parent, obj, key, labelText){
  var row=document.createElement('div'); row.className='fld';
  var lab=document.createElement('label'); lab.className='cbrow';
  var cb=document.createElement('input'); cb.type='checkbox'; cb.checked=!!obj[key];
  cb.onchange=function(){ obj[key]=cb.checked; };
  var span=document.createElement('span'); span.textContent=labelText;
  lab.appendChild(cb); lab.appendChild(span);
  row.appendChild(lab); parent.appendChild(row);
}

function renderScalar(parent, obj, key, widget, labelText){
  var row=document.createElement('div'); row.className='fld';
  var lbl=document.createElement('label'); lbl.textContent=labelText; row.appendChild(lbl);
  var el = (widget==='text') ? document.createElement('textarea') : document.createElement('input');
  if(el.tagName==='INPUT'){ el.type = (widget==='date'?'date':(widget==='datetime'?'datetime-local':'text')); }
  el.value = (obj[key]==null)?'':String(obj[key]);
  el.oninput=function(){ obj[key]=el.value; };
  row.appendChild(el); parent.appendChild(row);
}

function renderObject(parent, obj, key, def){
  var group=document.createElement('div'); group.className='fld';
  var lbl=document.createElement('label'); lbl.textContent=labelOf(def,key); group.appendChild(lbl);
  var box=document.createElement('div'); box.className='subbox';
  var sub=obj[key];
  var keys=(def&&def.fields)? def.fields.map(function(f){return f.name;}) : Object.keys(sub);
  keys.forEach(function(k){ renderField(box, sub, k, defByName(def&&def.fields,k)); });
  group.appendChild(box); parent.appendChild(group);
}

function renderList(parent, obj, key, def){
  var arr=obj[key]; if(!Array.isArray(arr)){ arr=[]; obj[key]=arr; }
  var itemDefs = def&&def.fields;
  var group=document.createElement('div'); group.className='fld';
  var lbl=document.createElement('label'); lbl.textContent=labelOf(def,key)+' ('+arr.length+')'; group.appendChild(lbl);
  arr.forEach(function(item, i){
    var it=document.createElement('div'); it.className='listitem';
    var ih=document.createElement('div'); ih.className='ihead';
    ih.innerHTML='<span class="iname">Item '+(i+1)+'</span><span class="spacer"></span>';
    ih.appendChild(iconBtn('↑','move up',function(){ swapAt(arr,i,i-1); }));
    ih.appendChild(iconBtn('↓','move down',function(){ swapAt(arr,i,i+1); }));
    ih.appendChild(iconBtn('✕','remove','del',function(){ arr.splice(i,1); rerender(); }));
    it.appendChild(ih);
    var ib=document.createElement('div'); ib.className='ibody';
    if(item!==null && typeof item==='object' && !Array.isArray(item)){
      var keys=itemDefs? itemDefs.map(function(f){return f.name;}) : Object.keys(item);
      keys.forEach(function(k){ renderField(ib, item, k, defByName(itemDefs,k)); });
    } else {
      renderScalar(ib, arr, i, scalarWidget(null,item), 'value');
    }
    it.appendChild(ib); group.appendChild(it);
  });
  var add=document.createElement('button'); add.className='ghost additem'; add.textContent='+ Add item';
  add.onclick=function(){ arr.push(newItem(itemDefs, arr.length?arr[0]:undefined)); rerender(); };
  group.appendChild(add); parent.appendChild(group);
}

function newItem(defs, sample){
  if(defs && defs.length){ var o={}; defs.forEach(function(f){ o[f.name] = (f.widget==='list')?[]:''; }); return o; }
  if(sample!==undefined) return cloneShape(sample);
  return '';
}
function cloneShape(s){
  if(Array.isArray(s)) return [];
  if(s!==null && typeof s==='object'){ var o={}; Object.keys(s).forEach(function(k){ o[k]=cloneShape(s[k]); }); return o; }
  return '';
}

function swapAt(arr,a,b){ if(b<0||b>=arr.length) return; var t=arr[a]; arr[a]=arr[b]; arr[b]=t; rerender(); }

function availableTypes(){
  var set={};
  (config.blocks||[]).forEach(function(b){ set[b.type]=true; });
  (cur && cur.blocks || []).forEach(function(b){ if(b.type) set[b.type]=true; });
  return Object.keys(set);
}

function refreshAddMenu(){
  var sel=$('addType'); sel.innerHTML='';
  availableTypes().forEach(function(t){ var o=document.createElement('option'); o.value=t; o.textContent=t; sel.appendChild(o); });
}

function addBlock(){
  var type=$('addType').value; if(!type) return;
  var def=(config.blocks||[]).filter(function(b){return b.type===type;})[0];
  var blk={type:type};
  if(def){ (def.fields||[]).forEach(function(f){ blk[f.name] = (f.widget==='list')?[]:''; }); }
  else {
    // infer fields from another existing block of the same type
    var sample=(cur.blocks||[]).filter(function(b){return b.type===type;})[0];
    if(sample){ Object.keys(sample).forEach(function(k){ if(k!=='type') blk[k]=(typeof sample[k]==='object')?(Array.isArray(sample[k])?[]:{}):''; }); }
  }
  cur.blocks=cur.blocks||[]; cur.blocks.push(blk); rerender();
}

function save(){
  if(!cur) return;
  $('save').disabled=true; setStatus('saving…');
  var url, payload;
  if(mode==='blocks' && cur.isBlockPage){ url='/__cms/api/blocks'; payload={path:cur.path, blocks:cur.blocks}; }
  else { url='/__cms/api/source'; payload={path:cur.path, frontmatter:$('fm').value, body:$('body').value}; }
  fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)})
    .then(function(r){ return r.json().then(function(j){ return {ok:r.ok,j:j}; }); })
    .then(function(res){
      $('save').disabled=false;
      if(!res.ok){ setStatus('error'); alert(res.j.error||'save failed'); return; }
      setStatus('saved ✓');
      // keep the Raw frontmatter/body view in sync with what was written
      if(mode==='blocks' && cur.isBlockPage){ refreshRawFromDisk(); }
      setTimeout(reloadPreview, 400);
    }).catch(function(){ $('save').disabled=false; setStatus('error'); });
}

$('modeBlocks').onclick=function(){ if(!$('modeBlocks').disabled){ mode='blocks'; applyMode(); } };
$('modeRaw').onclick=function(){ refreshRawFromDisk(function(){ mode='raw'; applyMode(); }); };
$('addBtn').onclick=addBlock;
$('save').onclick=save;

// --- Preview controls: spinning reload, browser-like URL bar, open-in-tab ---
var previewEl=$('preview');
function startSpin(){ $('reload').classList.add('spin'); }
function stopSpin(){ $('reload').classList.remove('spin'); }
function previewHref(){ try{ return previewEl.contentWindow.location.href; }catch(e){ return previewEl.src; } }
function navPreview(url){ startSpin(); previewEl.src=url; }
function reloadPreview(){ startSpin(); try{ previewEl.contentWindow.location.reload(); }catch(e){ previewEl.src=previewEl.src; } }
previewEl.addEventListener('load', function(){
  stopSpin();
  try{ if(document.activeElement!==$('purl')) $('purl').value = previewEl.contentWindow.location.href; }catch(e){}
});
$('reload').onclick=reloadPreview;
$('purl').addEventListener('keydown', function(e){ if(e.key==='Enter'){ e.preventDefault(); navPreview($('purl').value); } });
$('openTab').onclick=function(){ var u=previewHref(); if(u) window.open(u, '_blank'); };
try{ $('purl').value = previewHref(); }catch(e){}

function applyPreview(){
  var hidden = localStorage.getItem('cms.hidePreview')==='1';
  document.querySelector('main').classList.toggle('nopreview', hidden);
  $('togglePreview').textContent = hidden ? 'Show preview' : 'Hide preview';
  $('openTab').style.display = hidden ? '' : 'none';
}
$('togglePreview').onclick=function(){
  localStorage.setItem('cms.hidePreview', localStorage.getItem('cms.hidePreview')==='1' ? '0' : '1');
  applyPreview();
};
applyPreview();

document.addEventListener('keydown',function(e){ if((e.metaKey||e.ctrlKey)&&e.key==='s'){ e.preventDefault(); save(); } });
loadList();
</script>
</body>
</html>`
