(async function () {
  const FEED_SEL = '#messagesFeed';
  const FEED_URL_LATEST = 'api/messages/latest';
  const FEED_URL_ALL = 'api/messages';
  const el = document.querySelector(FEED_SEL);
  if (!el) return;
  const btnAll = document.getElementById('loadAllMessagesBtn');
  const h = (s, ...v) => s.reduce((a, b, i) => a + b + (v[i] ?? ''), '');

  const esc = (s) =>
    (s ?? '').replace(/[&<>"']/g, (c) => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[c]));

  const typeClass = (t) => (t === 'alert' ? 'type-alert' : t === 'warn' ? 'type-warn' : 'type-info');
  const messageHref = (id) => `messages/${id}`;

  const fmtTime = (ts) => {
    if (!ts) return '';
    const d = new Date(ts * 1000);
    return d.toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: '2-digit',
      hour: '2-digit', minute: '2-digit'
    });
  };

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

  async function fetchMessages(url) {
    const res = await fetch(url, { credentials: 'same-origin' });
    if (!res.ok) throw new Error(await res.text());
    const j = await res.json();
    return (j && j.data && j.data.messages) || [];
  }

  async function fetchLatest() {
    return fetchMessages(FEED_URL_LATEST);
  }

  async function fetchAll() {
    return fetchMessages(FEED_URL_ALL);
  }

  function render(messages) {
    el.innerHTML = '';

    messages.forEach((m) => {
      const hasImg = !!m.hasImage && !!m.imageUrl;
      const cls = typeClass((m.type || '').toLowerCase());
      const title = esc(m.title);
      const bodyHTML = mdToHtml(m.message);
      const timeText = fmtTime(m.timestamp);
      const href = messageHref(m.id);
      const node = document.createElement('article');
      node.className = `msg ${cls} ${hasImg ? '' : 'no-image'}`;

      node.innerHTML = h`
        <div class="msg__media">
          ${hasImg ? `<img class="msg__img" src="${esc(m.imageUrl)}" alt="">` : ``}
        </div>
        <div class="msg__title">
          <span class="msg__titletext">${title}</span>
          <span class="msg__time">${timeText}</span>
        </div>
        <div class="msg__bodylink" data-href="${esc(href)}" aria-label="Open message: ${title}">
          <div class="msg__body">${bodyHTML}</div>
        </div>
      `;
      node.addEventListener('click', (e) => {
        const a = e.target && e.target.closest ? e.target.closest('a') : null;
        if (a) return;
        window.location.href = href;
      });

      node.tabIndex = 0;
      node.setAttribute('role', 'link');
      node.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          window.location.href = href;
        }
      });

      el.appendChild(node);
    });
  }

  async function loadLatest() {
    const msgs = await fetchLatest();
    render(msgs);

    if (btnAll) btnAll.style.display = '';
  }

  async function loadAll() {
    if (!btnAll) return;

    btnAll.disabled = true;
    const oldText = btnAll.textContent;
    btnAll.textContent = 'Loading…';

    try {
      const msgs = await fetchAll();
      render(msgs);
      btnAll.style.display = 'none';
    } catch (e) {
      console.error(e);
      btnAll.disabled = false;
      btnAll.textContent = oldText || 'See All Messages';
    }
  }

  if (btnAll) {
    btnAll.addEventListener('click', loadAll);
  }

  try {
    await loadLatest();
  } catch (e) {
    console.error(e);
  }
})();