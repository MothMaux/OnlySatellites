window.MessageViewer = (function(){
  const FEED_ENDPOINT = id => `../../api/messages/${id}`;

  const esc = (s) => (s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':'&#39;'}[c]));
  function mdToHtml(md) {
    let s = esc(md || '');
    s = s.replace(/`([^`]+)`/g, '<code>$1</code>');
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    s = s.replace(/\[([^\]]*)\]\(([^)]+)\)/g, (_, text, url) => {
      const u = String(url || '').trim();
      const label = String(text || '').trim();

      if (!/^https?:\/\//i.test(u)) {
        return label ? `${label} (${esc(u)})` : esc(u);
      }
      const shown = label ? esc(label) : esc(u);
      return `<a href="${esc(u)}" target="_blank" rel="noopener noreferrer">${shown}</a>`;
    });

    return s.replace(/\r?\n/g, '<br>');
  }

  const fmtTime = (ts) => {
    if (!ts) return '';
    const d = new Date(ts * 1000);
    return d.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit'
    });
  };

  const typeClass = (t) => (t === 'alert' ? 'type-alert' : t === 'warn' ? 'type-warn' : 'type-info');

  async function fetchMessage(id){
    const res = await fetch(FEED_ENDPOINT(id), { credentials: 'same-origin' });
    if (!res.ok) throw new Error(await res.text());
    const j = await res.json();
    return j && j.data;
  }

  function makeCard(m) {
    const hasImg = !!m.hasImage && !!m.imageUrl;
    const cls = typeClass((m.type||'').toLowerCase());

    const el = document.createElement('article');
    el.className = `msg ${cls} ${hasImg ? '' : 'no-image'}`;
    el.style.maxWidth = '1100px';

    el.innerHTML = `
      <div class="msg__media">${hasImg ? `<img class="msg__img" src="${m.imageUrl}" alt="">` : ``}</div>
      <div class="msg__title">
        <span class="msg__time">${fmtTime(m.timestamp)}</span>
        <span class="msg__titletext">${esc(m.title)}</span>
      </div>
      <div class="msg__bodylink">
        <div class="msg__body" style="display:block; -webkit-line-clamp:initial; white-space:normal;">${mdToHtml(m.message)}</div>
      </div>`;
    return el;
  }

  function ensureModalShell(){
    let shell = document.querySelector('#messageModal');
    if (shell) return shell;
    shell = document.createElement('div');
    shell.id = 'messageModal';
    shell.innerHTML = `
      <style>
        #messageModal { position: fixed; inset: 0; display:none; align-items:center; justify-content:center; background: rgba(0,0,0,.6); z-index: 9999; }
        #messageModal.active { display:flex; }
        #messageModal .panel { background: var(--panel, #171a21); border: 2px solid #222833; border-radius: 14px; padding: 16px; max-height: 90vh; overflow:auto; }
        #messageModal .close { position:absolute; top: 16px; right: 20px; background:transparent; border:0; color: var(--text, #eaeef5); font-size: 22px; cursor:pointer; }
      </style>
      <button class="close" aria-label="Close">×</button>
      <div class="panel"></div>`;
    document.body.appendChild(shell);

    const close = () => shell.classList.remove('active');
    shell.querySelector('.close').addEventListener('click', close);
    shell.addEventListener('click', (e)=>{ if(e.target===shell) close(); });
    window.addEventListener('keydown', (e)=>{ if(e.key==='Escape') close(); });
    return shell;
  }

  async function openModalFor(id){
    const shell = ensureModalShell();
    const panel = shell.querySelector('.panel');
    panel.innerHTML = '<div style="padding:40px;color:var(--text,.8)">Loading…</div>';
    shell.classList.add('active');
    try {
      const m = await fetchMessage(id);
      panel.innerHTML = '';
      panel.appendChild(makeCard(m));
    } catch(e){
      panel.innerHTML = `<div style="padding:40px;color:var(--text,.8)">Failed to load message: ${esc(e.message)}</div>`;
    }
  }

  function attachModalTriggers(containerSel='body'){
    const root = document.querySelector(containerSel) || document.body;
    root.addEventListener('click', (e)=>{
      const a = e.target.closest('a[href^="/messages/"]');
      if (!a) return;
      const m = a.getAttribute('href').match(/\/messages\/(\d+)/);
      if (!m) return;
      e.preventDefault();
      openModalFor(Number(m[1]));
    });
  }

  async function renderStandalone(mountSel, id){
    const mount = document.querySelector(mountSel);
    if (!mount) return;
    try {
      const m = await fetchMessage(id);
      mount.innerHTML = '';
      mount.appendChild(makeCard(m));
    } catch(e){
      mount.innerHTML = `<p style="opacity:.8">Failed to load message.</p>`;
    }
  }

  return { attachModalTriggers, renderStandalone };
})();